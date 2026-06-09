package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is the in-memory Store. It is safe for concurrent use and is the
// default backend for tests and dev. All slices returned are fresh copies so
// callers cannot mutate internal state.
type Memory struct {
	mu       sync.RWMutex
	tokens   map[string]TokenRecord
	quotas   map[string]QuotaOverride // key = scope + "/" + subject
	skills   map[string]Skill
	audit    []AuditEntry
	auditSeq int64
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		tokens: map[string]TokenRecord{},
		quotas: map[string]QuotaOverride{},
		skills: map[string]Skill{},
	}
}

var _ Store = (*Memory)(nil)

func quotaKey(scope, subject string) string { return scope + "/" + subject }

// ---- Tokens ----

func (m *Memory) CreateToken(ctx context.Context, r TokenRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tokens[r.ID]; ok {
		return ErrConflict
	}
	m.tokens[r.ID] = r
	return nil
}

func (m *Memory) GetToken(ctx context.Context, id string) (TokenRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.tokens[id]
	if !ok {
		return TokenRecord{}, ErrNotFound
	}
	return r, nil
}

func (m *Memory) ListTokens(ctx context.Context, userID string) ([]TokenRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TokenRecord, 0, len(m.tokens))
	for _, r := range m.tokens {
		if userID != "" && r.UserID != userID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IssuedAt.After(out[j].IssuedAt) })
	return out, nil
}

func (m *Memory) RevokeToken(ctx context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.tokens[id]
	if !ok {
		return ErrNotFound
	}
	r.Revoked = true
	r.RevokedAt = at
	m.tokens[id] = r
	return nil
}

func (m *Memory) IsRevoked(ctx context.Context, id string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.tokens[id]
	if !ok {
		return false, ErrNotFound
	}
	return r.Revoked, nil
}

// ---- Quota overrides ----

func (m *Memory) SetQuota(ctx context.Context, q QuotaOverride) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotas[quotaKey(q.Scope, q.Subject)] = q
	return nil
}

func (m *Memory) GetQuota(ctx context.Context, scope, subject string) (QuotaOverride, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotas[quotaKey(scope, subject)]
	if !ok {
		return QuotaOverride{}, ErrNotFound
	}
	return q, nil
}

func (m *Memory) ListQuotas(ctx context.Context) ([]QuotaOverride, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]QuotaOverride, 0, len(m.quotas))
	for _, q := range m.quotas {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Subject < out[j].Subject
	})
	return out, nil
}

func (m *Memory) DeleteQuota(ctx context.Context, scope, subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := quotaKey(scope, subject)
	if _, ok := m.quotas[k]; !ok {
		return ErrNotFound
	}
	delete(m.quotas, k)
	return nil
}

// ---- Skills ----

func (m *Memory) CreateSkill(ctx context.Context, s Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[s.ID]; ok {
		return ErrConflict
	}
	m.skills[s.ID] = s
	return nil
}

func (m *Memory) GetSkill(ctx context.Context, id string) (Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[id]
	if !ok {
		return Skill{}, ErrNotFound
	}
	return s, nil
}

func (m *Memory) ListSkills(ctx context.Context, onlyEnabled bool) ([]Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Skill, 0, len(m.skills))
	for _, s := range m.skills {
		if onlyEnabled && !s.Enabled {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) UpdateSkill(ctx context.Context, s Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[s.ID]; !ok {
		return ErrNotFound
	}
	m.skills[s.ID] = s
	return nil
}

func (m *Memory) DeleteSkill(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[id]; !ok {
		return ErrNotFound
	}
	delete(m.skills, id)
	return nil
}

// ---- Audit ----

func (m *Memory) AppendAudit(ctx context.Context, e AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditSeq++
	e.ID = m.auditSeq
	m.audit = append(m.audit, e)
	return nil
}

func (m *Memory) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.audit)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]AuditEntry, 0, limit)
	// newest first
	for i := n - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.audit[i])
	}
	return out, nil
}
