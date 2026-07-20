package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
)

func (a *API) memorySettings(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.memory == nil {
		writeJSON(w, http.StatusOK, memory.Settings{UseEnabled: true, LearnEnabled: true})
		return
	}
	settings, err := a.memory.GetSettings(r.Context(), memory.Identity{
		TenantID: identity.TenantID, UserID: identity.UserID,
	})
	if err != nil {
		a.log.Warn("memory settings read failed: " + err.Error())
		writeErr(w, http.StatusServiceUnavailable, "MEMORY_UNAVAILABLE", "memory settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *API) updateMemorySettings(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.memory == nil {
		writeErr(w, http.StatusConflict, "MEMORY_DISABLED", "memory is disabled by administrator")
		return
	}
	var body struct {
		UseEnabled   bool `json:"use_enabled"`
		LearnEnabled bool `json:"learn_enabled"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return
	}
	settings, err := a.memory.UpdateSettings(r.Context(), memory.Identity{
		TenantID: identity.TenantID, UserID: identity.UserID,
	}, body.UseEnabled, body.LearnEnabled)
	if errors.Is(err, memory.ErrDisabled) {
		writeErr(w, http.StatusConflict, "MEMORY_DISABLED", "memory is disabled by administrator")
		return
	}
	if err != nil {
		a.log.Warn("memory settings update failed: " + err.Error())
		writeErr(w, http.StatusServiceUnavailable, "MEMORY_UNAVAILABLE", "memory settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *API) memoryItems(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.memoryRequestIdentity(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := a.memory.ListItems(r.Context(), identity, r.URL.Query().Get("category"),
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		a.memoryAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) memoryItem(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.memoryRequestIdentity(w, r)
	if !ok {
		return
	}
	item, err := a.memory.GetItem(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		a.memoryAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteMemoryItem(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.memoryRequestIdentity(w, r)
	if !ok {
		return
	}
	if err := a.memory.DeleteItem(r.Context(), identity, r.PathValue("id")); err != nil {
		a.memoryAPIError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) clearMemory(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.memoryRequestIdentity(w, r)
	if !ok {
		return
	}
	if err := a.memory.Clear(r.Context(), identity); err != nil {
		a.memoryAPIError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) memoryRequestIdentity(
	w http.ResponseWriter,
	r *http.Request,
) (memory.Identity, bool) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return memory.Identity{}, false
	}
	if a.memory == nil {
		writeErr(w, http.StatusServiceUnavailable, "MEMORY_UNAVAILABLE", "memory service unavailable")
		return memory.Identity{}, false
	}
	return memory.Identity{TenantID: identity.TenantID, UserID: identity.UserID}, true
}

func (a *API) memoryAPIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, memory.ErrNotFound):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "memory item not found")
	case strings.Contains(err.Error(), "invalid"):
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid memory request")
	default:
		a.log.Warn("memory API failed: " + err.Error())
		writeErr(w, http.StatusServiceUnavailable, "MEMORY_UNAVAILABLE", "memory service unavailable")
	}
}
