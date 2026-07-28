package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
)

const maxAgentRequestBytes = agentprofile.MaxInstructionsBytes + 64*1024

var errAgentSkillCatalogUnavailable = errors.New("Agent Skill catalog unavailable")
var errAgentWikiUnavailable = errors.New("Agent Wiki unavailable")

type archiveAgentRequest struct {
	Version int64 `json:"version"`
}

func (a *API) listAgents(w http.ResponseWriter, r *http.Request) {
	id, ok := a.agentIdentity(w, r)
	if !ok {
		return
	}
	result, err := a.agents.List(r.Context(), id)
	if a.writeAgentError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) createAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := a.agentIdentity(w, r)
	if !ok {
		return
	}
	var input agentprofile.CreateInput
	if !decodeAgentJSON(w, r, &input) {
		return
	}
	if err := a.validateAgentSkillSelection(
		r.Context(), id, input.SkillIDs, nil,
	); a.writeAgentError(w, err) {
		return
	}
	if err := a.validateNewAgentKnowledge(
		r.Context(), id, input.SkillIDs, input.KnowledgeSources, nil,
	); a.writeAgentError(w, err) {
		return
	}
	result, err := a.agents.Create(r.Context(), id, input)
	if a.writeAgentError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) getAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := a.agentIdentity(w, r)
	if !ok {
		return
	}
	result, err := a.agents.Get(r.Context(), id, r.PathValue("id"))
	if a.writeAgentError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) updateAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := a.agentIdentity(w, r)
	if !ok {
		return
	}
	var input agentprofile.UpdateInput
	if !decodeAgentJSON(w, r, &input) {
		return
	}
	current, err := a.agents.Get(r.Context(), id, r.PathValue("id"))
	if a.writeAgentError(w, err) {
		return
	}
	if err := a.validateAgentSkillSelection(
		r.Context(), id, input.SkillIDs, current.SkillIDs,
	); a.writeAgentError(w, err) {
		return
	}
	if err := a.validateNewAgentKnowledge(
		r.Context(), id, input.SkillIDs, input.KnowledgeSources, current.KnowledgeSources,
	); a.writeAgentError(w, err) {
		return
	}
	result, err := a.agents.Update(r.Context(), id, r.PathValue("id"), input)
	if a.writeAgentError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) archiveAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := a.agentIdentity(w, r)
	if !ok {
		return
	}
	var input archiveAgentRequest
	if !decodeAgentJSON(w, r, &input) {
		return
	}
	_, err := a.agents.Archive(r.Context(), id, r.PathValue("id"), input.Version)
	if a.writeAgentError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) agentIdentity(
	w http.ResponseWriter,
	r *http.Request,
) (agentprofile.Identity, bool) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return agentprofile.Identity{}, false
	}
	if a.agents == nil {
		writeErr(w, http.StatusServiceUnavailable, "AGENTS_UNAVAILABLE", "Agent service unavailable")
		return agentprofile.Identity{}, false
	}
	return agentprofile.Identity{TenantID: identity.TenantID, UserID: identity.UserID}, true
}

func decodeAgentJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	return true
}

func (a *API) writeAgentError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, agentprofile.ErrInvalidArgument):
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid Agent request")
	case errors.Is(err, agentprofile.ErrNotFound):
		writeErr(w, http.StatusNotFound, "AGENT_NOT_FOUND", "Agent not found")
	case errors.Is(err, agentprofile.ErrVersionConflict):
		writeErr(w, http.StatusConflict, "VERSION_CONFLICT", "Agent was changed by another request")
	case errors.Is(err, agentprofile.ErrConflict):
		writeErr(w, http.StatusConflict, "AGENT_CONFLICT", "an Agent with this name already exists")
	case errors.Is(err, agentprofile.ErrInUse):
		writeErr(w, http.StatusConflict, "AGENT_IN_USE", "disconnect the Agent's Feishu bot first")
	case errors.Is(err, agentprofile.ErrArchived):
		writeErr(w, http.StatusConflict, "AGENT_ARCHIVED", "Agent is archived")
	case errors.Is(err, errAgentSkillCatalogUnavailable):
		writeErr(w, http.StatusServiceUnavailable, "SKILL_CATALOG_UNAVAILABLE", "Skills are temporarily unavailable")
	case errors.Is(err, errAgentWikiUnavailable):
		writeErr(w, http.StatusServiceUnavailable, "WIKI_UNAVAILABLE", "Wiki is temporarily unavailable")
	default:
		a.log.Warn("Agent request failed: " + strings.ReplaceAll(err.Error(), "\n", " "))
		writeErr(w, http.StatusServiceUnavailable, "AGENTS_UNAVAILABLE", "Agent service unavailable")
	}
	return true
}

type agentSkillCatalogItem struct {
	ID             string `json:"id"`
	RuntimeID      string `json:"runtime_id"`
	Available      bool   `json:"available"`
	DefaultEnabled bool   `json:"default_enabled"`
}

