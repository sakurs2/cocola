// Package service is the admin-api business layer: it composes the token Issuer
// and the store into the operations the HTTP handlers expose, and it is the one
// place that writes the audit log. Handlers stay thin (decode -> call service ->
// encode); the store stays dumb (CRUD). Errors are returned as typed sentinels
// the handler maps to HTTP status codes.
//
// Identity note: the tokens minted here are the SAME cocola-signed HS256 JWTs
// the Python llm-gateway verifies (see internal/token, byte-compatible with the
// gateway's auth/jwt.py). Issuing here, verifying there, with a shared secret,
// is exactly the M4 identity-as-signed-token decision now driven over HTTP.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors mapped to HTTP codes by the handler layer.
var (
	ErrInvalidArg       = errors.New("service: invalid argument")
	ErrUnauthenticated  = errors.New("service: unauthenticated")
	ErrAccountDisabled  = errors.New("service: account disabled")
	ErrProtectedAdmin   = errors.New("service: protected admin")
	ErrSelfPermission   = errors.New("service: self permission change")
	ErrPermissionDenied = errors.New("service: permission denied")
	ErrNotFound         = store.ErrNotFound
	ErrConflict         = store.ErrConflict
)

// Clock is injectable so tests get deterministic timestamps.
type Clock func() time.Time

// Admin is the admin-api service.
type Admin struct {
	store        store.Store
	issuer       *token.Issuer
	now          Clock
	sandboxNodes SandboxNodeManager
}

// New builds the service. issuer may be nil if token minting is disabled (no
// signing secret configured); token endpoints then return ErrInvalidArg.
func New(s store.Store, iss *token.Issuer, now Clock) *Admin {
	if now == nil {
		now = time.Now
	}
	return &Admin{store: s, issuer: iss, now: now}
}

// WithSandboxNodeManager attaches the optional lightweight Kubernetes node
// operations backend. When nil, the HTTP layer returns a clear "not configured"
// error for sandbox-node routes while the rest of admin-api stays usable.
func (a *Admin) WithSandboxNodeManager(m SandboxNodeManager) *Admin {
	a.sandboxNodes = m
	return a
}

// ---- Auth users / whitelist ----

const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	bootstrapActor = "bootstrap"
)

// AuthUserInput describes user creation/update over the admin surface.
type AuthUserInput struct {
	Username string
	Email    string
	Role     string
	Enabled  *bool
	Password string
	Actor    string
}

// LoginResult is the safe identity payload returned to Auth.js. It deliberately
// excludes PasswordHash and any cocola runtime token.
type LoginResult struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Enabled  bool   `json:"enabled"`
}

func isAuthUserUnavailable(u store.AuthUser) bool {
	return !u.Enabled || !u.DeletedAt.IsZero()
}

func isBootstrapAdmin(u store.AuthUser) bool {
	return strings.EqualFold(strings.TrimSpace(u.CreatedBy), bootstrapActor)
}

func isSelfActor(u store.AuthUser, actor string) bool {
	return normalizeEmail(actor) != "" && normalizeEmail(actor) == u.Email
}

