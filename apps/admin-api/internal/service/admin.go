// Package service is the admin-api business layer. Handlers stay thin
// (decode -> call service -> encode), while the store stays focused on
// persistence. Errors are returned as typed sentinels that handlers map to
// HTTP status codes.
//
// Identity note: the tokens minted here are the SAME cocola-signed HS256 JWTs
// the Python llm-gateway verifies (see internal/token, byte-compatible with the
// gateway's auth/jwt.py). Issuing here, verifying there, with a shared secret,
// is exactly the M4 identity-as-signed-token decision now driven over HTTP.
package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors mapped to HTTP codes by the handler layer.
var (
	ErrInvalidArg                  = errors.New("service: invalid argument")
	ErrUnauthenticated             = errors.New("service: unauthenticated")
	ErrAccountDisabled             = errors.New("service: account disabled")
	ErrProtectedAdmin              = errors.New("service: protected admin")
	ErrSelfPermission              = errors.New("service: self permission change")
	ErrCurrentPassword             = errors.New("service: current password invalid")
	ErrPermissionDenied            = errors.New("service: permission denied")
	ErrScheduleInPast              = errors.New("service: schedule time is in the past")
	ErrScheduleExpiration          = errors.New("service: task expiration does not allow a future run")
	ErrStorageUnavailable          = errors.New("service: storage measurement unavailable")
	ErrStorageUnsupported          = errors.New("service: storage measurement unsupported")
	ErrWorkspaceNotFound           = errors.New("service: workspace not found")
	ErrWorkspaceNodeUnavailable    = errors.New("service: workspace node unavailable")
	ErrWorkspaceFileTooLarge       = errors.New("service: workspace file too large")
	ErrWorkspacePreviewUnsupported = errors.New("service: workspace preview unsupported")
	ErrWorkspaceDirectoryTooLarge  = errors.New("service: workspace directory too large")
	ErrTooManyRequests             = errors.New("service: too many requests")
	ErrNotFound                    = store.ErrNotFound
	ErrConflict                    = store.ErrConflict
)

// Clock is injectable so tests get deterministic timestamps.
type Clock func() time.Time

// Admin is the admin-api service.
type Admin struct {
	store                    store.Store
	issuer                   *token.Issuer
	now                      Clock
	skillBundles             SkillBundleStore
	sandboxNodes             SandboxNodeManager
	sandboxRuntimes          SandboxRuntimeManager
	sessionStorage           SessionStorageMonitor
	workspaceBrowser         WorkspaceBrowser
	architectureChecker      ArchitectureHealthChecker
	userEvents               UserEventBroker
	modelSecretKey           string
	memoryEmbeddingDimension int
	memoryOpenVikingURL      string
	memoryHTTPClient         *http.Client
	embeddingHTTPClient      *http.Client
	configSecretKey          string
	schedulerStarted         atomic.Bool
}

