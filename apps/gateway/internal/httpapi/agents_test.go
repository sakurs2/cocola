package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

func newAgentHandler() http.Handler {
	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must())
	return api.WithAgents(agentprofile.NewService(agentprofile.NewMemory())).Handler()
}

func agentRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		request.Header.Set("content-type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAgentHTTPLifecycleAndVersionConflict(t *testing.T) {
	handler := newAgentHandler()
	response := agentRequest(t, handler, http.MethodPost, "/v1/agents", map[string]any{
		"name": "Analyst", "description": "Explains data", "instructions": "Verify totals.",
		"avatar_key": "chart", "avatar_color": "cyan", "runtime_id": "claude-code",
		"model_route_id": "route-1", "model_alias": "sonnet",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created agentprofile.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Version != 1 {
		t.Fatalf("created Agent = %+v", created)
	}

	response = agentRequest(t, handler, http.MethodGet, "/v1/agents", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var listed []agentprofile.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed Agents = %+v", listed)
	}

	update := map[string]any{
		"name": "Senior Analyst", "description": created.Description,
		"instructions": created.Instructions, "avatar_key": created.AvatarKey,
		"avatar_color": created.AvatarColor, "runtime_id": created.RuntimeID,
		"model_route_id": created.ModelRouteID, "model_alias": created.ModelAlias,
		"version": created.Version,
	}
	response = agentRequest(t, handler, http.MethodPatch, "/v1/agents/"+created.ID, update)
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	var updated agentprofile.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Senior Analyst" || updated.Version != 2 {
		t.Fatalf("updated Agent = %+v", updated)
	}

	response = agentRequest(t, handler, http.MethodPatch, "/v1/agents/"+created.ID, update)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale update status=%d body=%s", response.Code, response.Body.String())
	}

	response = agentRequest(t, handler, http.MethodDelete, "/v1/agents/"+created.ID, map[string]any{
		"version": updated.Version,
	})
	if response.Code != http.StatusNoContent {
		t.Fatalf("archive status=%d body=%s", response.Code, response.Body.String())
	}
	response = agentRequest(t, handler, http.MethodGet, "/v1/agents", nil)
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("list after archive status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentHTTPRejectsInvalidID(t *testing.T) {
	response := agentRequest(t, newAgentHandler(), http.MethodGet, "/v1/agents/not-a-uuid", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentHTTPSkillSelectionValidationAndPreservation(t *testing.T) {
	catalogUnavailable := false
	adminDisabled := false
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/skills/agent-catalog" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("Agent catalog request is missing its runtime user credential")
		}
		if catalogUnavailable {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		available := !adminDisabled
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": []map[string]any{
			{
				"id": "personal-disabled-by-default", "runtime_id": "private",
				"available": true, "default_enabled": false,
			},
			{
				"id": "shared-a", "runtime_id": "duplicate",
				"available": available, "default_enabled": available,
			},
			{
				"id": "shared-b", "runtime_id": "duplicate",
				"available": true, "default_enabled": true,
			},
		}})
	}))
	defer admin.Close()

	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must())
	api.WithAgents(agentprofile.NewService(agentprofile.NewMemory()))
	api.WithSandboxTokenIssuer(token.NewIssuer("agent-catalog-secret", "cocola", time.Hour), time.Hour)
	api.WithAgentSkillCatalog(admin.URL, admin.Client())
	handler := api.Handler()

	create := map[string]any{
		"name": "Specialist", "runtime_id": "claude-code",
		"model_route_id": "route-1", "model_alias": "sonnet",
		"skill_ids": []string{"personal-disabled-by-default", "shared-a"},
	}
	response := agentRequest(t, handler, http.MethodPost, "/v1/agents", create)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created agentprofile.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.SkillIDs) != 2 {
		t.Fatalf("created Skill IDs = %#v", created.SkillIDs)
	}

	create["name"] = "Duplicate"
	create["skill_ids"] = []string{"shared-a", "shared-b"}
	response = agentRequest(t, handler, http.MethodPost, "/v1/agents", create)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate runtime status=%d body=%s", response.Code, response.Body.String())
	}

	adminDisabled = true
	update := map[string]any{
		"name": "Renamed Specialist", "runtime_id": created.RuntimeID,
		"model_route_id": created.ModelRouteID, "model_alias": created.ModelAlias,
		"avatar_key": created.AvatarKey, "avatar_color": created.AvatarColor,
		"skill_ids": created.SkillIDs, "version": created.Version,
	}
	response = agentRequest(t, handler, http.MethodPatch, "/v1/agents/"+created.ID, update)
	if response.Code != http.StatusOK {
		t.Fatalf("preserve disabled selection status=%d body=%s", response.Code, response.Body.String())
	}
	var preserved agentprofile.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &preserved); err != nil {
		t.Fatal(err)
	}
	update["skill_ids"] = []string{"shared-a", "shared-b"}
	update["version"] = preserved.Version
	response = agentRequest(t, handler, http.MethodPatch, "/v1/agents/"+created.ID, update)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate with preserved disabled selection status=%d body=%s",
			response.Code, response.Body.String())
	}

	create["name"] = "Unavailable"
	create["skill_ids"] = []string{"shared-a"}
	response = agentRequest(t, handler, http.MethodPost, "/v1/agents", create)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("new disabled selection status=%d body=%s", response.Code, response.Body.String())
	}

	catalogUnavailable = true
	create["name"] = "Catalog Outage"
	create["skill_ids"] = []string{"shared-b"}
	response = agentRequest(t, handler, http.MethodPost, "/v1/agents", create)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("catalog outage status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "SKILL_CATALOG_UNAVAILABLE" {
		t.Fatalf("catalog outage error = %q", envelope.Error.Code)
	}
}

