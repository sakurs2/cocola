package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
)

const maxAgentRequestBytes = agentprofile.MaxInstructionsBytes + 8*1024

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
	default:
		a.log.Warn("Agent request failed: " + strings.ReplaceAll(err.Error(), "\n", " "))
		writeErr(w, http.StatusServiceUnavailable, "AGENTS_UNAVAILABLE", "Agent service unavailable")
	}
	return true
}