// BootstrapAdminInput seeds the first admin from environment variables.
type BootstrapAdminInput struct {
	Username     string
	Email        string
	Password     string
	PasswordHash string
	Reset        bool
	Actor        string
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func normalizeIdentifier(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}

func defaultUsername(email string) string {
	local, _, ok := strings.Cut(email, "@")
	if !ok {
		local = email
	}
	local = normalizeUsername(local)
	if local == "" {
		return "user"
	}
	return local
}

func validRole(role string) bool {
	return role == RoleUser || role == RoleAdmin
}

func publicUser(u store.AuthUser) LoginResult {
	name := u.Name
	if name == "" {
		name = u.Username
	}
	if name == "" {
		name = u.Email
	}
	return LoginResult{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		Name:     name,
		Role:     u.Role,
		Enabled:  u.Enabled,
	}
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func hashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", ErrInvalidArg
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (a *Admin) CreateAuthUser(ctx context.Context, in AuthUserInput) (store.AuthUser, error) {
	email := normalizeEmail(in.Email)
	username := normalizeUsername(in.Username)
	if username == "" {
		username = defaultUsername(email)
	}
	role := in.Role
	if role == "" {
		role = RoleUser
	}
	if email == "" || username == "" || !validRole(role) || strings.TrimSpace(in.Password) == "" {
		return store.AuthUser{}, ErrInvalidArg
	}
	pw, err := hashPassword(in.Password)
	if err != nil {
		return store.AuthUser{}, err
	}
	now := a.now().UTC()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	u := store.AuthUser{
		ID:              newID(),
		Username:        username,
		Email:           email,
		Name:            username,
		Role:            role,
		Enabled:         enabled,
		PasswordHash:    pw,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       in.Actor,
		UpdatedBy:       in.Actor,
		PasswordUpdated: now,
	}
	if err := a.store.CreateAuthUser(ctx, u); err != nil {
		return store.AuthUser{}, err
	}
	a.audit(ctx, in.Actor, "auth_user.create", u.ID, "email="+u.Email+" role="+u.Role)
	return u, nil
}

func (a *Admin) ListAuthUsers(ctx context.Context) ([]store.AuthUser, error) {
	return a.store.ListAuthUsers(ctx)
}

func (a *Admin) GetAuthUserByEmail(ctx context.Context, email string) (store.AuthUser, error) {
	normalized := normalizeEmail(email)
	if normalized == "" {
		return store.AuthUser{}, ErrInvalidArg
	}
	return a.store.GetAuthUserByEmail(ctx, normalized)
}

func (a *Admin) GetAuthUserByIdentifier(ctx context.Context, identifier string) (store.AuthUser, error) {
	normalized := normalizeIdentifier(identifier)
	if normalized == "" {
		return store.AuthUser{}, ErrInvalidArg
	}
	return a.store.GetAuthUserByIdentifier(ctx, normalized)
}

func (a *Admin) SetAuthUser(ctx context.Context, id string, in AuthUserInput) (store.AuthUser, error) {
	if id == "" {
		return store.AuthUser{}, ErrInvalidArg
	}
	u, err := a.store.GetAuthUser(ctx, id)
	if err != nil {
		return store.AuthUser{}, err
	}
	if !u.DeletedAt.IsZero() {
		return store.AuthUser{}, ErrNotFound
	}
	if email := normalizeEmail(in.Email); email != "" {
		u.Email = email
	}
	if username := normalizeUsername(in.Username); username != "" {
		u.Username = username
	}
	u.Name = u.Username
	if in.Role != "" {
		if isSelfActor(u, in.Actor) {
			return store.AuthUser{}, ErrSelfPermission
		}
		if !validRole(in.Role) {
			return store.AuthUser{}, ErrInvalidArg
		}
		if isBootstrapAdmin(u) && in.Role != RoleAdmin {
			return store.AuthUser{}, ErrProtectedAdmin
		}
		u.Role = in.Role
	}
	if in.Enabled != nil {
		if isSelfActor(u, in.Actor) {
			return store.AuthUser{}, ErrSelfPermission
		}
		if isBootstrapAdmin(u) && !*in.Enabled {
			return store.AuthUser{}, ErrProtectedAdmin
		}
		u.Enabled = *in.Enabled
	}
	u.UpdatedAt = a.now().UTC()
	u.UpdatedBy = in.Actor
	if err := a.store.UpdateAuthUser(ctx, u); err != nil {
		return store.AuthUser{}, err
	}
	a.audit(ctx, in.Actor, "auth_user.update", u.ID, "email="+u.Email+" role="+u.Role+" enabled="+strconv.FormatBool(u.Enabled))
	return u, nil
}

func (a *Admin) ResetAuthUserPassword(ctx context.Context, id, password, actor string) (store.AuthUser, error) {
	if id == "" {
		return store.AuthUser{}, ErrInvalidArg
	}
	u, err := a.store.GetAuthUser(ctx, id)
	if err != nil {
		return store.AuthUser{}, err
	}
	if !u.DeletedAt.IsZero() {
		return store.AuthUser{}, ErrNotFound
	}
	pw, err := hashPassword(password)
	if err != nil {
		return store.AuthUser{}, err
	}
	now := a.now().UTC()
	u.PasswordHash = pw
	u.PasswordUpdated = now
	u.UpdatedAt = now
	u.UpdatedBy = actor
	if err := a.store.UpdateAuthUser(ctx, u); err != nil {
		return store.AuthUser{}, err
	}
	a.audit(ctx, actor, "auth_user.password_reset", u.ID, "email="+u.Email)
	return u, nil
}

func (a *Admin) DeleteAuthUser(ctx context.Context, id, actor string) error {
	if id == "" {
		return ErrInvalidArg
	}
	u, err := a.store.GetAuthUser(ctx, id)
	if err != nil {
		return err
	}
	if !u.DeletedAt.IsZero() {
		return ErrNotFound
	}
	if isSelfActor(u, actor) {
		return ErrSelfPermission
	}
	if isBootstrapAdmin(u) {
		return ErrProtectedAdmin
	}
	now := a.now().UTC()
	if err := a.store.DeleteAuthUser(ctx, id, actor, now); err != nil {
		return err
	}
	a.audit(ctx, actor, "auth_user.delete", id, "email="+u.Email)
	return nil
}

func (a *Admin) Authenticate(ctx context.Context, identifier, password string) (LoginResult, error) {
	u, err := a.GetAuthUserByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return LoginResult{}, ErrUnauthenticated
		}
		return LoginResult{}, err
	}
	if isAuthUserUnavailable(u) {
		return LoginResult{}, ErrAccountDisabled
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return LoginResult{}, ErrUnauthenticated
	}
	_ = a.store.TouchAuthUserLogin(ctx, u.ID, a.now().UTC())
	return publicUser(u), nil
}

