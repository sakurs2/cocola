// Handlers for the admin surface. Each handler is the thin "decode -> call
// service -> encode" shell the package comment promises: it pulls path/query/
// body params, delegates to service.Admin, and maps the result (or a sentinel
// error via mapErr) to JSON. The actor for the audit trail is read from the
// request context, populated by requireAdmin.
package httpapi

import (
	"context"
	"errors"
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

// ---- auth users ----

type loginReq struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	identifier := req.Identifier
	if identifier == "" {
		identifier = req.Email
	}
	user, err := a.svc.Authenticate(r.Context(), identifier, req.Password)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

type createAuthUserReq struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Password string `json:"password"`
}

type updateAuthUserReq struct {
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
	Role     string `json:"role,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type resetAuthUserPasswordReq struct {
	Password string `json:"password"`
}

func (a *API) createAuthUser(w http.ResponseWriter, r *http.Request) {
	var req createAuthUserReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	u, err := a.svc.CreateAuthUser(r.Context(), service.AuthUserInput{
		Username: req.Username,
		Email:    req.Email,
		Role:     req.Role,
		Enabled:  req.Enabled,
		Password: req.Password,
		Actor:    actorOf(r),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "CONFLICT", "username or email already exists")
			return
		}
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) listAuthUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.svc.ListAuthUsers(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (a *API) lookupAuthUser(w http.ResponseWriter, r *http.Request) {
	u, err := a.svc.GetAuthUserByEmail(r.Context(), r.URL.Query().Get("email"))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) updateAuthUser(w http.ResponseWriter, r *http.Request) {
	var req updateAuthUserReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	u, err := a.svc.SetAuthUser(r.Context(), chi.URLParam(r, "id"), service.AuthUserInput{
		Username: req.Username,
		Email:    req.Email,
		Role:     req.Role,
		Enabled:  req.Enabled,
		Actor:    actorOf(r),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "CONFLICT", "username or email already exists")
			return
		}
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) resetAuthUserPassword(w http.ResponseWriter, r *http.Request) {
	var req resetAuthUserPasswordReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	u, err := a.svc.ResetAuthUserPassword(r.Context(), chi.URLParam(r, "id"), req.Password, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) deleteAuthUser(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteAuthUser(r.Context(), chi.URLParam(r, "id"), actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type runtimeTokenReq struct {
	UserID     string `json:"user_id"`
	TenantID   string `json:"tenant_id,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

func (a *API) issueRuntimeToken(w http.ResponseWriter, r *http.Request) {
	var req runtimeTokenReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	var ttl time.Duration
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	effectiveTTL := ttl
	if effectiveTTL <= 0 {
		effectiveTTL = 10 * time.Minute
	}
	tok, err := a.svc.IssueRuntimeToken(r.Context(), req.UserID, req.TenantID, ttl)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "ttl_seconds": int64(effectiveTTL.Seconds())})
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

// ---- sandbox nodes ----

func (a *API) listSandboxNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.svc.ListSandboxNodes(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (a *API) sandboxNodeJoinCommand(w http.ResponseWriter, r *http.Request) {
	cmd, err := a.svc.SandboxNodeJoinCommand(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cmd)
}

func (a *API) disableSandboxNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	node, err := a.svc.DisableSandboxNode(r.Context(), name, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (a *API) restoreSandboxNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	node, err := a.svc.RestoreSandboxNode(r.Context(), name, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

type offlineSandboxNodeReq struct {
	Force bool `json:"force,omitempty"`
}

func (a *API) offlineSandboxNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req offlineSandboxNodeReq
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			mapErr(w, err)
			return
		}
	}
	out, err := a.svc.OfflineSandboxNode(r.Context(), name, req.Force, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
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
