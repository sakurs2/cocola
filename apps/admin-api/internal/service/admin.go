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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors mapped to HTTP codes by the handler layer.
var (
	ErrInvalidArg          = errors.New("service: invalid argument")
	ErrUnauthenticated     = errors.New("service: unauthenticated")
	ErrAccountDisabled     = errors.New("service: account disabled")
	ErrProtectedAdmin      = errors.New("service: protected admin")
	ErrSelfPermission      = errors.New("service: self permission change")
	ErrPermissionDenied    = errors.New("service: permission denied")
	ErrScheduleTooFrequent = errors.New("service: schedule frequency below minimum")
	ErrScheduleInPast      = errors.New("service: schedule time is in the past")
	ErrNotFound            = store.ErrNotFound
	ErrConflict            = store.ErrConflict
)

// Clock is injectable so tests get deterministic timestamps.
type Clock func() time.Time

// Admin is the admin-api service.
type Admin struct {
	store               store.Store
	issuer              *token.Issuer
	now                 Clock
	skillBundles        SkillBundleStore
	sandboxNodes        SandboxNodeManager
	sandboxRuntimes     SandboxRuntimeManager
	userEvents          UserEventBroker
	modelSecretKey      string
	configSecretKey     string
	minScheduleInterval time.Duration
	schedulerStarted    atomic.Bool
}

// New builds the service. issuer may be nil if token minting is disabled (no
// signing secret configured); token endpoints then return ErrInvalidArg.
func New(s store.Store, iss *token.Issuer, now Clock) *Admin {
	if now == nil {
		now = time.Now
	}
	return &Admin{store: s, issuer: iss, now: now}
}

type SkillBundleStore interface {
	PutBytes(ctx context.Context, key string, data []byte, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, string, error)
}

func (a *Admin) WithSkillBundleStore(store SkillBundleStore) *Admin {
	a.skillBundles = store
	return a
}

// WithSandboxNodeManager attaches the optional lightweight Kubernetes node
// operations backend. When nil, the HTTP layer returns a clear "not configured"
// error for sandbox-node routes while the rest of admin-api stays usable.
func (a *Admin) WithSandboxNodeManager(m SandboxNodeManager) *Admin {
	a.sandboxNodes = m
	return a
}

// WithSandboxRuntimeManager attaches the optional read-only sandbox runtime
// monitor backend. When nil, the HTTP layer returns "not configured" while the
// rest of admin-api remains usable.
func (a *Admin) WithSandboxRuntimeManager(m SandboxRuntimeManager) *Admin {
	a.sandboxRuntimes = m
	return a
}

// WithModelSecretKey configures API-key encryption for admin-managed LLM
// providers. Without it, provider saves that include a plaintext API key fail.
func (a *Admin) WithModelSecretKey(secret string) *Admin {
	a.modelSecretKey = strings.TrimSpace(secret)
	return a
}

// WithConfigSecretKey configures encryption for administrator-managed runtime
// configuration secrets such as MCP env/header values. Empty falls back to the
// legacy model secret for compatibility.
func (a *Admin) WithConfigSecretKey(secret string) *Admin {
	a.configSecretKey = strings.TrimSpace(secret)
	return a
}

func (a *Admin) configSecret() string {
	if strings.TrimSpace(a.configSecretKey) != "" {
		return strings.TrimSpace(a.configSecretKey)
	}
	return strings.TrimSpace(a.modelSecretKey)
}