func (a *Admin) BootstrapAdmin(ctx context.Context, in BootstrapAdminInput) error {
	email := normalizeEmail(in.Email)
	if email == "" {
		return nil
	}
	username := normalizeUsername(in.Username)
	if username == "" {
		username = defaultUsername(email)
	}
	if in.Actor == "" {
		in.Actor = bootstrapActor
	}
	existing, err := a.store.GetAuthUserByEmail(ctx, email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && !existing.DeletedAt.IsZero() {
		return nil
	}

	hash := strings.TrimSpace(in.PasswordHash)
	if hash == "" && strings.TrimSpace(in.Password) != "" {
		var hErr error
		hash, hErr = hashPassword(in.Password)
		if hErr != nil {
			return hErr
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		if hash == "" {
			return ErrInvalidArg
		}
		now := a.now().UTC()
		u := store.AuthUser{
			ID:              newID(),
			Username:        username,
			Email:           email,
			Name:            username,
			Role:            RoleAdmin,
			Enabled:         true,
			PasswordHash:    hash,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       bootstrapActor,
			UpdatedBy:       in.Actor,
			PasswordUpdated: now,
		}
		if err := a.store.CreateAuthUser(ctx, u); err != nil {
			return err
		}
		a.audit(ctx, in.Actor, "auth_user.bootstrap", u.ID, "email="+u.Email)
		return nil
	}
	if !in.Reset {
		return nil
	}
	if hash == "" {
		return ErrInvalidArg
	}
	now := a.now().UTC()
	existing.Role = RoleAdmin
	existing.Username = username
	existing.Name = username
	existing.Enabled = true
	existing.CreatedBy = bootstrapActor
	existing.PasswordHash = hash
	existing.PasswordUpdated = now
	existing.UpdatedAt = now
	existing.UpdatedBy = in.Actor
	if err := a.store.UpdateAuthUser(ctx, existing); err != nil {
		return err
	}
	a.audit(ctx, in.Actor, "auth_user.bootstrap_reset", existing.ID, "email="+existing.Email)
	return nil
}

func (a *Admin) IssueRuntimeToken(ctx context.Context, userID, tenant string, ttl time.Duration) (string, error) {
	if a.issuer == nil {
		return "", ErrInvalidArg
	}
	if strings.TrimSpace(userID) == "" {
		return "", ErrInvalidArg
	}
	if strings.Contains(userID, "@") {
		u, err := a.store.GetAuthUserByEmail(ctx, normalizeEmail(userID))
		if err != nil {
			return "", err
		}
		if isAuthUserUnavailable(u) {
			return "", ErrAccountDisabled
		}
		userID = u.Email
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	tok, _, err := a.issuer.Issue(strings.TrimSpace(userID), strings.TrimSpace(tenant), ttl, a.now().Unix())
	return tok, err
}

// ---- Tokens ----

// IssueTokenInput describes a mint request.
type IssueTokenInput struct {
	UserID string
	Tenant string
	TTL    time.Duration // 0 => issuer default; negative => non-expiring
	Actor  string        // admin principal, for audit
}

// IssueTokenResult returns the bearer token plus its stored metadata. The token
// string is returned ONCE here and never persisted.
type IssueTokenResult struct {
	Token  string            `json:"token"`
	Record store.TokenRecord `json:"record"`
}

// IssueToken mints a signed token, stores its metadata, and audits the action.
func (a *Admin) IssueToken(ctx context.Context, in IssueTokenInput) (IssueTokenResult, error) {
	if a.issuer == nil {
		return IssueTokenResult{}, ErrInvalidArg
	}
	if in.UserID == "" {
		return IssueTokenResult{}, ErrInvalidArg
	}
	nowUnix := a.now().Unix()
	tok, claims, err := a.issuer.Issue(in.UserID, in.Tenant, in.TTL, nowUnix)
	if err != nil {
		return IssueTokenResult{}, ErrInvalidArg
	}
	rec := store.TokenRecord{
		// Use the token's own jti as the record id so the denylist key the
		// gateway reads back from a verified token matches this record exactly.
		ID:        claims.ID,
		UserID:    claims.Subject,
		TenantID:  claims.Tenant,
		Issuer:    claims.Issuer,
		IssuedAt:  time.Unix(claims.IssuedAt, 0).UTC(),
		CreatedBy: in.Actor,
	}
	if claims.Expires != 0 {
		rec.ExpiresAt = time.Unix(claims.Expires, 0).UTC()
	}
	if err := a.store.CreateToken(ctx, rec); err != nil {
		return IssueTokenResult{}, err
	}
	a.audit(ctx, in.Actor, "token.issue", rec.ID, "user="+in.UserID+" tenant="+in.Tenant)
	return IssueTokenResult{Token: tok, Record: rec}, nil
}

// ListTokens returns minted-token metadata, optionally filtered by user.
func (a *Admin) ListTokens(ctx context.Context, userID string) ([]store.TokenRecord, error) {
	return a.store.ListTokens(ctx, userID)
}

// RevokeToken marks a token revoked so the gateway's denylist check rejects it.
func (a *Admin) RevokeToken(ctx context.Context, id, actor string) error {
	if id == "" {
		return ErrInvalidArg
	}
	if err := a.store.RevokeToken(ctx, id, a.now().UTC()); err != nil {
		return err
	}
	a.audit(ctx, actor, "token.revoke", id, "")
	return nil
}

// RevokedIDs returns the set of revoked token ids — the denylist the gateway
// polls/consults to reject revoked-but-unexpired tokens.
func (a *Admin) RevokedIDs(ctx context.Context) ([]string, error) {
	all, err := a.store.ListTokens(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, r := range all {
		if r.Revoked {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

// ---- Quota overrides ----

// SetQuota upserts a per-subject cap. scope must be "user" or "tenant".
func (a *Admin) SetQuota(ctx context.Context, scope, subject string, limit int64, actor string) (store.QuotaOverride, error) {
	if (scope != "user" && scope != "tenant") || subject == "" || limit < 0 {
		return store.QuotaOverride{}, ErrInvalidArg
	}
	q := store.QuotaOverride{Scope: scope, Subject: subject, Limit: limit, UpdatedAt: a.now().UTC(), UpdatedBy: actor}
	if err := a.store.SetQuota(ctx, q); err != nil {
		return store.QuotaOverride{}, err
	}
	a.audit(ctx, actor, "quota.set", scope+"/"+subject, "limit="+strconv.FormatInt(limit, 10))
	return q, nil
}

// ListQuotas returns all overrides.
func (a *Admin) ListQuotas(ctx context.Context) ([]store.QuotaOverride, error) {
	return a.store.ListQuotas(ctx)
}

// DeleteQuota removes an override (subject falls back to the static env cap).
func (a *Admin) DeleteQuota(ctx context.Context, scope, subject, actor string) error {
	if err := a.store.DeleteQuota(ctx, scope, subject); err != nil {
		return err
	}
	a.audit(ctx, actor, "quota.delete", scope+"/"+subject, "")
	return nil
}

// ---- Skills ----

// CreateSkill registers a new skill (disabled by default unless Enabled set).
func (a *Admin) CreateSkill(ctx context.Context, s store.Skill, actor string) (store.Skill, error) {
	if s.ID == "" || s.Name == "" {
		return store.Skill{}, ErrInvalidArg
	}
	now := a.now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now
	if err := a.store.CreateSkill(ctx, s); err != nil {
		return store.Skill{}, err
	}
	a.audit(ctx, actor, "skill.create", s.ID, "name="+s.Name)
	return s, nil
}

// ListSkills returns the catalog; onlyEnabled filters to enabled entries.
func (a *Admin) ListSkills(ctx context.Context, onlyEnabled bool) ([]store.Skill, error) {
	return a.store.ListSkills(ctx, onlyEnabled)
}

// GetSkill fetches one entry.
func (a *Admin) GetSkill(ctx context.Context, id string) (store.Skill, error) {
	return a.store.GetSkill(ctx, id)
}

// SetSkillEnabled flips a skill on/off without touching other fields.
func (a *Admin) SetSkillEnabled(ctx context.Context, id string, enabled bool, actor string) (store.Skill, error) {
	s, err := a.store.GetSkill(ctx, id)
	if err != nil {
		return store.Skill{}, err
	}
	s.Enabled = enabled
	s.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateSkill(ctx, s); err != nil {
		return store.Skill{}, err
	}
	action := "skill.disable"
	if enabled {
		action = "skill.enable"
	}
	a.audit(ctx, actor, action, id, "")
	return s, nil
}

// DeleteSkill removes a skill from the catalog.
func (a *Admin) DeleteSkill(ctx context.Context, id, actor string) error {
	if err := a.store.DeleteSkill(ctx, id); err != nil {
		return err
	}
	a.audit(ctx, actor, "skill.delete", id, "")
	return nil
}

// ---- Audit ----

// ListAudit returns the most recent audit entries.
func (a *Admin) ListAudit(ctx context.Context, limit int) ([]store.AuditEntry, error) {
	return a.store.ListAudit(ctx, limit)
}

// audit appends one entry best-effort; an audit-write failure must not fail the
// underlying operation (it already succeeded), so it is intentionally ignored.
func (a *Admin) audit(ctx context.Context, actor, action, resource, detail string) {
	_ = a.store.AppendAudit(ctx, store.AuditEntry{
		At:       a.now().UTC(),
		Actor:    actor,
		Action:   action,
		Resource: resource,
		Detail:   detail,
	})
}
