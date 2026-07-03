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
	"errors"
	"strconv"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

// Sentinel errors mapped to HTTP codes by the handler layer.
var (
	ErrInvalidArg = errors.New("service: invalid argument")
	ErrNotFound   = store.ErrNotFound
	ErrConflict   = store.ErrConflict
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