func (a *API) validateAgentSkillSelection(
	ctx context.Context,
	id agentprofile.Identity,
	selected []string,
	preserved []string,
) error {
	if len(selected) == 0 {
		return nil
	}
	catalogItems, err := a.fetchAgentSkillCatalog(ctx, id)
	if err != nil {
		return err
	}
	catalog := make(map[string]agentSkillCatalogItem, len(catalogItems))
	for _, item := range catalogItems {
		catalog[item.ID] = item
	}
	preservedSet := make(map[string]struct{}, len(preserved))
	for _, value := range preserved {
		preservedSet[value] = struct{}{}
	}
	seenRuntimeIDs := make(map[string]struct{}, len(selected))
	for _, catalogID := range selected {
		item, exists := catalog[catalogID]
		_, wasPreserved := preservedSet[catalogID]
		if !exists {
			if wasPreserved {
				continue
			}
			return agentprofile.ErrInvalidArgument
		}
		if !item.Available && !wasPreserved {
			return agentprofile.ErrInvalidArgument
		}
		runtimeID := strings.TrimSpace(item.RuntimeID)
		if runtimeID == "" {
			return agentprofile.ErrInvalidArgument
		}
		if _, duplicate := seenRuntimeIDs[runtimeID]; duplicate {
			return agentprofile.ErrInvalidArgument
		}
		seenRuntimeIDs[runtimeID] = struct{}{}
	}
	return nil
}

func (a *API) fetchAgentSkillCatalog(
	ctx context.Context,
	id agentprofile.Identity,
) ([]agentSkillCatalogItem, error) {
	if a.agentSkillAdminURL == "" || a.agentSkillHTTPClient == nil ||
		a.sandboxTokenIssuer == nil {
		return nil, errAgentSkillCatalogUnavailable
	}
	runtimeToken, _, err := a.sandboxTokenIssuer.Issue(
		id.UserID, id.TenantID, 5*time.Minute, 0,
	)
	if err != nil {
		return nil, errAgentSkillCatalogUnavailable
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, a.agentSkillAdminURL+"/me/skills/agent-catalog", nil,
	)
	if err != nil {
		return nil, errAgentSkillCatalogUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+runtimeToken)
	request.Header.Set("Accept", "application/json")
	response, err := a.agentSkillHTTPClient.Do(request)
	if err != nil {
		return nil, errAgentSkillCatalogUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errAgentSkillCatalogUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, errAgentSkillCatalogUnavailable
	}
	var payload struct {
		Skills []agentSkillCatalogItem `json:"skills"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, errAgentSkillCatalogUnavailable
	}
	return payload.Skills, nil
}

func (a *API) validateNewAgentKnowledge(
	ctx context.Context,
	id agentprofile.Identity,
	skillIDs []string,
	sources []agentprofile.KnowledgeSource,
	preserved []agentprofile.KnowledgeSource,
) error {
	if len(sources) == 0 {
		return nil
	}
	normalizedSources := make([]agentprofile.KnowledgeSource, 0, len(sources))
	for _, source := range sources {
		normalized, ok := agentprofile.NormalizeKnowledgeSource(source)
		if !ok {
			return agentprofile.ErrInvalidArgument
		}
		normalizedSources = append(normalizedSources, normalized)
	}
	preservedKeys := make(map[string]struct{}, len(preserved))
	for _, source := range preserved {
		preservedKeys[agentprofile.KnowledgeSourceKey(source)] = struct{}{}
	}
	hasNewFeishuSource := false
	for _, source := range normalizedSources {
		key := agentprofile.KnowledgeSourceKey(source)
		if _, ok := preservedKeys[key]; ok {
			continue
		}
		if source.Type == agentprofile.KnowledgeTypeCocolaWiki {
			if a.wiki == nil || a.store == nil {
				return errAgentWikiUnavailable
			}
			_, _, err := a.wiki.GetCurrent(ctx, wiki.Identity{
				TenantID: id.TenantID,
				UserID:   id.UserID,
			}, source.NodeID)
			switch {
			case errors.Is(err, wiki.ErrNotFound):
				return agentprofile.ErrInvalidArgument
			case err != nil:
				return errAgentWikiUnavailable
			}
			continue
		}
		hasNewFeishuSource = true
	}
	if !hasNewFeishuSource {
		return nil
	}
	catalog, err := a.fetchAgentSkillCatalog(ctx, id)
	if err != nil {
		return err
	}
	candidate := agentprofile.Agent{SkillIDs: skillIDs}
	for _, source := range normalizedSources {
		if _, ok := preservedKeys[agentprofile.KnowledgeSourceKey(source)]; ok ||
			source.Type == agentprofile.KnowledgeTypeCocolaWiki {
			continue
		}
		if !agentKnowledgeSkillsAvailable(candidate, source, catalog) {
			return agentprofile.ErrInvalidArgument
		}
	}
	return nil
}

func agentKnowledgeSkillsAvailable(
	agent agentprofile.Agent,
	source agentprofile.KnowledgeSource,
	catalog []agentSkillCatalogItem,
) bool {
	required := agentprofile.RequiredKnowledgeSkillIDs(source.Type)
	if len(required) == 0 {
		return source.Type == agentprofile.KnowledgeTypeCocolaWiki
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