func (a *Admin) WithMinScheduleInterval(d time.Duration) *Admin {
	if d > 0 {
		a.minScheduleInterval = d
	}
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
	s.CreatedBy = actor
	s.UpdatedBy = actor
	if s.Scope == "" {
		s.Scope = "admin"
	}
	if s.SourceType == "" {
		s.SourceType = "manual"
	}
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

func (a *Admin) ListEffectiveSkills(ctx context.Context, userID string) ([]store.Skill, error) {
	adminSkills, err := a.store.ListSkills(ctx, true)
	if err != nil {
		return nil, err
	}
	prefs, err := a.store.ListUserSkillPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	prefMap := map[string]bool{}
	for _, pref := range prefs {
		prefMap[pref.SkillID] = pref.Enabled
	}
	out := make([]store.Skill, 0)
	for _, s := range adminSkills {
		if s.Scope != "" && s.Scope != "admin" {
			continue
		}
		if enabled, ok := prefMap[s.ID]; ok && !enabled {
			continue
		}
		out = append(out, s)
	}
	userSkills, err := a.store.ListSkillsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, s := range userSkills {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

func (a *Admin) ListUserSkillCatalog(ctx context.Context, userID string) ([]store.Skill, error) {
	adminSkills, err := a.store.ListSkills(ctx, false)
	if err != nil {
		return nil, err
	}
	prefs, err := a.store.ListUserSkillPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	prefMap := map[string]bool{}
	for _, pref := range prefs {
		prefMap[pref.SkillID] = pref.Enabled
	}
	out := make([]store.Skill, 0)
	for _, s := range adminSkills {
		if s.Scope != "" && s.Scope != "admin" {
			continue
		}
		if enabled, ok := prefMap[s.ID]; ok {
			s.Enabled = enabled
		}
		out = append(out, s)
	}
	userSkills, err := a.store.ListSkillsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out = append(out, userSkills...)
	return out, nil
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
	s.UpdatedBy = actor
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

func (a *Admin) ScanSkillArchive(ctx context.Context, archive []byte) ([]SkillImportCandidate, error) {
	_ = ctx
	return parseSkillArchive(archive)
}

func (a *Admin) ImportSkillArchive(ctx context.Context, scope, ownerUserID, actor string, archive []byte, selectedIDs []string) ([]store.Skill, []SkillImportCandidate, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "admin"
	}
	if scope != "admin" && scope != "user" {
		return nil, nil, ErrInvalidArg
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	if scope == "user" && ownerUserID == "" {
		return nil, nil, ErrInvalidArg
	}
	candidates, err := parseSkillArchive(archive)
	if err != nil {
		return nil, nil, err
	}
	selected := map[string]bool{}
	for _, id := range selectedIDs {
		id = sanitizeSkillID(id)
		if id != "" {
			selected[id] = true
		}
	}
	allSelected := len(selected) == 0
	imported := make([]store.Skill, 0)
	for i := range candidates {
		c := &candidates[i]
		if !c.Valid || (!allSelected && !selected[c.ID]) {
			continue
		}
		skillID := c.ID
		if scope == "user" {
			skillID = userSkillID(ownerUserID, c.ID)
		}
		objectKey := fmt.Sprintf("skills/%s/%s/%s.zip", scope, skillID, c.ContentSHA256)
		if a.skillBundles != nil {
			if err := a.skillBundles.PutBytes(ctx, objectKey, c.Bundle, "application/zip"); err != nil {
				return nil, candidates, err
			}
			c.BundleObjectKey = objectKey
		}
		now := a.now().UTC()
		s := store.Skill{
			ID:              skillID,
			Name:            c.Name,
			Description:     c.Description,
			Version:         c.Version,
			Entrypoint:      "$CLAUDE_CONFIG_DIR/skills/" + skillID,
			Enabled:         true,
			Scope:           scope,
			OwnerUserID:     ownerUserID,
			SourceType:      "archive",
			SourcePath:      c.Path,
			BundleObjectKey: c.BundleObjectKey,
			ContentSHA256:   c.ContentSHA256,
			ManifestJSON:    skillManifestJSON(*c),
			FrontmatterJSON: skillFrontmatterJSON(*c),
			SkillMD:         c.SkillMD,
			FileCount:       c.FileCount,
			SizeBytes:       c.SizeBytes,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       actor,
			UpdatedBy:       actor,
		}
		existing, err := a.store.GetSkill(ctx, skillID)
		switch {
		case err == nil:
			if existing.Scope != "" && existing.Scope != scope {
				return nil, candidates, ErrConflict
			}
			if scope == "user" && existing.OwnerUserID != ownerUserID {
				return nil, candidates, ErrConflict
			}
			s.CreatedAt = existing.CreatedAt
			s.CreatedBy = existing.CreatedBy
			if err := a.store.UpdateSkill(ctx, s); err != nil {
				return nil, candidates, err
			}
		case errors.Is(err, store.ErrNotFound):
			if err := a.store.CreateSkill(ctx, s); err != nil {
				return nil, candidates, err
			}
		default:
			return nil, candidates, err
		}
		imported = append(imported, s)
	}
	if len(imported) == 0 {
		return nil, candidates, ErrInvalidArg
	}
	a.audit(ctx, actor, "skill.import", scope, "count="+strconv.Itoa(len(imported)))
	return imported, candidates, nil
}

type SkillGitInput struct {
	RepoURL     string
	Ref         string
	Path        string
	SelectedIDs []string
}

func (a *Admin) ScanSkillGit(ctx context.Context, in SkillGitInput) ([]SkillImportCandidate, error) {
	archive, err := skillArchiveFromGit(ctx, in.RepoURL, in.Ref, in.Path)
	if err != nil {
		return nil, err
	}
	return parseSkillArchive(archive)
}

func (a *Admin) ImportSkillGit(ctx context.Context, scope, ownerUserID, actor string, in SkillGitInput) ([]store.Skill, []SkillImportCandidate, error) {
	archive, err := skillArchiveFromGit(ctx, in.RepoURL, in.Ref, in.Path)
	if err != nil {
		return nil, nil, err
	}
	imported, candidates, err := a.ImportSkillArchive(ctx, scope, ownerUserID, actor, archive, in.SelectedIDs)
	if err != nil {
		return nil, candidates, err
	}
	for i := range imported {
		s := imported[i]
		s.SourceType = "git"
		s.SourceURL = strings.TrimSpace(in.RepoURL)
		s.SourceRef = strings.TrimSpace(in.Ref)
		s.SourcePath = strings.Trim(strings.TrimSpace(in.Path), "/")
		if s.SourcePath == "" {
			s.SourcePath = "skills"
		}
		s.UpdatedAt = a.now().UTC()
		s.UpdatedBy = actor
		if err := a.store.UpdateSkill(ctx, s); err != nil {
			return nil, candidates, err
		}
		imported[i] = s
	}
	a.audit(ctx, actor, "skill.import_git", scope, "count="+strconv.Itoa(len(imported)))
	return imported, candidates, nil
}

func (a *Admin) SetUserSkillEnabled(ctx context.Context, userID, skillID string, enabled bool) error {
	s, err := a.store.GetSkill(ctx, skillID)
	if err != nil {
		return err
	}
	now := a.now().UTC()
	if s.Scope == "user" {
		if s.OwnerUserID != userID {
			return ErrNotFound
		}
		s.Enabled = enabled
		s.UpdatedAt = now
		s.UpdatedBy = userID
		return a.store.UpdateSkill(ctx, s)
	}
	return a.store.SetUserSkillPreference(ctx, store.UserSkillPreference{
		UserID:    userID,
		SkillID:   skillID,
		Enabled:   enabled,
		UpdatedAt: now,
	})
}

func (a *Admin) DeleteUserSkill(ctx context.Context, userID, skillID string) error {
	s, err := a.store.GetSkill(ctx, skillID)
	if err != nil {
		return err
	}
	if s.Scope != "user" || s.OwnerUserID != userID {
		return ErrPermissionDenied
	}
	return a.store.DeleteSkill(ctx, skillID)
}

func (a *Admin) GetSkillBundle(ctx context.Context, id string) ([]byte, string, error) {
	if a.skillBundles == nil {
		return nil, "", ErrInvalidArg
	}
	s, err := a.store.GetSkill(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if s.BundleObjectKey == "" {
		return nil, "", ErrNotFound
	}
	return a.skillBundles.GetBytes(ctx, s.BundleObjectKey)
}

func userSkillID(userID, skillID string) string {
	sum := sha256.Sum256([]byte(userID))
	return "user-" + hex.EncodeToString(sum[:4]) + "-" + sanitizeSkillID(skillID)
}

// ---- LLM model configuration ----

const (
	ProviderAnthropic    = "anthropic"
	ProviderOpenAICompat = "openai_compat"
	ProviderFake         = "fake"
	RuntimeClaudeCode    = "claude-code"
	IconSimpleIcons      = "simple-icons"
	IconImage            = "image"
)

type LLMProviderInput struct {
	ID      string
	Name    string
	Type    string
	BaseURL string
	APIKey  *string
	Enabled *bool
	Actor   string
}

type LLMModelInput struct {
	Alias      string
	ProviderID string
	RealModel  string
	Runtime    string
	Label      string
	IconType   string
	IconSlug   string
	IconURL    string
	Enabled    *bool
	Visible    *bool
	IsDefault  bool
	SortOrder  int
	Actor      string
}

func (a *Admin) CreateLLMProvider(ctx context.Context, in LLMProviderInput) (store.LLMProvider, error) {
	id := normalizeID(in.ID)
	ptype := normalizeProviderType(in.Type)
	if id == "" || strings.TrimSpace(in.Name) == "" || !validProviderType(ptype) {
		return store.LLMProvider{}, ErrInvalidArg
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := a.now().UTC()
	provider := store.LLMProvider{
		ID:        id,
		Name:      strings.TrimSpace(in.Name),
		Type:      ptype,
		BaseURL:   strings.TrimSpace(in.BaseURL),
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.applyProviderAPIKey(&provider, in.APIKey); err != nil {
		return store.LLMProvider{}, err
	}
	if err := validateProviderReady(provider); err != nil {
		return store.LLMProvider{}, err
	}
	if err := a.store.CreateLLMProvider(ctx, provider); err != nil {
		return store.LLMProvider{}, err
	}
	a.audit(ctx, in.Actor, "llm_provider.create", provider.ID, "type="+provider.Type)
	return provider, nil
}

func (a *Admin) ListLLMProviders(ctx context.Context) ([]store.LLMProvider, error) {
	return a.store.ListLLMProviders(ctx)
}

func (a *Admin) UpdateLLMProvider(ctx context.Context, id string, in LLMProviderInput) (store.LLMProvider, error) {
	provider, err := a.store.GetLLMProvider(ctx, normalizeID(id))
	if err != nil {
		return store.LLMProvider{}, err
	}
	if strings.TrimSpace(in.Name) != "" {
		provider.Name = strings.TrimSpace(in.Name)
	}
	if in.Type != "" {
		ptype := normalizeProviderType(in.Type)
		if !validProviderType(ptype) {
			return store.LLMProvider{}, ErrInvalidArg
		}
		provider.Type = ptype
	}
	if in.BaseURL != "" || provider.Type == ProviderFake {
		provider.BaseURL = strings.TrimSpace(in.BaseURL)
	}
	if in.Enabled != nil {
		provider.Enabled = *in.Enabled
	}
	if err := a.applyProviderAPIKey(&provider, in.APIKey); err != nil {
		return store.LLMProvider{}, err
	}
	if err := validateProviderReady(provider); err != nil {
		return store.LLMProvider{}, err
	}
	provider.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateLLMProvider(ctx, provider); err != nil {
		return store.LLMProvider{}, err
	}
	a.audit(ctx, in.Actor, "llm_provider.update", provider.ID, "enabled="+strconv.FormatBool(provider.Enabled))
	return provider, nil
}

func (a *Admin) DeleteLLMProvider(ctx context.Context, id, actor string) error {
	id = normalizeID(id)
	if err := a.store.DeleteLLMProvider(ctx, id); err != nil {
		return err
	}
	a.audit(ctx, actor, "llm_provider.delete", id, "")
	return nil
}

func (a *Admin) CreateLLMModel(ctx context.Context, in LLMModelInput) (store.LLMModelRoute, error) {
	route, err := a.llmRouteFromInput(store.LLMModelRoute{}, in, true)
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	if _, err := a.store.GetLLMProvider(ctx, route.ProviderID); err != nil {
		return store.LLMModelRoute{}, err
	}
	if err := a.store.CreateLLMModelRoute(ctx, route); err != nil {
		return store.LLMModelRoute{}, err
	}
	a.audit(ctx, in.Actor, "llm_model.create", route.Alias, "provider="+route.ProviderID)
	return route, nil
}

func (a *Admin) ListLLMModels(ctx context.Context) ([]store.LLMModelRoute, error) {
	return a.store.ListLLMModelRoutes(ctx)
}

func (a *Admin) UpdateLLMModel(ctx context.Context, alias string, in LLMModelInput) (store.LLMModelRoute, error) {
	existing, err := a.store.GetLLMModelRoute(ctx, normalizeID(alias))
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	route, err := a.llmRouteFromInput(existing, in, false)
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	if _, err := a.store.GetLLMProvider(ctx, route.ProviderID); err != nil {
		return store.LLMModelRoute{}, err
	}
	if err := a.store.UpdateLLMModelRoute(ctx, route); err != nil {
		return store.LLMModelRoute{}, err
	}
	a.audit(ctx, in.Actor, "llm_model.update", route.Alias, "enabled="+strconv.FormatBool(route.Enabled))
	return route, nil
}

func (a *Admin) DeleteLLMModel(ctx context.Context, alias, actor string) error {
	alias = normalizeID(alias)
	if err := a.store.DeleteLLMModelRoute(ctx, alias); err != nil {
		return err
	}
	a.audit(ctx, actor, "llm_model.delete", alias, "")
	return nil
}

func (a *Admin) SetDefaultLLMModel(ctx context.Context, alias, actor string) (store.LLMModelRoute, error) {
	route, err := a.store.GetLLMModelRoute(ctx, normalizeID(alias))
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	if !route.Enabled || !route.Visible {
		return store.LLMModelRoute{}, ErrInvalidArg
	}
	route.IsDefault = true
	route.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateLLMModelRoute(ctx, route); err != nil {
		return store.LLMModelRoute{}, err
	}
	a.audit(ctx, actor, "llm_model.default", route.Alias, "")
	return route, nil
}

func (a *Admin) ListPublicLLMModels(ctx context.Context) ([]store.PublicLLMModel, error) {
	providers, err := a.store.ListLLMProviders(ctx)
	if err != nil {
		return nil, err
	}
	enabledProviders := map[string]bool{}
	for _, p := range providers {
		enabledProviders[p.ID] = p.Enabled
	}
	routes, err := a.store.ListLLMModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]store.PublicLLMModel, 0, len(routes))
	for _, route := range routes {
		if !route.Enabled || !route.Visible || !enabledProviders[route.ProviderID] {
			continue
		}
		out = append(out, publicLLMModel(route))
	}
	return out, nil
}

func (a *Admin) llmRouteFromInput(existing store.LLMModelRoute, in LLMModelInput, create bool) (store.LLMModelRoute, error) {
	route := existing
	if create {
		route.Alias = normalizeID(in.Alias)
		route.Enabled = true
		route.Visible = true
		route.Runtime = RuntimeClaudeCode
		route.IconType = IconSimpleIcons
		now := a.now().UTC()
		route.CreatedAt = now
		route.UpdatedAt = now
	}
	if route.Alias == "" {
		return store.LLMModelRoute{}, ErrInvalidArg
	}
	if in.ProviderID != "" || create {
		route.ProviderID = normalizeID(in.ProviderID)
	}
	if in.RealModel != "" || create {
		route.RealModel = strings.TrimSpace(in.RealModel)
	}
	if in.Runtime != "" {
		route.Runtime = strings.TrimSpace(in.Runtime)
	}
	if route.Runtime == "" {
		route.Runtime = RuntimeClaudeCode
	}
	if in.Label != "" || create {
		route.Label = strings.TrimSpace(in.Label)
	}
	if route.Label == "" {
		route.Label = route.Alias
	}
	if in.IconType != "" || create {
		route.IconType = strings.TrimSpace(in.IconType)
	}
	if in.IconSlug != "" || create {
		route.IconSlug = strings.TrimSpace(in.IconSlug)
	}
	if in.IconURL != "" || create {
		route.IconURL = strings.TrimSpace(in.IconURL)
	}
	if in.Enabled != nil {
		route.Enabled = *in.Enabled
	}
	if in.Visible != nil {
		route.Visible = *in.Visible
	}
	route.IsDefault = in.IsDefault
	route.SortOrder = in.SortOrder
	route.UpdatedAt = a.now().UTC()
	if route.ProviderID == "" || route.RealModel == "" || !validIcon(route) {
		return store.LLMModelRoute{}, ErrInvalidArg
	}
	if route.IsDefault && (!route.Enabled || !route.Visible) {
		return store.LLMModelRoute{}, ErrInvalidArg
	}
	return route, nil
}

func (a *Admin) applyProviderAPIKey(provider *store.LLMProvider, apiKey *string) error {
	if apiKey == nil {
		return nil
	}
	key := strings.TrimSpace(*apiKey)
	if key == "" {
		provider.APIKeyCiphertext = ""
		provider.APIKeyHint = ""
		return nil
	}
	ciphertext, err := encryptModelSecret(a.modelSecretKey, key)
	if err != nil {
		return err
	}
	provider.APIKeyCiphertext = ciphertext
	provider.APIKeyHint = maskAPIKey(key)
	return nil
}

func encryptModelSecret(secret, plaintext string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrInvalidArg
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptModelSecret(secret, ciphertext string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrInvalidArg
	}
	raw := strings.TrimSpace(ciphertext)
	if raw == "" {
		return "", nil
	}
	raw = strings.TrimPrefix(raw, "v1:")
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", ErrInvalidArg
	}
	nonce, sealed := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

func validateProviderReady(provider store.LLMProvider) error {
	if provider.Type != ProviderFake && provider.Enabled {
		if provider.BaseURL == "" || provider.APIKeyCiphertext == "" {
			return ErrInvalidArg
		}
	}
	return nil
}

func normalizeID(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func normalizeProviderType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "openai-compatible" || v == "openai" {
		return ProviderOpenAICompat
	}
	return v
}

func validProviderType(v string) bool {
	return v == ProviderAnthropic || v == ProviderOpenAICompat || v == ProviderFake
}

func validIcon(route store.LLMModelRoute) bool {
	switch route.IconType {
	case IconSimpleIcons:
		return route.IconSlug != ""
	case IconImage:
		return strings.HasPrefix(route.IconURL, "https://")
	default:
		return false
	}
}

func publicLLMModel(route store.LLMModelRoute) store.PublicLLMModel {
	icon := store.LLMModelIcon{Type: route.IconType}
	if route.IconType == IconImage {
		icon.Src = route.IconURL
	} else {
		icon.Slug = route.IconSlug
	}
	return store.PublicLLMModel{
		Alias: route.Alias,
		Label: route.Label,
		Icon:  icon,
	}
}

// ---- Audit ----

// ListAudit returns the most recent audit entries.
func (a *Admin) ListAudit(ctx context.Context, limit int) ([]store.AuditEntry, error) {
	return a.store.ListAudit(ctx, limit)
}

// AppendAuditEvent appends one structured audit event.
func (a *Admin) AppendAuditEvent(ctx context.Context, e store.AuditEvent) error {
	if e.At.IsZero() {
		e.At = a.now().UTC()
	}
	return a.store.AppendAuditEvent(ctx, e)
}

// ListAuditEvents returns filtered structured audit events.
func (a *Admin) ListAuditEvents(ctx context.Context, q store.AuditEventQuery) ([]store.AuditEvent, error) {
	return a.store.ListAuditEvents(ctx, q)
}

// ListTraceEvents returns timing events for one trace.
func (a *Admin) ListTraceEvents(ctx context.Context, q store.TraceEventQuery) ([]store.TraceEvent, error) {
	return a.store.ListTraceEvents(ctx, q)
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
