package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	feishuconnector "github.com/cocola-project/cocola/apps/gateway/internal/channel/feishu"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
)

const (
	knowledgeReady                  = "ready"
	knowledgeConnectorRequired      = "connector_required"
	knowledgePermissionRequired     = "permission_required"
	knowledgeNotFound               = "not_found"
	knowledgeTemporarilyUnavailable = "temporarily_unavailable"
	knowledgeUnsupported            = "unsupported"
	maxKnowledgeResponseBytes       = int64(1 << 20)
)

type agentKnowledgeCheckRequest struct {
	Sources []agentprofile.KnowledgeSource `json:"sources"`
}

type agentKnowledgeCheckResult struct {
	Source agentprofile.KnowledgeSource `json:"source"`
	Status string                       `json:"status"`
}

func (a *API) checkAgentKnowledge(w http.ResponseWriter, r *http.Request) {
	id, ok := a.agentIdentity(w, r)
	if !ok {
		return
	}
	agent, err := a.agents.GetActive(r.Context(), id, r.PathValue("id"))
	if a.writeAgentError(w, err) {
		return
	}
	var input agentKnowledgeCheckRequest
	if !decodeAgentJSON(w, r, &input) {
		return
	}
	if len(input.Sources) > agentprofile.MaxKnowledgeSources {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "too many Knowledge sources")
		return
	}

	results := make([]agentKnowledgeCheckResult, len(input.Sources))
	valid := make([]bool, len(input.Sources))
	for index, source := range input.Sources {
		normalized, sourceOK := agentprofile.NormalizeKnowledgeSource(source)
		if !sourceOK {
			results[index] = agentKnowledgeCheckResult{Source: source, Status: knowledgeUnsupported}
			continue
		}
		results[index] = agentKnowledgeCheckResult{Source: normalized}
		if normalized.Type == agentprofile.KnowledgeTypeCocolaWiki {
			results[index].Status = a.checkCocolaWikiKnowledge(r.Context(), id, normalized)
			continue
		}
		valid[index] = true
	}
	if len(input.Sources) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	hasPendingFeishuSource := false
	for index := range valid {
		if valid[index] {
			hasPendingFeishuSource = true
			break
		}
	}
	if !hasPendingFeishuSource {
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}

	catalog, catalogErr := a.fetchAgentSkillCatalog(r.Context(), id)
	if catalogErr != nil {
		setPendingKnowledgeStatus(results, valid, knowledgeTemporarilyUnavailable)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	for index := range results {
		if valid[index] && !agentKnowledgeSkillsAvailable(agent, results[index].Source, catalog) {
			results[index].Status = knowledgeUnsupported
			valid[index] = false
		}
	}

	if a.feishu == nil {
		setPendingKnowledgeStatus(results, valid, knowledgeConnectorRequired)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	identity, _ := auth.IdentityOf(r)
	feishuID := feishuconnector.Identity{
		TenantID: identity.TenantID,
		UserID:   identity.UserID,
	}
	connectorID, err := a.feishu.ConnectorID(r.Context(), feishuID, agent.ID)
	if err != nil {
		setPendingKnowledgeStatus(results, valid, knowledgeTemporarilyUnavailable)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	if connectorID == "" {
		setPendingKnowledgeStatus(results, valid, knowledgeConnectorRequired)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	credential, err := a.feishu.RuntimeCredentialByID(r.Context(), feishuID, connectorID)
	if err != nil || credential.Status == feishuconnector.RuntimeCredentialUnavailable {
		setPendingKnowledgeStatus(results, valid, knowledgeTemporarilyUnavailable)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	if credential.Status != feishuconnector.RuntimeCredentialReady ||
		credential.TenantAccessToken == "" {
		setPendingKnowledgeStatus(results, valid, knowledgeConnectorRequired)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}

	sem := make(chan struct{}, 4)
	var wait sync.WaitGroup
	for index := range results {
		if !valid[index] {
			continue
		}
		wait.Add(1)
		go func(resultIndex int) {
			defer wait.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-r.Context().Done():
				results[resultIndex].Status = knowledgeTemporarilyUnavailable
				return
			}
			results[resultIndex].Status = a.checkFeishuKnowledgeSource(
				r.Context(), credential, results[resultIndex].Source,
			)
		}(index)
	}
	wait.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (a *API) checkCocolaWikiKnowledge(
	ctx context.Context,
	id agentprofile.Identity,
	source agentprofile.KnowledgeSource,
) string {
	if a.wiki == nil || a.store == nil {
		return knowledgeTemporarilyUnavailable
	}
	_, _, err := a.wiki.GetCurrent(ctx, wiki.Identity{
		TenantID: id.TenantID,
		UserID:   id.UserID,
	}, source.NodeID)
	switch {
	case err == nil:
		return knowledgeReady
	case errors.Is(err, wiki.ErrNotFound):
		return knowledgeNotFound
	default:
		return knowledgeTemporarilyUnavailable
	}
}

func setPendingKnowledgeStatus(
	results []agentKnowledgeCheckResult,
	valid []bool,
	status string,
) {
	for index := range results {
		if valid[index] && results[index].Status == "" {
			results[index].Status = status
		}
	}
}

func agentKnowledgeSkillsAvailable(
	agent agentprofile.Agent,
	source agentprofile.KnowledgeSource,
	catalog []agentSkillCatalogItem,
) bool {
	if source.Type == agentprofile.KnowledgeTypeCocolaWiki {
		return true
	}
	required := agentprofile.RequiredKnowledgeSkillIDs(source.Type)
	if len(required) == 0 {
		return false
	}
	availableRuntimeIDs := make(map[string]struct{})
	if len(agent.SkillIDs) == 0 {
		for _, skill := range catalog {
			if skill.Available && skill.DefaultEnabled {
				availableRuntimeIDs[skill.RuntimeID] = struct{}{}
			}
		}
	} else {
		selected := make(map[string]struct{}, len(agent.SkillIDs))
		for _, catalogID := range agent.SkillIDs {
			selected[catalogID] = struct{}{}
		}
		for _, skill := range catalog {
			if _, ok := selected[skill.ID]; ok && skill.Available {
				availableRuntimeIDs[skill.RuntimeID] = struct{}{}
			}
		}
	}
	for _, runtimeID := range required {
		if _, ok := availableRuntimeIDs[runtimeID]; !ok {
			return false
		}
	}
	return true
}

func (a *API) checkFeishuKnowledgeSource(
	ctx context.Context,
	credential feishuconnector.RuntimeCredential,
	source agentprofile.KnowledgeSource,
) string {
	if a.agentKnowledgeHTTPClient == nil {
		return knowledgeTemporarilyUnavailable
	}
	endpoint, ok := feishuKnowledgeEndpoint(credential.Brand, source)
	if !ok {
		return knowledgeUnsupported
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return knowledgeTemporarilyUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+credential.TenantAccessToken)
	request.Header.Set("Accept", "application/json")
	response, err := a.agentKnowledgeHTTPClient.Do(request)
	if err != nil {
		return knowledgeTemporarilyUnavailable
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return knowledgeNotFound
	case response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusForbidden:
		return knowledgePermissionRequired
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return knowledgeTemporarilyUnavailable
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxKnowledgeResponseBytes+1))
	if err != nil {
		return knowledgeTemporarilyUnavailable
	}
	if int64(len(data)) > maxKnowledgeResponseBytes {
		return knowledgeTemporarilyUnavailable
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return knowledgeTemporarilyUnavailable
	}
	if envelope.Code == 0 {
		return knowledgeReady
	}
	return mapFeishuKnowledgeError(envelope.Code, envelope.Msg)
}

func feishuKnowledgeEndpoint(
	brand string,
	source agentprofile.KnowledgeSource,
) (string, bool) {
	baseURL := "https://open.feishu.cn"
	if brand == feishuconnector.DomainLark {
		baseURL = "https://open.larksuite.com"
	} else if brand != feishuconnector.DomainFeishu {
		return "", false
	}
	token := agentprofile.KnowledgeToken(source)
	if token == "" {
		return "", false
	}
	escaped := url.PathEscape(token)
	switch source.Type {
	case agentprofile.KnowledgeTypeFeishuDoc:
		return baseURL + "/open-apis/docx/v1/documents/" + escaped + "/raw_content", true
	case agentprofile.KnowledgeTypeFeishuWiki:
		return baseURL + "/open-apis/wiki/v2/spaces/get_node?token=" +
			url.QueryEscape(token), true
	case agentprofile.KnowledgeTypeFeishuSheet:
		return baseURL + "/open-apis/sheets/v3/spreadsheets/" + escaped + "/sheets/query", true
	case agentprofile.KnowledgeTypeFeishuBase:
		return baseURL + "/open-apis/bitable/v1/apps/" + escaped, true
	default:
		return "", false
	}
}

func mapFeishuKnowledgeError(code int, message string) string {
	switch code {
	case 1770002, 1770003, 131005, 1310214, 1310249, 1254003, 1254040:
		return knowledgeNotFound
	case 1770032, 131006, 1310213, 1254302, 99991672, 99991679:
		return knowledgePermissionRequired
	}
	normalized := strings.ToLower(message)
	switch {
	case strings.Contains(normalized, "not found"),
		strings.Contains(normalized, "not exist"):
		return knowledgeNotFound
	case strings.Contains(normalized, "permission"),
		strings.Contains(normalized, "forbidden"),
		strings.Contains(normalized, "access denied"):
		return knowledgePermissionRequired
	default:
		return knowledgeTemporarilyUnavailable
	}
}