func TestAgentHTTPCocolaWikiKnowledgeUsesOwnerIdentityAndPreservesDeletedSource(t *testing.T) {
	nodeID := uuid.NewString()
	wikiStore := &wikiStoreStub{
		currentNode: wiki.Node{ID: nodeID, Kind: "file", Name: "handbook.md"},
		current:     wiki.Version{ID: uuid.NewString(), NodeID: nodeID},
	}
	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024).
		WithAgents(agentprofile.NewService(agentprofile.NewMemory()))
	handler := api.Handler()

	response := agentRequest(t, handler, http.MethodPost, "/v1/agents", map[string]any{
		"name": "Wiki specialist", "runtime_id": "claude-code",
		"model_route_id": "route-1", "model_alias": "sonnet",
		"knowledge_sources": []map[string]any{{
			"type": "cocola_wiki", "label": "Handbook", "node_id": nodeID,
		}},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if wikiStore.currentID != (wiki.Identity{
		TenantID: auth.DevIdentity.TenantID,
		UserID:   auth.DevIdentity.UserID,
	}) {
		t.Fatalf("Wiki validation identity = %+v", wikiStore.currentID)
	}
	var created agentprofile.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	wikiStore.currentErr = wiki.ErrNotFound
	wikiStore.currentNodeID = ""
	response = agentRequest(t, handler, http.MethodPatch, "/v1/agents/"+created.ID, map[string]any{
		"name": "Renamed specialist", "runtime_id": created.RuntimeID,
		"model_route_id": created.ModelRouteID, "model_alias": created.ModelAlias,
		"avatar_key": created.AvatarKey, "avatar_color": created.AvatarColor,
		"knowledge_sources": created.KnowledgeSources, "version": created.Version,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("preserve deleted Wiki status=%d body=%s", response.Code, response.Body.String())
	}
	if wikiStore.currentNodeID != "" {
		t.Fatalf("preserved Wiki source was unexpectedly revalidated: %q", wikiStore.currentNodeID)
	}
}
