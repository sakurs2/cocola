// Handlers for the admin surface. Each handler is the thin "decode -> call
// service -> encode" shell the package comment promises: it pulls path/query/
// body params, delegates to service.Admin, and maps the result (or a sentinel
// error via mapErr) to JSON. The actor for the audit trail is read from the
// request context, populated by requireAdmin.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

// withActor stashes the resolved admin principal on the request context so
// handlers can attribute audit entries without re-parsing auth headers.
func withActor(r *http.Request, actor string) context.Context {
	return context.WithValue(r.Context(), actorKey, actor)
}

// ---- health ----

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- tokens ----

type issueTokenReq struct {
	UserID     string `json:"user_id"`
	Tenant     string `json:"tenant,omitempty"`
	TTLSeconds *int64 `json:"ttl_seconds,omitempty"` // nil => issuer default; <0 => non-expiring
}

func (a *API) issueToken(w http.ResponseWriter, r *http.Request) {
	var req issueTokenReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	in := service.IssueTokenInput{
		UserID: req.UserID,
		Tenant: req.Tenant,
		Actor:  actorOf(r),
	}
	if req.TTLSeconds != nil {
		if *req.TTLSeconds < 0 {
			in.TTL = -1 // any negative => non-expiring
		} else {
			in.TTL = time.Duration(*req.TTLSeconds) * time.Second
		}
	}
	res, err := a.svc.IssueToken(r.Context(), in)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user_id")
	recs, err := a.svc.ListTokens(r.Context(), user)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": recs})
}

func (a *API) listRevoked(w http.ResponseWriter, r *http.Request) {
	ids, err := a.svc.RevokedIDs(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": ids})
}

func (a *API) revokeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.svc.RevokeToken(r.Context(), id, actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- quotas ----

type setQuotaReq struct {
	Scope   string `json:"scope"`   // "user" | "tenant"
	Subject string `json:"subject"` // user_id or tenant_id
	Limit   int64  `json:"limit"`
}

func (a *API) listQuotas(w http.ResponseWriter, r *http.Request) {
	qs, err := a.svc.ListQuotas(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quotas": qs})
}

func (a *API) setQuota(w http.ResponseWriter, r *http.Request) {
	var req setQuotaReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	q, err := a.svc.SetQuota(r.Context(), req.Scope, req.Subject, req.Limit, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (a *API) deleteQuota(w http.ResponseWriter, r *http.Request) {
	scope := chi.URLParam(r, "scope")
	subject := chi.URLParam(r, "subject")
	if err := a.svc.DeleteQuota(r.Context(), scope, subject, actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- skills ----

type createSkillReq struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Entrypoint  string `json:"entrypoint,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
}

func (a *API) createSkill(w http.ResponseWriter, r *http.Request) {
	var req createSkillReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	s := store.Skill{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Version:     req.Version,
		Entrypoint:  req.Entrypoint,
		Enabled:     req.Enabled,
	}
	out, err := a.svc.CreateSkill(r.Context(), s, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *API) listSkills(w http.ResponseWriter, r *http.Request) {
	onlyEnabled := r.URL.Query().Get("enabled") == "true"
	skills, err := a.svc.ListSkills(r.Context(), onlyEnabled)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (a *API) getSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := a.svc.GetSkill(r.Context(), id)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) enableSkill(w http.ResponseWriter, r *http.Request) {
	a.setSkillEnabled(w, r, true)
}

func (a *API) disableSkill(w http.ResponseWriter, r *http.Request) {
	a.setSkillEnabled(w, r, false)
}

func (a *API) setSkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := chi.URLParam(r, "id")
	s, err := a.svc.SetSkillEnabled(r.Context(), id, enabled, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) deleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.svc.DeleteSkill(r.Context(), id, actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- audit ----

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit := qInt(r, "limit", 100)
	entries, err := a.svc.ListAudit(r.Context(), limit)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}
