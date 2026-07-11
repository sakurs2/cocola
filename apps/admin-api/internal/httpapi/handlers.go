// Handlers for the admin surface. Each handler is the thin "decode -> call
// service -> encode" shell the package comment promises: it pulls path/query/
// body params, delegates to service.Admin, and maps the result (or a sentinel
// error via mapErr) to JSON. The actor for the audit trail is read from the
// request context, populated by requireAdmin.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func (a *API) getArchitecture(w http.ResponseWriter, r *http.Request) {
	graph, err := a.svc.Architecture(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (a *API) streamMyEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "streaming unsupported")
		return
	}
	userID := actorOf(r)
	ch, cancel, err := a.svc.SubscribeUserEvents(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	defer cancel()
	snapshot, err := a.svc.UserEventSnapshot(r.Context(), userID)
	if err != nil {
		mapErr(w, err)
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache, no-transform")
	w.Header().Set("connection", "keep-alive")
	w.Header().Set("x-accel-buffering", "no")
	w.WriteHeader(http.StatusOK)
	writeSSE(w, flusher, "snapshot", map[string]any{"events": snapshot})
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.UserID != userID {
				continue
			}
			writeSSE(w, flusher, "user_event", event)
		case <-heartbeat.C:
			snapshot, err := a.svc.UserEventSnapshot(r.Context(), userID)
			if err != nil {
				writeSSE(w, flusher, "ping", map[string]any{"at": time.Now().UTC()})
				continue
			}
			writeSSE(w, flusher, "snapshot", map[string]any{"events": snapshot})
		case <-r.Context().Done():
			return
		}
	}
}

// ---- auth users ----

type loginReq struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req loginReq
	if err := decode(r, &req); err != nil {
		a.appendHTTPAudit(r, "user", "auth.login", "auth", "", http.StatusBadRequest, "INVALID_ARGUMENT", start)
		mapErr(w, err)
		return
	}
	identifier := req.Identifier
	if identifier == "" {
		identifier = req.Email
	}
	user, err := a.svc.Authenticate(r.Context(), identifier, req.Password)
	if err != nil {
		a.appendHTTPAudit(r, "user", "auth.login", "auth", "", http.StatusUnauthorized, "UNAUTHENTICATED", start)
		mapErr(w, err)
		return
	}
	a.appendHTTPAudit(r, "user", "auth.login", "auth_user", user.ID, http.StatusOK, "", start)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

type createAuthUserReq struct {
	Username string  `json:"username"`
	Email    string  `json:"email"`
	Tenant   *string `json:"tenant_id,omitempty"`
	Role     string  `json:"role,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
	Password string  `json:"password"`
}

type updateAuthUserReq struct {
	Username string  `json:"username,omitempty"`
	Email    string  `json:"email,omitempty"`
	Tenant   *string `json:"tenant_id,omitempty"`
	Role     string  `json:"role,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
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
		Tenant:   req.Tenant,
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
		Tenant:   req.Tenant,
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

// ---- system settings ----

type updateSystemSettingReq struct {
	Value           any   `json:"value"`
	ExpectedVersion int64 `json:"expected_version"`
}

func (a *API) listSystemSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.svc.ListSystemSettings(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (a *API) updateSystemSetting(w http.ResponseWriter, r *http.Request) {
	var req updateSystemSettingReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	out, err := a.svc.UpdateSystemSetting(r.Context(), chi.URLParam(r, "key"), service.SystemSettingUpdateInput{
		Value:           req.Value,
		ExpectedVersion: req.ExpectedVersion,
		Actor:           actorOf(r),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) resetSystemSetting(w http.ResponseWriter, r *http.Request) {
	expected := qInt(r, "expected_version", -1)
	if err := a.svc.ResetSystemSetting(r.Context(), chi.URLParam(r, "key"), int64(expected), actorOf(r)); err != nil {
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

type skillGitReq struct {
	RepoURL     string   `json:"repo_url"`
	Ref         string   `json:"ref,omitempty"`
	Path        string   `json:"path,omitempty"`
	SelectedIDs []string `json:"selected_ids,omitempty"`
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

func readSkillArchiveUpload(r *http.Request) ([]byte, []string, error) {
	if err := r.ParseMultipartForm(80 << 20); err != nil {
		return nil, nil, service.ErrInvalidArg
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, nil, service.ErrInvalidArg
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 80<<20))
	if err != nil {
		return nil, nil, err
	}
	selected := make([]string, 0)
	for _, raw := range r.MultipartForm.Value["selected"] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				selected = append(selected, part)
			}
		}
	}
	for _, raw := range r.MultipartForm.Value["selected_ids"] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				selected = append(selected, part)
			}
		}
	}
	return data, selected, nil
}

