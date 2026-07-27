package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/packages/go-common/logger"
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