// New builds the service. issuer may be nil if token minting is disabled (no
// signing secret configured); token endpoints then return ErrInvalidArg.
func New(s store.Store, iss *token.Issuer, now Clock) *Admin {
	if now == nil {
		now = time.Now
	}
	return &Admin{
		store: s, issuer: iss, now: now,
		embeddingHTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
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

func (a *Admin) WithArchitectureHealthChecker(c ArchitectureHealthChecker) *Admin {
	a.architectureChecker = c
	return a
}

// WithModelSecretKey configures API-key encryption for admin-managed LLM
// providers. Without it, provider saves that include a plaintext API key fail.
func (a *Admin) WithModelSecretKey(secret string) *Admin {
	a.modelSecretKey = strings.TrimSpace(secret)
	return a
}

// WithMemoryEmbeddingDimension installs the startup-locked OpenViking vector
// dimension used to validate administrator model selections.
func (a *Admin) WithMemoryEmbeddingDimension(dimension int) *Admin {
	a.memoryEmbeddingDimension = dimension
	return a
}

func (a *Admin) WithMemoryOpenVikingURL(rawURL string) *Admin {
	a.memoryOpenVikingURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	a.memoryHTTPClient = &http.Client{Timeout: 2 * time.Second}
	return a
}

// WithConfigSecretKey configures encryption for administrator-managed runtime
// configuration secrets such as MCP env/header values.
func (a *Admin) WithConfigSecretKey(secret string) *Admin {
	a.configSecretKey = strings.TrimSpace(secret)
	return a
}

func (a *Admin) configSecret() string {
	return strings.TrimSpace(a.configSecretKey)
}

// ---- Auth users / whitelist ----

const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	bootstrapActor = "bootstrap"
)

// AuthUserInput describes user creation/update over the admin surface.
type AuthUserInput struct {
	Name     string
	Username string
	Email    string
	// Tenant is the team/tenant assignment. On update, a nil pointer leaves the
	// existing value untouched; a non-nil pointer (including empty string) sets it.
	Tenant   *string
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
	Version  int64  `json:"version"`
}

type OwnAccountInput struct {
	Name            string
	Username        string
	Email           string
	CurrentPassword string
	ExpectedVersion int64
}

type OwnPasswordInput struct {
	CurrentPassword string
	NewPassword     string
	ExpectedVersion int64
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

func normalizeName(name string) string { return strings.TrimSpace(name) }

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

func validAccountProfile(name, username, email string) bool {
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") ||
		username == "" || len(username) > 64 || strings.ContainsAny(username, "\x00\r\n\t ") ||
		email == "" || len(email) > 254 || strings.ContainsAny(email, "\x00\r\n\t ") {
		return false
	}
	at := strings.IndexByte(email, '@')
	return at > 0 && at == strings.LastIndexByte(email, '@') && at < len(email)-1
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
		Version:  u.Version,
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
	name := normalizeName(in.Name)
	if name == "" {
		name = username
	}
	if !validAccountProfile(name, username, email) || !validRole(role) || strings.TrimSpace(in.Password) == "" {
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
	tenant := ""
	if in.Tenant != nil {
		tenant = strings.TrimSpace(*in.Tenant)
	}
	u := store.AuthUser{
		ID:              newID(),
		Username:        username,
		Email:           email,
		Name:            name,
		TenantID:        tenant,
		Role:            role,
		Enabled:         enabled,
		Version:         1,
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

func (a *Admin) GetAuthUser(ctx context.Context, id string) (store.AuthUser, error) {
	if strings.TrimSpace(id) == "" {
		return store.AuthUser{}, ErrInvalidArg
	}
	u, err := a.store.GetAuthUser(ctx, strings.TrimSpace(id))
	if err != nil {
		return store.AuthUser{}, err
	}
	if !u.DeletedAt.IsZero() {
		return store.AuthUser{}, ErrNotFound
	}
	return u, nil
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
	if name := normalizeName(in.Name); name != "" {
		u.Name = name
	}
	if !validAccountProfile(u.Name, u.Username, u.Email) {
		return store.AuthUser{}, ErrInvalidArg
	}
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
	if in.Tenant != nil {
		u.TenantID = strings.TrimSpace(*in.Tenant)
	}
	u.UpdatedAt = a.now().UTC()
	u.UpdatedBy = in.Actor
	u.Version++
	if err := a.store.UpdateAuthUser(ctx, u); err != nil {
		return store.AuthUser{}, err
	}
	if !u.Enabled {
		if err := a.pauseScheduledTasksForUnavailableOwner(ctx, u.ID, "Owner disabled", in.Actor); err != nil {
			return store.AuthUser{}, err
		}
	}
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
	u.Version++
	if err := a.store.UpdateAuthUser(ctx, u); err != nil {
		return store.AuthUser{}, err
	}
	return u, nil
}

func (a *Admin) GetOwnAccount(ctx context.Context, userID string) (store.AuthUser, error) {
	u, err := a.GetAuthUser(ctx, userID)
	if err != nil {
		return store.AuthUser{}, err
	}
	if !u.Enabled {
		return store.AuthUser{}, ErrAccountDisabled
	}
	return u, nil
}

func (a *Admin) UpdateOwnAccount(ctx context.Context, userID string, in OwnAccountInput) (store.AuthUser, error) {
	u, err := a.GetOwnAccount(ctx, userID)
	if err != nil {
		return store.AuthUser{}, err
	}
	if in.ExpectedVersion <= 0 || u.Version != in.ExpectedVersion {
		return store.AuthUser{}, store.ErrVersionConflict
	}
	name := normalizeName(in.Name)
	username := normalizeUsername(in.Username)
	email := normalizeEmail(in.Email)
	if !validAccountProfile(name, username, email) {
		return store.AuthUser{}, ErrInvalidArg
	}
	if email != u.Email && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
		return store.AuthUser{}, ErrCurrentPassword
	}
	now := a.now().UTC()
	u.Name = name
	u.Username = username
	u.Email = email
	u.UpdatedAt = now
	u.UpdatedBy = u.ID
	u.Version = in.ExpectedVersion + 1
	if err := a.store.UpdateAuthUserVersion(ctx, u, in.ExpectedVersion); err != nil {
		return store.AuthUser{}, err
	}
	return u, nil
}

func (a *Admin) ChangeOwnPassword(ctx context.Context, userID string, in OwnPasswordInput) (store.AuthUser, error) {
	u, err := a.GetOwnAccount(ctx, userID)
	if err != nil {
		return store.AuthUser{}, err
	}
	if in.ExpectedVersion <= 0 || u.Version != in.ExpectedVersion {
		return store.AuthUser{}, store.ErrVersionConflict
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
		return store.AuthUser{}, ErrCurrentPassword
	}
	if utf8.RuneCountInString(in.NewPassword) < 8 || len(in.NewPassword) > 72 {
		return store.AuthUser{}, ErrInvalidArg
	}
	pw, err := hashPassword(in.NewPassword)
	if err != nil {
		return store.AuthUser{}, err
	}
	now := a.now().UTC()
	u.PasswordHash = pw
	u.PasswordUpdated = now
	u.UpdatedAt = now
	u.UpdatedBy = u.ID
	u.Version = in.ExpectedVersion + 1
	if err := a.store.UpdateAuthUserVersion(ctx, u, in.ExpectedVersion); err != nil {
		return store.AuthUser{}, err
	}
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
	if err := a.pauseScheduledTasksForUnavailableOwner(ctx, id, "Owner deleted", actor); err != nil {
		return err
	}
	return nil
}

func (a *Admin) pauseScheduledTasksForUnavailableOwner(ctx context.Context, ownerID, reason, actor string) error {
	tasks, err := a.store.ListScheduledTasksForOwner(ctx, ownerID)
	if err != nil {
		return err
	}
	now := a.now().UTC()
	for _, task := range tasks {
		if task.Status != TaskStatusActive {
			continue
		}
		task.Status = TaskStatusPaused
		task.NextRunAt = time.Time{}
		task.LastError = reason
		task.UpdatedAt = now
		task.UpdatedBy = actor
		if err := a.store.UpdateScheduledTask(ctx, task, false, nil); err != nil {
			return err
		}
	}
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
			Version:         1,
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
	existing.Version++
	if err := a.store.UpdateAuthUser(ctx, existing); err != nil {
		return err
	}
	return nil
}

func (a *Admin) IssueRuntimeToken(ctx context.Context, email string, ttl time.Duration) (string, error) {
	if a.issuer == nil {
		return "", ErrInvalidArg
	}
	email = normalizeEmail(email)
	if email == "" {
		return "", ErrInvalidArg
	}
	// Runtime identity always comes from a persisted account. The caller cannot
	// supply a fallback tenant or an unverified id-only principal.
	u, err := a.store.GetAuthUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if isAuthUserUnavailable(u) {
		return "", ErrAccountDisabled
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	name := normalizeName(u.Name)
	if name == "" {
		name = u.Username
	}
	tok, _, err := a.issuer.IssueUser(
		u.ID, strings.TrimSpace(u.TenantID), u.Email, name, u.Username, ttl, a.now().Unix(),
	)
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
	return nil
}

// ---- Skills ----

// CreateSkill registers a new skill (disabled by default unless Enabled set).
func (a *Admin) CreateSkill(ctx context.Context, s store.Skill, actor string) (store.Skill, error) {
	if s.ID == "" || s.Name == "" {
		return store.Skill{}, ErrInvalidArg
	}
	if err := validateEnabledSkill(s); err != nil {
		return store.Skill{}, err
	}
	if s.RuntimeID == "" {
		s.RuntimeID = s.ID
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
	byRuntimeID := make(map[string]int)
	appendSkill := func(s store.Skill, replace bool) {
		if s.RuntimeID == "" {
			s.RuntimeID = s.ID
		}
		if index, ok := byRuntimeID[s.RuntimeID]; ok {
			if replace {
				out[index] = s
			}
			return
		}
		byRuntimeID[s.RuntimeID] = len(out)
		out = append(out, s)
	}
	for _, s := range adminSkills {
		if s.Scope != "" && s.Scope != "admin" {
			continue
		}
		if enabled, ok := prefMap[s.ID]; ok && !enabled {
			continue
		}
		appendSkill(s, false)
	}
	userSkills, err := a.store.ListSkillsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, s := range userSkills {
		if s.Enabled {
			// A personal Skill with the same Runtime-native ID intentionally
			// overrides the shared Skill for this user. The internal catalog IDs
			// remain distinct, while the sandbox receives one unambiguous name.
			appendSkill(s, true)
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
	if err := validateEnabledSkill(s); err != nil {
		return store.Skill{}, err
	}
	s.UpdatedAt = a.now().UTC()
	s.UpdatedBy = actor
	if err := a.store.UpdateSkill(ctx, s); err != nil {
		return store.Skill{}, err
	}
	return s, nil
}

// DeleteSkill removes a skill from the catalog.
func (a *Admin) DeleteSkill(ctx context.Context, id, actor string) error {
	if err := a.store.DeleteSkill(ctx, id); err != nil {
		return err
	}
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
			RuntimeID:       c.ID,
			Name:            c.Name,
			Description:     c.Description,
			Version:         c.Version,
			Entrypoint:      "$CLAUDE_CONFIG_DIR/skills/" + c.ID,
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
	return imported, candidates, nil
}

func (a *Admin) SetUserSkillEnabled(ctx context.Context, userID, skillID string, enabled bool) error {
	s, err := a.store.GetSkill(ctx, skillID)
	if err != nil {
		return err
	}
	if enabled {
		candidate := s
		candidate.Enabled = true
		if err := validateEnabledSkill(candidate); err != nil {
			return err
		}
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

func validateEnabledSkill(s store.Skill) error {
	if s.Enabled && strings.TrimSpace(s.BundleObjectKey) == "" && strings.TrimSpace(s.SkillMD) == "" {
		return ErrInvalidArg
	}
	return nil
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
	ProviderAnthropic        = "anthropic"
	ProviderOpenAIResponses  = "openai_responses"
	ProviderOpenAIEmbeddings = "openai_embeddings"
	IconSimpleIcons          = "simple-icons"
	IconImage                = "image"
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
	Alias              string
	ProviderID         string
	RealModel          string
	Label              string
	IconType           string
	IconSlug           string
	IconURL            string
	Enabled            *bool
	Visible            *bool
	IsDefault          bool
	SortOrder          int
	EmbeddingDimension int
	Actor              string
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
		if ptype != provider.Type {
			routes, listErr := a.store.ListLLMModelRoutes(ctx)
			if listErr != nil {
				return store.LLMProvider{}, listErr
			}
			for _, route := range routes {
				if route.ProviderID == provider.ID {
					return store.LLMProvider{}, store.ErrConflict
				}
			}
		}
		provider.Type = ptype
	}
	if in.BaseURL != "" {
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
	return provider, nil
}

func (a *Admin) DeleteLLMProvider(ctx context.Context, id, actor string) error {
	id = normalizeID(id)
	if err := a.store.DeleteLLMProvider(ctx, id); err != nil {
		return err
	}
	return nil
}

func (a *Admin) CreateLLMModel(ctx context.Context, in LLMModelInput) (store.LLMModelRoute, error) {
	route, err := a.llmRouteFromInput(store.LLMModelRoute{}, in, true)
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	provider, err := a.store.GetLLMProvider(ctx, route.ProviderID)
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	route.Protocol = modelProtocol(provider.Type)
	if route.Protocol == "openai-embeddings" {
		if route.EmbeddingDimension <= 0 {
			return store.LLMModelRoute{}, ErrInvalidArg
		}
		route.Visible = false
		route.IsDefault = false
	} else {
		route.EmbeddingDimension = 0
	}
	if err := a.store.CreateLLMModelRoute(ctx, route); err != nil {
		return store.LLMModelRoute{}, err
	}
	return route, nil
}

func (a *Admin) ListLLMModels(ctx context.Context) ([]store.LLMModelRoute, error) {
	return a.store.ListLLMModelRoutes(ctx)
}

func (a *Admin) UpdateLLMModel(ctx context.Context, id string, in LLMModelInput) (store.LLMModelRoute, error) {
	existing, err := a.store.GetLLMModelRoute(ctx, normalizeID(id))
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	route, err := a.llmRouteFromInput(existing, in, false)
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	provider, err := a.store.GetLLMProvider(ctx, route.ProviderID)
	if err != nil {
		return store.LLMModelRoute{}, err
	}
	protocol := modelProtocol(provider.Type)
	if existing.Protocol != "" && existing.Protocol != protocol {
		return store.LLMModelRoute{}, store.ErrConflict
	}
	route.Protocol = protocol
	if route.Protocol == "openai-embeddings" {
		if route.EmbeddingDimension <= 0 {
			return store.LLMModelRoute{}, ErrInvalidArg
		}
		route.Visible = false
		route.IsDefault = false
	} else {
		route.EmbeddingDimension = 0
	}
	if err := a.store.UpdateLLMModelRoute(ctx, route); err != nil {
		return store.LLMModelRoute{}, err
	}
	return route, nil
}

func (a *Admin) DeleteLLMModel(ctx context.Context, id, actor string) error {
	id = normalizeID(id)
	route, err := a.store.GetLLMModelRoute(ctx, id)
	if err != nil {
		return err
	}
	if err := a.store.DeleteLLMModelRoute(ctx, id); err != nil {
		return err
	}
	if route.Protocol == "openai-embeddings" {
		routes, listErr := a.store.ListLLMModelRoutes(ctx)
		if listErr == nil {
			providerInUse := false
			for _, candidate := range routes {
				if candidate.ProviderID == route.ProviderID {
					providerInUse = true
					break
				}
			}
			if !providerInUse {
				_ = a.store.DeleteLLMProvider(ctx, route.ProviderID)
			}
		}
	}
	return nil
}

func (a *Admin) SetDefaultLLMModel(ctx context.Context, id, actor string) (store.LLMModelRoute, error) {
	route, err := a.store.GetLLMModelRoute(ctx, normalizeID(id))
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
	return route, nil
}

func (a *Admin) ListPublicLLMModels(ctx context.Context) ([]store.PublicLLMModel, error) {
	providers, err := a.store.ListLLMProviders(ctx)
	if err != nil {
		return nil, err
	}
	enabledProviders := map[string]store.LLMProvider{}
	for _, p := range providers {
		if p.Enabled {
			enabledProviders[p.ID] = p
		}
	}
	routes, err := a.store.ListLLMModelRoutes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]store.PublicLLMModel, 0, len(routes))
	for _, route := range routes {
		provider, ok := enabledProviders[route.ProviderID]
		if !route.Enabled || !route.Visible || !ok || route.Protocol == "openai-embeddings" {
			continue
		}
		out = append(out, publicLLMModel(route, provider.Type))
	}
	return out, nil
}

func (a *Admin) llmRouteFromInput(existing store.LLMModelRoute, in LLMModelInput, create bool) (store.LLMModelRoute, error) {
	route := existing
	if create {
		route.ID = newID()
		route.Alias = normalizeID(in.Alias)
		route.Enabled = true
		route.Visible = true
		route.IconType = IconSimpleIcons
		now := a.now().UTC()
		route.CreatedAt = now
		route.UpdatedAt = now
	}
	if route.ID == "" || route.Alias == "" {
		return store.LLMModelRoute{}, ErrInvalidArg
	}
	if in.ProviderID != "" || create {
		route.ProviderID = normalizeID(in.ProviderID)
	}
	if in.RealModel != "" || create {
		route.RealModel = strings.TrimSpace(in.RealModel)
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
	if in.EmbeddingDimension != 0 || create {
		route.EmbeddingDimension = in.EmbeddingDimension
	}
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
	if provider.Enabled {
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
	return strings.ToLower(strings.TrimSpace(v))
}

func validProviderType(v string) bool {
	return v == ProviderAnthropic || v == ProviderOpenAIResponses || v == ProviderOpenAIEmbeddings
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

func publicModelSlug(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.NewReplacer("_", "-", " ", "-").Replace(v)
	return v
}

func inferPublicModelFamily(route store.LLMModelRoute) string {
	haystack := publicModelSlug(strings.Join([]string{
		route.Alias,
		route.RealModel,
		route.Label,
		route.IconSlug,
		route.ProviderID,
	}, " "))
	switch {
	case strings.Contains(haystack, "deepseek"):
		return "deepseek"
	case strings.Contains(haystack, "claude"):
		return "claude"
	case strings.Contains(haystack, "anthropic"):
		return "anthropic"
	case strings.Contains(haystack, "gemini"):
		return "gemini"
	case strings.Contains(haystack, "google"):
		return "google"
	case strings.Contains(haystack, "qwen") || strings.Contains(haystack, "tongyi") ||
		strings.Contains(haystack, "dashscope") || strings.Contains(haystack, "bailian"):
		return "qwen"
	case strings.Contains(haystack, "doubao"):
		return "doubao"
	case strings.Contains(haystack, "volc") || strings.Contains(haystack, "bytedance"):
		return "bytedance"
	case strings.Contains(haystack, "mistral"):
		return "mistral"
	case strings.Contains(haystack, "grok"):
		return "grok"
	case strings.Contains(haystack, "xai"):
		return "xai"
	case strings.Contains(haystack, "moonshot") || strings.Contains(haystack, "kimi"):
		return "moonshot"
	case strings.Contains(haystack, "codex"):
		return "codex"
	case strings.Contains(haystack, "gpt") || strings.Contains(haystack, "openai"):
		return "openai"
	default:
		return publicModelSlug(route.ProviderID)
	}
}

func publicLLMModel(route store.LLMModelRoute, providerType string) store.PublicLLMModel {
	provider := publicModelSlug(route.ProviderID)
	family := inferPublicModelFamily(route)
	iconSlug := publicModelSlug(route.IconSlug)
	if iconSlug == "" {
		iconSlug = family
	}
	if iconSlug == "" {
		iconSlug = provider
	}
	icon := store.LLMModelIcon{Type: route.IconType}
	if route.IconType == IconImage {
		icon.Src = route.IconURL
	} else {
		icon.Slug = iconSlug
	}
	return store.PublicLLMModel{
		ID:        route.ID,
		Alias:     route.Alias,
		Label:     route.Label,
		Provider:  provider,
		Family:    family,
		IconSlug:  iconSlug,
		Icon:      icon,
		Protocols: []string{modelProtocol(providerType)},
		IsDefault: route.IsDefault,
	}
}

func modelProtocol(providerType string) string {
	if providerType == ProviderOpenAIResponses {
		return "openai-responses"
	}
	if providerType == ProviderOpenAIEmbeddings {
		return "openai-embeddings"
	}
	return "anthropic-messages"
}

type MemoryConfigView struct {
	store.MemoryConfig
	Status             string `json:"status"`
	CanEnable          bool   `json:"can_enable"`
	EmbeddingDimension int    `json:"embedding_dimension"`
	OpenVikingStatus   string `json:"openviking_status"`
	VLMStatus          string `json:"vlm_status"`
	EmbeddingStatus    string `json:"embedding_status"`
	Error              string `json:"error,omitempty"`
}

type MemoryConfigInput struct {
	Enabled                bool
	ExtractionModelRouteID string
	EmbeddingModelRouteID  string
	ExpectedVersion        int64
	Actor                  string
}

type EmbeddingModelTestInput struct {
	RouteID string
	Model   string
	BaseURL string
	APIKey  *string
}

type EmbeddingModelTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Dimension int    `json:"dimension,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

// EmbeddingModelInput is the intentionally small admin contract for a hidden
// OpenAI Embeddings model. Provider and route details stay an implementation
// detail so every future consumer can select the same route from the catalog.
type EmbeddingModelInput struct {
	Model   string
	BaseURL string
	APIKey  *string
	Actor   string
}

type EmbeddingModelView struct {
	store.LLMModelRoute
	BaseURL    string `json:"base_url"`
	APIKeyHint string `json:"api_key_hint"`
}

func (a *Admin) GetMemoryConfig(ctx context.Context) (MemoryConfigView, error) {
	config, err := a.store.GetMemoryConfig(ctx)
	if err != nil {
		return MemoryConfigView{}, err
	}
	return a.memoryConfigView(ctx, config, true), nil
}

func (a *Admin) UpdateMemoryConfig(ctx context.Context, in MemoryConfigInput) (MemoryConfigView, error) {
	current, err := a.store.GetMemoryConfig(ctx)
	if err != nil {
		return MemoryConfigView{}, err
	}
	config := store.MemoryConfig{
		Enabled:                in.Enabled,
		ExtractionModelRouteID: strings.TrimSpace(in.ExtractionModelRouteID),
		EmbeddingModelRouteID:  strings.TrimSpace(in.EmbeddingModelRouteID),
		UpdatedAt:              a.now().UTC(), UpdatedBy: in.Actor,
	}
	if config.Enabled {
		view := a.memoryConfigView(ctx, config, true)
		if view.Status != "ready" {
			return MemoryConfigView{}, fmt.Errorf("%w: memory configuration is incomplete: %s", ErrInvalidArg, view.Error)
		}
		if err := a.store.LockMemoryIndex(ctx, a.memoryEmbeddingDimension); err != nil {
			return MemoryConfigView{}, fmt.Errorf("%w: memory index dimension is locked", ErrInvalidArg)
		}
	}
	updated, err := a.store.UpdateMemoryConfig(ctx, config, in.ExpectedVersion)
	if err != nil {
		return MemoryConfigView{}, err
	}
	// An enabled-to-disabled transition is the emergency stop path. Persist it
	// without waiting for any downstream readiness probe; later GETs still probe
	// normally so the Drawer can decide whether re-enabling is safe.
	probeDownstream := !(current.Enabled && !updated.Enabled)
	return a.memoryConfigView(ctx, updated, probeDownstream), nil
}

func (a *Admin) memoryConfigView(
	ctx context.Context,
	config store.MemoryConfig,
	probeDownstream bool,
) MemoryConfigView {
	view := MemoryConfigView{
		MemoryConfig: config, Status: "disabled", EmbeddingDimension: a.memoryEmbeddingDimension,
		OpenVikingStatus: "not_ready", VLMStatus: "not_configured", EmbeddingStatus: "not_configured",
	}
	if !config.Enabled && config.ExtractionModelRouteID == "" && config.EmbeddingModelRouteID == "" {
		return view
	}
	if config.ExtractionModelRouteID == "" || config.EmbeddingModelRouteID == "" || a.memoryEmbeddingDimension <= 0 {
		view.Status, view.Error = "incomplete", "select extraction and embedding models"
		if config.Enabled {
			view.Status = "degraded"
		}
		return view
	}
	extraction, extractionErr := a.store.GetLLMModelRoute(ctx, config.ExtractionModelRouteID)
	embedding, embeddingErr := a.store.GetLLMModelRoute(ctx, config.EmbeddingModelRouteID)
	extractionProvider, extractionProviderErr := a.store.GetLLMProvider(ctx, extraction.ProviderID)
	embeddingProvider, embeddingProviderErr := a.store.GetLLMProvider(ctx, embedding.ProviderID)
	switch {
	case extractionErr != nil || embeddingErr != nil:
		view.Status, view.Error = "incomplete", "selected model route is unavailable"
	case extractionProviderErr != nil || embeddingProviderErr != nil:
		view.Status, view.Error = "incomplete", "selected model provider is unavailable"
	case (extraction.Protocol != "anthropic-messages" && extraction.Protocol != "openai-responses") || !extraction.Enabled || !extractionProvider.Enabled:
		view.Status, view.Error = "incomplete", "extraction model is not an enabled generation route"
	case embedding.Protocol != "openai-embeddings" || !embedding.Enabled || !embeddingProvider.Enabled:
		view.Status, view.Error = "incomplete", "embedding model is not an enabled embedding route"
	case embedding.EmbeddingDimension != a.memoryEmbeddingDimension:
		view.Status, view.Error = "incomplete", "embedding model dimension does not match the memory index"
	default:
		view.Status = "ready"
		view.CanEnable = true
		view.VLMStatus = "ready"
		view.EmbeddingStatus = "ready"
	}
	if view.CanEnable {
		if !probeDownstream {
			view.CanEnable = false
		} else if err := a.memoryOpenVikingReady(ctx); err != nil {
			view.Status, view.CanEnable = "incomplete", false
			view.Error = "OpenViking is not ready"
		} else {
			view.OpenVikingStatus = "ready"
		}
	}
	if !config.Enabled && view.Status == "ready" {
		view.Status = "disabled"
	}
	if config.Enabled && view.Status != "ready" {
		view.Status = "degraded"
	}
	return view
}

func (a *Admin) TestEmbeddingModel(
	ctx context.Context,
	in EmbeddingModelTestInput,
) (EmbeddingModelTestResult, error) {
	provider := store.LLMProvider{Type: ProviderOpenAIEmbeddings, Enabled: true}
	model := strings.TrimSpace(in.Model)
	if strings.TrimSpace(in.RouteID) != "" {
		route, err := a.store.GetLLMModelRoute(ctx, strings.TrimSpace(in.RouteID))
		if err != nil {
			return EmbeddingModelTestResult{}, err
		}
		if route.Protocol != "openai-embeddings" {
			return EmbeddingModelTestResult{}, ErrInvalidArg
		}
		provider, err = a.store.GetLLMProvider(ctx, route.ProviderID)
		if err != nil {
			return EmbeddingModelTestResult{}, err
		}
		if model == "" {
			model = route.RealModel
		}
	}
	if strings.TrimSpace(in.BaseURL) != "" {
		baseURL, err := normalizeEmbeddingBaseURL(in.BaseURL)
		if err != nil {
			return EmbeddingModelTestResult{}, err
		}
		provider.BaseURL = baseURL
	}
	if err := a.applyProviderAPIKey(&provider, in.APIKey); err != nil {
		return EmbeddingModelTestResult{}, err
	}
	return a.probeEmbedding(ctx, model, provider)
}

func (a *Admin) CreateEmbeddingModel(
	ctx context.Context,
	in EmbeddingModelInput,
) (EmbeddingModelView, error) {
	model := strings.TrimSpace(in.Model)
	baseURL, err := normalizeEmbeddingBaseURL(in.BaseURL)
	if err != nil {
		return EmbeddingModelView{}, err
	}
	now := a.now().UTC()
	provider := store.LLMProvider{
		ID: "embedding-" + newID(), Name: model, Type: ProviderOpenAIEmbeddings,
		BaseURL: baseURL, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.applyProviderAPIKey(&provider, in.APIKey); err != nil {
		return EmbeddingModelView{}, err
	}
	if model == "" || in.APIKey == nil || strings.TrimSpace(*in.APIKey) == "" {
		return EmbeddingModelView{}, ErrInvalidArg
	}
	probe, err := a.probeEmbedding(ctx, model, provider)
	if err != nil {
		return EmbeddingModelView{}, err
	}
	if !probe.OK {
		return EmbeddingModelView{}, fmt.Errorf("%w: embedding connection test failed: %s", ErrInvalidArg, probe.Error)
	}
	route := store.LLMModelRoute{
		ID: "embedding-" + newID(), Alias: normalizeID(model), ProviderID: provider.ID,
		Protocol: "openai-embeddings", RealModel: model, Label: model,
		IconType: IconSimpleIcons, IconSlug: "openai", Enabled: true, Visible: false,
		EmbeddingDimension: probe.Dimension, CreatedAt: now, UpdatedAt: now,
	}
	if route.Alias == "" {
		route.Alias = "embedding"
	}
	if err := a.store.CreateLLMProvider(ctx, provider); err != nil {
		return EmbeddingModelView{}, err
	}
	if err := a.store.CreateLLMModelRoute(ctx, route); err != nil {
		_ = a.store.DeleteLLMProvider(ctx, provider.ID)
		return EmbeddingModelView{}, err
	}
	return embeddingModelView(route, provider), nil
}

func (a *Admin) UpdateEmbeddingModel(
	ctx context.Context,
	id string,
	in EmbeddingModelInput,
) (EmbeddingModelView, error) {
	route, err := a.store.GetLLMModelRoute(ctx, normalizeID(id))
	if err != nil {
		return EmbeddingModelView{}, err
	}
	if route.Protocol != "openai-embeddings" {
		return EmbeddingModelView{}, ErrInvalidArg
	}
	provider, err := a.store.GetLLMProvider(ctx, route.ProviderID)
	if err != nil {
		return EmbeddingModelView{}, err
	}
	previousProvider := provider
	model := strings.TrimSpace(in.Model)
	if model == "" || strings.TrimSpace(in.BaseURL) == "" {
		return EmbeddingModelView{}, ErrInvalidArg
	}
	provider.BaseURL, err = normalizeEmbeddingBaseURL(in.BaseURL)
	if err != nil {
		return EmbeddingModelView{}, err
	}
	if err := a.applyProviderAPIKey(&provider, in.APIKey); err != nil {
		return EmbeddingModelView{}, err
	}
	provider.Name = model
	provider.Enabled = true
	provider.UpdatedAt = a.now().UTC()
	probe, err := a.probeEmbedding(ctx, model, provider)
	if err != nil {
		return EmbeddingModelView{}, err
	}
	if !probe.OK {
		return EmbeddingModelView{}, fmt.Errorf("%w: embedding connection test failed: %s", ErrInvalidArg, probe.Error)
	}
	route.RealModel = model
	route.Label = model
	route.Alias = normalizeID(model)
	route.Enabled = true
	route.Visible = false
	route.IsDefault = false
	route.EmbeddingDimension = probe.Dimension
	route.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateLLMProvider(ctx, provider); err != nil {
		return EmbeddingModelView{}, err
	}
	if err := a.store.UpdateLLMModelRoute(ctx, route); err != nil {
		_ = a.store.UpdateLLMProvider(ctx, previousProvider)
		return EmbeddingModelView{}, err
	}
	return embeddingModelView(route, provider), nil
}

func embeddingModelView(route store.LLMModelRoute, provider store.LLMProvider) EmbeddingModelView {
	return EmbeddingModelView{
		LLMModelRoute: route,
		BaseURL:       provider.BaseURL,
		APIKeyHint:    provider.APIKeyHint,
	}
}

func normalizeEmbeddingBaseURL(raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: embedding base URL must be an HTTP(S) URL without credentials, query, or fragment", ErrInvalidArg)
	}
	if strings.HasSuffix(parsed.Path, "/embeddings") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/embeddings")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (a *Admin) probeEmbedding(
	ctx context.Context,
	model string,
	provider store.LLMProvider,
) (EmbeddingModelTestResult, error) {
	model = strings.TrimSpace(model)
	if model == "" || strings.TrimSpace(provider.BaseURL) == "" || provider.APIKeyCiphertext == "" {
		return EmbeddingModelTestResult{}, fmt.Errorf("%w: embedding model, base URL, and API key are required", ErrInvalidArg)
	}
	apiKey, err := decryptModelSecret(a.modelSecretKey, provider.APIKeyCiphertext)
	if err != nil {
		return EmbeddingModelTestResult{}, fmt.Errorf("%w: embedding API key cannot be decrypted", ErrInvalidArg)
	}
	payload, err := json.Marshal(map[string]any{
		"model":           model,
		"input":           []string{"cocola embedding connectivity test"},
		"encoding_format": "float",
	})
	if err != nil {
		return EmbeddingModelTestResult{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(
		probeCtx,
		http.MethodPost,
		strings.TrimRight(provider.BaseURL, "/")+"/embeddings",
		bytes.NewReader(payload),
	)
	if err != nil {
		return EmbeddingModelTestResult{}, fmt.Errorf("%w: invalid embedding endpoint", ErrInvalidArg)
	}
	req.Header.Set("authorization", "Bearer "+apiKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	started := time.Now()
	client := a.embeddingHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		var networkErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()) {
			return embeddingProbeFailure("timeout", "Request timed out", latency), nil
		}
		return embeddingProbeFailure("transport_error", "Could not reach the embedding endpoint", latency), nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		code, message := embeddingHTTPError(response.StatusCode)
		return embeddingProbeFailure(code, message, latency), nil
	}
	var body struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&body); err != nil || len(body.Data) == 0 {
		return embeddingProbeFailure("invalid_response", "Provider returned an invalid OpenAI embedding response", latency), nil
	}
	dimension := len(body.Data[0].Embedding)
	if dimension == 0 {
		return embeddingProbeFailure("invalid_response", "Provider returned an empty embedding vector", latency), nil
	}
	for _, value := range body.Data[0].Embedding {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return embeddingProbeFailure("invalid_response", "Provider returned invalid vector values", latency), nil
		}
	}
	return EmbeddingModelTestResult{
		OK: true, LatencyMS: latency, Dimension: dimension,
	}, nil
}

func embeddingProbeFailure(code, message string, latency int64) EmbeddingModelTestResult {
	return EmbeddingModelTestResult{LatencyMS: latency, ErrorCode: code, Error: message}
}

func embeddingHTTPError(status int) (string, string) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_failed", fmt.Sprintf("Authentication failed (HTTP %d)", status)
	case http.StatusNotFound:
		return "endpoint_not_found", "Endpoint not found (HTTP 404)"
	case http.StatusTooManyRequests:
		return "rate_limited", "Provider rate limit exceeded (HTTP 429)"
	default:
		return "provider_rejected", fmt.Sprintf("Provider rejected the request (HTTP %d)", status)
	}
}

func (a *Admin) memoryOpenVikingReady(ctx context.Context) error {
	if a.memoryOpenVikingURL == "" || a.memoryHTTPClient == nil {
		return errors.New("OpenViking is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.memoryOpenVikingURL+"/ready", nil)
	if err != nil {
		return err
	}
	response, err := a.memoryHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenViking readiness returned %d", response.StatusCode)
	}
	return nil
}

func (a *Admin) GetConversationRun(ctx context.Context, traceID string) (store.ConversationRun, error) {
	return a.store.GetConversationRun(ctx, traceID)
}

func (a *Admin) ListConversationRuns(ctx context.Context, q store.ConversationRunQuery) ([]store.ConversationRun, error) {
	return a.store.ListConversationRuns(ctx, q)
}

func (a *Admin) ListConversationTraceSpans(ctx context.Context, q store.ConversationTraceSpanQuery) ([]store.ConversationTraceSpan, error) {
	return a.store.ListConversationTraceSpans(ctx, q)
}