func (a *API) scanSkillArchive(w http.ResponseWriter, r *http.Request) {
	data, _, err := readSkillArchiveUpload(r)
	if err != nil {
		mapErr(w, err)
		return
	}
	candidates, err := a.svc.ScanSkillArchive(r.Context(), data)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": candidates})
}

func (a *API) importSkillArchive(w http.ResponseWriter, r *http.Request) {
	data, selected, err := readSkillArchiveUpload(r)
	if err != nil {
		mapErr(w, err)
		return
	}
	imported, candidates, err := a.svc.ImportSkillArchive(r.Context(), "admin", "", actorOf(r), data, selected)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skills": imported, "candidates": candidates})
}

func (a *API) scanSkillGit(w http.ResponseWriter, r *http.Request) {
	var req skillGitReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	candidates, err := a.svc.ScanSkillGit(r.Context(), service.SkillGitInput{
		RepoURL: req.RepoURL,
		Ref:     req.Ref,
		Path:    req.Path,
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": candidates})
}

func (a *API) importSkillGit(w http.ResponseWriter, r *http.Request) {
	var req skillGitReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	imported, candidates, err := a.svc.ImportSkillGit(r.Context(), "admin", "", actorOf(r), service.SkillGitInput{
		RepoURL:     req.RepoURL,
		Ref:         req.Ref,
		Path:        req.Path,
		SelectedIDs: req.SelectedIDs,
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skills": imported, "candidates": candidates})
}

func (a *API) listEffectiveSkills(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		mapErr(w, service.ErrInvalidArg)
		return
	}
	skills, err := a.svc.ListEffectiveSkills(r.Context(), userID)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (a *API) getSkillBundle(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := a.svc.GetSkillBundle(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		mapErr(w, err)
		return
	}
	if contentType == "" {
		contentType = "application/zip"
	}
	w.Header().Set("content-type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) listMySkills(w http.ResponseWriter, r *http.Request) {
	skills, err := a.svc.ListUserSkillCatalog(r.Context(), actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (a *API) scanMySkillArchive(w http.ResponseWriter, r *http.Request) {
	data, _, err := readSkillArchiveUpload(r)
	if err != nil {
		mapErr(w, err)
		return
	}
	candidates, err := a.svc.ScanSkillArchive(r.Context(), data)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": candidates})
}

func (a *API) importMySkillArchive(w http.ResponseWriter, r *http.Request) {
	data, selected, err := readSkillArchiveUpload(r)
	if err != nil {
		mapErr(w, err)
		return
	}
	userID := actorOf(r)
	imported, candidates, err := a.svc.ImportSkillArchive(r.Context(), "user", userID, userID, data, selected)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skills": imported, "candidates": candidates})
}

func (a *API) scanMySkillGit(w http.ResponseWriter, r *http.Request) {
	var req skillGitReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	candidates, err := a.svc.ScanSkillGit(r.Context(), service.SkillGitInput{
		RepoURL: req.RepoURL,
		Ref:     req.Ref,
		Path:    req.Path,
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": candidates})
}

func (a *API) importMySkillGit(w http.ResponseWriter, r *http.Request) {
	var req skillGitReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	userID := actorOf(r)
	imported, candidates, err := a.svc.ImportSkillGit(r.Context(), "user", userID, userID, service.SkillGitInput{
		RepoURL:     req.RepoURL,
		Ref:         req.Ref,
		Path:        req.Path,
		SelectedIDs: req.SelectedIDs,
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skills": imported, "candidates": candidates})
}

func (a *API) getMySkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := a.svc.GetSkill(r.Context(), id)
	if err != nil {
		mapErr(w, err)
		return
	}
	if s.Scope == "user" && s.OwnerUserID != actorOf(r) {
		mapErr(w, service.ErrPermissionDenied)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) enableMySkill(w http.ResponseWriter, r *http.Request) {
	a.setMySkillEnabled(w, r, true)
}

func (a *API) disableMySkill(w http.ResponseWriter, r *http.Request) {
	a.setMySkillEnabled(w, r, false)
}

func (a *API) setMySkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if err := a.svc.SetUserSkillEnabled(r.Context(), actorOf(r), chi.URLParam(r, "id"), enabled); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteMySkill(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteUserSkill(r.Context(), actorOf(r), chi.URLParam(r, "id")); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- MCP servers ----

type mcpReq struct {
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name,omitempty"`
	Description    string            `json:"description,omitempty"`
	Transport      string            `json:"transport,omitempty"`
	Command        string            `json:"command,omitempty"`
	Args           *[]string         `json:"args,omitempty"`
	URL            string            `json:"url,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	ClearEnv       bool              `json:"clear_env,omitempty"`
	ClearHeaders   bool              `json:"clear_headers,omitempty"`
	DefaultEnabled *bool             `json:"default_enabled,omitempty"`
}

func (req mcpReq) input(actor string) service.MCPServerInput {
	return service.MCPServerInput{
		ID:             req.ID,
		Name:           req.Name,
		Description:    req.Description,
		Transport:      req.Transport,
		Command:        req.Command,
		Args:           req.Args,
		URL:            req.URL,
		Env:            req.Env,
		Headers:        req.Headers,
		ClearEnv:       req.ClearEnv,
		ClearHeaders:   req.ClearHeaders,
		DefaultEnabled: req.DefaultEnabled,
		Actor:          actor,
	}
}

func (a *API) createMCP(w http.ResponseWriter, r *http.Request) {
	var req mcpReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	out, err := a.svc.CreateMCPServer(r.Context(), req.input(actorOf(r)))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *API) listMCPs(w http.ResponseWriter, r *http.Request) {
	onlyEnabled := r.URL.Query().Get("enabled") == "true"
	mcps, err := a.svc.ListMCPServers(r.Context(), onlyEnabled)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcps": mcps})
}

func (a *API) getMCP(w http.ResponseWriter, r *http.Request) {
	mcp, err := a.svc.GetMCPServer(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mcp)
}

func (a *API) updateMCP(w http.ResponseWriter, r *http.Request) {
	var req mcpReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	out, err := a.svc.UpdateMCPServer(r.Context(), chi.URLParam(r, "id"), req.input(actorOf(r)))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) enableMCP(w http.ResponseWriter, r *http.Request) {
	a.setMCPEnabled(w, r, true)
}

func (a *API) disableMCP(w http.ResponseWriter, r *http.Request) {
	a.setMCPEnabled(w, r, false)
}

func (a *API) setMCPEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	mcp, err := a.svc.SetMCPServerEnabled(r.Context(), chi.URLParam(r, "id"), enabled, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mcp)
}

func (a *API) deleteMCP(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteMCPServer(r.Context(), chi.URLParam(r, "id"), actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listEffectiveMCPs(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		mapErr(w, service.ErrInvalidArg)
		return
	}
	cfg, err := a.svc.ListEffectiveMCPRuntimeConfig(r.Context(), userID)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (a *API) listMyMCPs(w http.ResponseWriter, r *http.Request) {
	mcps, err := a.svc.ListUserMCPCatalog(r.Context(), actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcps": mcps})
}

func (a *API) enableMyMCP(w http.ResponseWriter, r *http.Request) {
	a.setMyMCPEnabled(w, r, true)
}

func (a *API) disableMyMCP(w http.ResponseWriter, r *http.Request) {
	a.setMyMCPEnabled(w, r, false)
}

func (a *API) setMyMCPEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if err := a.svc.SetUserMCPEnabled(r.Context(), actorOf(r), chi.URLParam(r, "id"), enabled); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Agent prompts ----

type agentPromptReq struct {
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func (a *API) getGlobalAgentPrompt(w http.ResponseWriter, r *http.Request) {
	prompt, err := a.svc.DefaultAgentPrompt(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prompt)
}

func (a *API) updateGlobalAgentPrompt(w http.ResponseWriter, r *http.Request) {
	var req agentPromptReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	prompt, err := a.svc.UpdateGlobalAgentPrompt(r.Context(), service.AgentPromptInput{
		Name:    req.Name,
		Content: req.Content,
		Enabled: req.Enabled,
		Actor:   actorOf(r),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prompt)
}

func (a *API) enableGlobalAgentPrompt(w http.ResponseWriter, r *http.Request) {
	a.setGlobalAgentPromptEnabled(w, r, true)
}

func (a *API) disableGlobalAgentPrompt(w http.ResponseWriter, r *http.Request) {
	a.setGlobalAgentPromptEnabled(w, r, false)
}

func (a *API) setGlobalAgentPromptEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	prompt, err := a.svc.SetGlobalAgentPromptEnabled(r.Context(), enabled, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prompt)
}

func (a *API) effectiveAgentPrompt(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		mapErr(w, service.ErrInvalidArg)
		return
	}
	cfg, err := a.svc.EffectiveAgentPromptRuntimeConfig(r.Context(), userID)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ---- LLM model configuration ----

type llmProviderReq struct {
	ID      string  `json:"id,omitempty"`
	Name    string  `json:"name,omitempty"`
	Type    string  `json:"type,omitempty"`
	BaseURL string  `json:"base_url,omitempty"`
	APIKey  *string `json:"api_key,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

func (a *API) createLLMProvider(w http.ResponseWriter, r *http.Request) {
	var req llmProviderReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	provider, err := a.svc.CreateLLMProvider(r.Context(), service.LLMProviderInput{
		ID:      req.ID,
		Name:    req.Name,
		Type:    req.Type,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Enabled: req.Enabled,
		Actor:   actorOf(r),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, provider)
}

func (a *API) listLLMProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := a.svc.ListLLMProviders(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (a *API) updateLLMProvider(w http.ResponseWriter, r *http.Request) {
	var req llmProviderReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	provider, err := a.svc.UpdateLLMProvider(r.Context(), chi.URLParam(r, "id"), service.LLMProviderInput{
		Name:    req.Name,
		Type:    req.Type,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Enabled: req.Enabled,
		Actor:   actorOf(r),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (a *API) deleteLLMProvider(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteLLMProvider(r.Context(), chi.URLParam(r, "id"), actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type llmModelReq struct {
	Alias      string `json:"alias,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	RealModel  string `json:"real_model,omitempty"`
	Runtime    string `json:"runtime,omitempty"`
	Label      string `json:"label,omitempty"`
	IconType   string `json:"icon_type,omitempty"`
	IconSlug   string `json:"icon_slug,omitempty"`
	IconURL    string `json:"icon_url,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
	Visible    *bool  `json:"visible,omitempty"`
	IsDefault  bool   `json:"is_default,omitempty"`
	SortOrder  int    `json:"sort_order,omitempty"`
}

func (a *API) createLLMModel(w http.ResponseWriter, r *http.Request) {
	var req llmModelReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	model, err := a.svc.CreateLLMModel(r.Context(), llmModelInput(req, actorOf(r)))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, model)
}

func (a *API) listLLMModels(w http.ResponseWriter, r *http.Request) {
	models, err := a.svc.ListLLMModels(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (a *API) updateLLMModel(w http.ResponseWriter, r *http.Request) {
	var req llmModelReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	model, err := a.svc.UpdateLLMModel(r.Context(), chi.URLParam(r, "alias"), llmModelInput(req, actorOf(r)))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (a *API) deleteLLMModel(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteLLMModel(r.Context(), chi.URLParam(r, "alias"), actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setDefaultLLMModel(w http.ResponseWriter, r *http.Request) {
	model, err := a.svc.SetDefaultLLMModel(r.Context(), chi.URLParam(r, "alias"), actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (a *API) listPublicLLMModels(w http.ResponseWriter, r *http.Request) {
	models, err := a.svc.ListPublicLLMModels(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func llmModelInput(req llmModelReq, actor string) service.LLMModelInput {
	return service.LLMModelInput{
		Alias:      req.Alias,
		ProviderID: req.ProviderID,
		RealModel:  req.RealModel,
		Runtime:    req.Runtime,
		Label:      req.Label,
		IconType:   req.IconType,
		IconSlug:   req.IconSlug,
		IconURL:    req.IconURL,
		Enabled:    req.Enabled,
		Visible:    req.Visible,
		IsDefault:  req.IsDefault,
		SortOrder:  req.SortOrder,
		Actor:      actor,
	}
}

// ---- scheduled tasks ----

type scheduledTaskAttachmentReq struct {
	Filename   string `json:"filename"`
	Mime       string `json:"mime"`
	SizeBytes  int64  `json:"size_bytes"`
	ContentB64 string `json:"content_b64"`
}

type scheduledTaskReq struct {
	OwnerUserID  string                       `json:"owner_user_id,omitempty"`
	Name         string                       `json:"name"`
	Description  string                       `json:"description,omitempty"`
	Status       string                       `json:"status,omitempty"`
	ScheduleKind string                       `json:"schedule_kind"`
	ScheduleSpec json.RawMessage              `json:"schedule_spec"`
	Timezone     string                       `json:"timezone,omitempty"`
	Prompt       string                       `json:"prompt"`
	ModelAlias   string                       `json:"model_alias"`
	ConfigJSON   json.RawMessage              `json:"config_json,omitempty"`
	ExpiresAt    json.RawMessage              `json:"expires_at"`
	Attachments  []scheduledTaskAttachmentReq `json:"attachments,omitempty"`
}

func (a *API) createMyScheduledTask(w http.ResponseWriter, r *http.Request) {
	var req scheduledTaskReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	owner := actorOf(r)
	in, err := scheduledTaskInput(req, owner, false)
	if err != nil {
		mapErr(w, err)
		return
	}
	out, err := a.svc.CreateUserScheduledTask(r.Context(), owner, in)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *API) listScheduledTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := a.svc.ListScheduledTasks(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (a *API) listMyScheduledTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := a.svc.ListUserScheduledTasks(r.Context(), actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (a *API) getScheduledTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.svc.GetScheduledTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (a *API) getMyScheduledTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.svc.GetUserScheduledTask(r.Context(), chi.URLParam(r, "id"), actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (a *API) updateScheduledTask(w http.ResponseWriter, r *http.Request) {
	var req scheduledTaskReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	in, err := scheduledTaskInput(req, actorOf(r), req.Attachments != nil)
	if err != nil {
		mapErr(w, err)
		return
	}
	out, err := a.svc.UpdateScheduledTask(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) updateMyScheduledTask(w http.ResponseWriter, r *http.Request) {
	var req scheduledTaskReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	owner := actorOf(r)
	in, err := scheduledTaskInput(req, owner, req.Attachments != nil)
	if err != nil {
		mapErr(w, err)
		return
	}
	out, err := a.svc.UpdateUserScheduledTask(r.Context(), chi.URLParam(r, "id"), owner, in)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func scheduledTaskInput(req scheduledTaskReq, actor string, replaceAttachments bool) (service.ScheduledTaskInput, error) {
	in := service.ScheduledTaskInput{
		OwnerUserID:        req.OwnerUserID,
		Name:               req.Name,
		Description:        req.Description,
		Status:             req.Status,
		ScheduleKind:       req.ScheduleKind,
		ScheduleSpec:       req.ScheduleSpec,
		Timezone:           req.Timezone,
		Prompt:             req.Prompt,
		ModelAlias:         req.ModelAlias,
		ConfigJSON:         req.ConfigJSON,
		Attachments:        scheduledTaskAttachments(req.Attachments),
		ReplaceAttachments: replaceAttachments,
		Actor:              actor,
	}
	if req.ExpiresAt != nil {
		in.ReplaceExpiresAt = true
		raw := strings.TrimSpace(string(req.ExpiresAt))
		if raw != "" && raw != "null" {
			var value string
			if err := json.Unmarshal(req.ExpiresAt, &value); err != nil {
				return service.ScheduledTaskInput{}, service.ErrInvalidArg
			}
			expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
			if err != nil {
				return service.ScheduledTaskInput{}, service.ErrInvalidArg
			}
			in.ExpiresAt = expiresAt.UTC()
		}
	}
	return in, nil
}

func (a *API) deleteMyScheduledTask(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteUserScheduledTask(r.Context(), chi.URLParam(r, "id"), actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func scheduledTaskAttachments(reqs []scheduledTaskAttachmentReq) []store.ScheduledTaskAttachment {
	out := make([]store.ScheduledTaskAttachment, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, store.ScheduledTaskAttachment{
			Filename:   req.Filename,
			Mime:       req.Mime,
			SizeBytes:  req.SizeBytes,
			ContentB64: req.ContentB64,
		})
	}
	return out
}

func (a *API) deleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteScheduledTask(r.Context(), chi.URLParam(r, "id"), actorOf(r)); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) pauseScheduledTask(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.SetScheduledTaskStatus(r.Context(), chi.URLParam(r, "id"), service.TaskStatusPaused, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) pauseMyScheduledTask(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.SetUserScheduledTaskStatus(r.Context(), chi.URLParam(r, "id"), actorOf(r), service.TaskStatusPaused)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) resumeScheduledTask(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.SetScheduledTaskStatus(r.Context(), chi.URLParam(r, "id"), service.TaskStatusActive, actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) resumeMyScheduledTask(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.SetUserScheduledTaskStatus(r.Context(), chi.URLParam(r, "id"), actorOf(r), service.TaskStatusActive)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) runScheduledTaskNow(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.EnqueueScheduledTaskNow(r.Context(), chi.URLParam(r, "id"), actorOf(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) listScheduledTaskRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.svc.ListScheduledTaskRuns(r.Context(), r.URL.Query().Get("task_id"), r.URL.Query().Get("status"), qInt(r, "limit", 50))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (a *API) getScheduledTaskRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.svc.GetScheduledTaskRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
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

type setSandboxNodeCapacityReq struct {
	MaxSandboxPods *int `json:"max_sandbox_pods"`
}

func (a *API) setSandboxNodeCapacity(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req setSandboxNodeCapacityReq
	if err := decode(r, &req); err != nil {
		mapErr(w, err)
		return
	}
	node, err := a.svc.SetSandboxNodeMaxPods(r.Context(), name, req.MaxSandboxPods, actorOf(r))
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

// ---- sandbox runtimes ----

func (a *API) listSandboxes(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListSandboxes(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) deleteSandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.svc.DeleteSandbox(r.Context(), id); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- token usage ----

func (a *API) tokenUsage(w http.ResponseWriter, r *http.Request) {
	query, ok := tokenUsageQueryFromRequest(w, r)
	if !ok {
		return
	}
	report, err := a.svc.TokenUsageReport(r.Context(), query)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *API) tokenUsageUser(w http.ResponseWriter, r *http.Request) {
	query, ok := tokenUsageQueryFromRequest(w, r)
	if !ok {
		return
	}
	report, err := a.svc.TokenUsageUserReport(r.Context(), chi.URLParam(r, "user_id"), query)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *API) exportTokenUsage(w http.ResponseWriter, r *http.Request) {
	query, ok := tokenUsageQueryFromRequest(w, r)
	if !ok {
		return
	}
	data, filename, err := a.svc.ExportTokenUsageXLSX(r.Context(), query)
	if err != nil {
		mapErr(w, err)
		return
	}
	w.Header().Set("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("content-disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func tokenUsageQueryFromRequest(w http.ResponseWriter, r *http.Request) (store.TokenUsageQuery, bool) {
	q := r.URL.Query()
	var out store.TokenUsageQuery
	var err error
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		out.From, err = parseTokenUsageTime(raw, false)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "from must be RFC3339 or YYYY-MM-DD")
			return store.TokenUsageQuery{}, false
		}
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		out.To, err = parseTokenUsageTime(raw, true)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "to must be RFC3339 or YYYY-MM-DD")
			return store.TokenUsageQuery{}, false
		}
	}
	out.Bucket = q.Get("bucket")
	out.Limit = qInt(r, "limit", 100)
	out.Offset = qInt(r, "offset", 0)
	return out, true
}

func parseTokenUsageTime(raw string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	t = t.UTC()
	if endOfDay {
		return t.AddDate(0, 0, 1), nil
	}
	return t, nil
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

func (a *API) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.AuditEventQuery{
		Limit:        qInt(r, "limit", 100),
		Offset:       qInt(r, "offset", 0),
		ActorUserID:  q.Get("actor_user_id"),
		ActorEmail:   q.Get("actor_email"),
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
		Result:       q.Get("result"),
		RequestID:    q.Get("request_id"),
		TraceID:      q.Get("trace_id"),
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "since must be RFC3339")
			return
		}
		query.Since = t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "until must be RFC3339")
			return
		}
		query.Until = t
	}
	events, err := a.svc.ListAuditEvents(r.Context(), query)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (a *API) getTrace(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "trace_id")
	if traceID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "trace_id is required")
		return
	}
	events, err := a.svc.ListTraceEvents(r.Context(), store.TraceEventQuery{
		TraceID: traceID,
		Limit:   qInt(r, "limit", 500),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	auditEvents, err := a.svc.ListAuditEvents(r.Context(), store.AuditEventQuery{
		TraceID: traceID,
		Limit:   qInt(r, "audit_limit", 100),
	})
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"trace_id":     traceID,
		"events":       events,
		"audit_events": auditEvents,
	})
}
