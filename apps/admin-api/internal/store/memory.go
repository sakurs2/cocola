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
	mu           sync.RWMutex
	users        map[string]AuthUser
	identifiers  map[string]string // normalized identifier -> user id
	tokens       map[string]TokenRecord
	quotas       map[string]QuotaOverride // key = scope + "/" + subject
	settings     map[string]SystemSetting
	skills       map[string]Skill
	skillPrefs   map[string]UserSkillPreference
	mcps         map[string]MCPServer
	mcpPrefs     map[string]UserMCPPreference
	llmProviders map[string]LLMProvider
	llmModels    map[string]LLMModelRoute
	tasks        map[string]ScheduledTask
	attachments  map[string]ScheduledTaskAttachment
	runs         map[string]ScheduledTaskRun
	runEvents    map[string][]ScheduledTaskRunEvent
	audit        []AuditEvent
	traceEvents  []TraceEvent
	auditSeq     int64
	runEventSeq  int64
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		users:        map[string]AuthUser{},
		identifiers:  map[string]string{},
		tokens:       map[string]TokenRecord{},
		quotas:       map[string]QuotaOverride{},
		settings:     map[string]SystemSetting{},
		skills:       map[string]Skill{},
		skillPrefs:   map[string]UserSkillPreference{},
		mcps:         map[string]MCPServer{},
		mcpPrefs:     map[string]UserMCPPreference{},
		llmProviders: map[string]LLMProvider{},
		llmModels:    map[string]LLMModelRoute{},
		tasks:        map[string]ScheduledTask{},
		attachments:  map[string]ScheduledTaskAttachment{},
		runs:         map[string]ScheduledTaskRun{},
		runEvents:    map[string][]ScheduledTaskRunEvent{},
	}
}

var _ Store = (*Memory)(nil)

func quotaKey(scope, subject string) string { return scope + "/" + subject }

// ---- Auth users ----

func (m *Memory) CreateAuthUser(ctx context.Context, u AuthUser) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; ok {
		return ErrConflict
	}
	for _, ident := range authUserIdentifiersFor(u) {
		if owner, ok := m.identifiers[ident.Value]; ok && owner != u.ID {
			return ErrConflict
		}
	}
	m.users[u.ID] = u
	for _, ident := range authUserIdentifiersFor(u) {
		m.identifiers[ident.Value] = u.ID
	}
	return nil
}

func (m *Memory) GetAuthUser(ctx context.Context, id string) (AuthUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return AuthUser{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) GetAuthUserByEmail(ctx context.Context, email string) (AuthUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return AuthUser{}, ErrNotFound
}

func (m *Memory) GetAuthUserByIdentifier(ctx context.Context, identifier string) (AuthUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.identifiers[identifier]
	if !ok {
		return AuthUser{}, ErrNotFound
	}
	u, ok := m.users[id]
	if !ok {
		return AuthUser{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) ListAuthUsers(ctx context.Context) ([]AuthUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AuthUser, 0, len(m.users))
	for _, u := range m.users {
		if !u.DeletedAt.IsZero() {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (m *Memory) UpdateAuthUser(ctx context.Context, u AuthUser) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; !ok {
		return ErrNotFound
	}
	for _, ident := range authUserIdentifiersFor(u) {
		if owner, ok := m.identifiers[ident.Value]; ok && owner != u.ID {
			return ErrConflict
		}
	}
	for value, owner := range m.identifiers {
		if owner == u.ID {
			delete(m.identifiers, value)
		}
	}
	m.users[u.ID] = u
	for _, ident := range authUserIdentifiersFor(u) {
		m.identifiers[ident.Value] = u.ID
	}
	return nil
}

func (m *Memory) DeleteAuthUser(ctx context.Context, id, actor string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok || !u.DeletedAt.IsZero() {
		return ErrNotFound
	}
	u.Enabled = false
	u.DeletedAt = at
	u.DeletedBy = actor
	u.UpdatedAt = at
	u.UpdatedBy = actor
	m.users[id] = u
	return nil
}

func (m *Memory) TouchAuthUserLogin(ctx context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	u.LastLoginAt = at
	m.users[id] = u
	return nil
}

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

// ---- System settings ----

func (m *Memory) GetSystemSetting(ctx context.Context, key string) (SystemSetting, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	setting, ok := m.settings[key]
	if !ok {
		return SystemSetting{}, ErrNotFound
	}
	setting.ValueJSON = append([]byte(nil), setting.ValueJSON...)
	return setting, nil
}

func (m *Memory) ListSystemSettings(ctx context.Context) ([]SystemSetting, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SystemSetting, 0, len(m.settings))
	for _, setting := range m.settings {
		setting.ValueJSON = append([]byte(nil), setting.ValueJSON...)
		out = append(out, setting)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *Memory) SetSystemSetting(ctx context.Context, setting SystemSetting, expectedVersion int64) (SystemSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, exists := m.settings[setting.Key]
	currentVersion := int64(0)
	if exists {
		currentVersion = current.Version
	}
	if expectedVersion >= 0 && expectedVersion != currentVersion {
		return SystemSetting{}, ErrConflict
	}
	setting.Version = currentVersion + 1
	setting.ValueJSON = append([]byte(nil), setting.ValueJSON...)
	m.settings[setting.Key] = setting
	return setting, nil
}

func (m *Memory) DeleteSystemSetting(ctx context.Context, key string, expectedVersion int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, exists := m.settings[key]
	if !exists {
		if expectedVersion > 0 {
			return ErrConflict
		}
		return ErrNotFound
	}
	if expectedVersion >= 0 && expectedVersion != current.Version {
		return ErrConflict
	}
	delete(m.settings, key)
	return nil
}

// ---- Skills ----

func normalizeSkill(s Skill) Skill {
	if s.Scope == "" {
		s.Scope = "admin"
	}
	if s.SourceType == "" {
		s.SourceType = "manual"
	}
	if s.ManifestJSON == nil {
		s.ManifestJSON = []byte("[]")
	}
	if s.FrontmatterJSON == nil {
		s.FrontmatterJSON = []byte("{}")
	}
	return s
}

func cloneSkill(s Skill) Skill {
	s = normalizeSkill(s)
	s.ManifestJSON = append([]byte(nil), s.ManifestJSON...)
	s.FrontmatterJSON = append([]byte(nil), s.FrontmatterJSON...)
	return s
}

func skillPrefKey(userID, skillID string) string { return userID + "/" + skillID }

func (m *Memory) CreateSkill(ctx context.Context, s Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[s.ID]; ok {
		return ErrConflict
	}
	m.skills[s.ID] = cloneSkill(s)
	return nil
}

func (m *Memory) GetSkill(ctx context.Context, id string) (Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[id]
	if !ok {
		return Skill{}, ErrNotFound
	}
	return cloneSkill(s), nil
}

func (m *Memory) ListSkills(ctx context.Context, onlyEnabled bool) ([]Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Skill, 0, len(m.skills))
	for _, s := range m.skills {
		if onlyEnabled && !s.Enabled {
			continue
		}
		out = append(out, cloneSkill(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) ListSkillsForUser(ctx context.Context, userID string) ([]Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Skill, 0)
	for _, s := range m.skills {
		s = normalizeSkill(s)
		if s.Scope == "user" && s.OwnerUserID == userID {
			out = append(out, cloneSkill(s))
		}
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
	m.skills[s.ID] = cloneSkill(s)
	return nil
}

func (m *Memory) DeleteSkill(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[id]; !ok {
		return ErrNotFound
	}
	delete(m.skills, id)
	for key, pref := range m.skillPrefs {
		if pref.SkillID == id {
			delete(m.skillPrefs, key)
		}
	}
	return nil
}

func (m *Memory) SetUserSkillPreference(ctx context.Context, pref UserSkillPreference) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[pref.SkillID]; !ok {
		return ErrNotFound
	}
	m.skillPrefs[skillPrefKey(pref.UserID, pref.SkillID)] = pref
	return nil
}

func (m *Memory) ListUserSkillPreferences(ctx context.Context, userID string) ([]UserSkillPreference, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]UserSkillPreference, 0)
	for _, pref := range m.skillPrefs {
		if pref.UserID == userID {
			out = append(out, pref)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SkillID < out[j].SkillID })
	return out, nil
}

func (m *Memory) DeleteUserSkillPreference(ctx context.Context, userID, skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.skillPrefs, skillPrefKey(userID, skillID))
	return nil
}

// ---- MCP servers ----

func normalizeMCPServer(s MCPServer) MCPServer {
	if s.ArgsJSON == nil {
		s.ArgsJSON = []byte("[]")
	}
	if s.EnvCiphertextJSON == nil {
		s.EnvCiphertextJSON = []byte("{}")
	}
	if s.EnvHintJSON == nil {
		s.EnvHintJSON = []byte("{}")
	}
	if s.URLVarCiphertextJSON == nil {
		s.URLVarCiphertextJSON = []byte("{}")
	}
	if s.URLVarHintJSON == nil {
		s.URLVarHintJSON = []byte("{}")
	}
	if s.HeaderCiphertextJSON == nil {
		s.HeaderCiphertextJSON = []byte("{}")
	}
	if s.HeaderHintJSON == nil {
		s.HeaderHintJSON = []byte("{}")
	}
	if s.Source == "" {
		s.Source = "admin"
	}
	if s.Status == "" {
		s.Status = "active"
	}
	return s
}

func cloneMCPServer(s MCPServer) MCPServer {
	s = normalizeMCPServer(s)
	s.ArgsJSON = append([]byte(nil), s.ArgsJSON...)
	s.URLVarCiphertextJSON = append([]byte(nil), s.URLVarCiphertextJSON...)
	s.URLVarHintJSON = append([]byte(nil), s.URLVarHintJSON...)
	s.EnvCiphertextJSON = append([]byte(nil), s.EnvCiphertextJSON...)
	s.EnvHintJSON = append([]byte(nil), s.EnvHintJSON...)
	s.HeaderCiphertextJSON = append([]byte(nil), s.HeaderCiphertextJSON...)
	s.HeaderHintJSON = append([]byte(nil), s.HeaderHintJSON...)
	return s
}

func mcpPrefKey(userID, mcpID string) string { return userID + "/" + mcpID }

func (m *Memory) CreateMCPServer(ctx context.Context, s MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mcps[s.ID]; ok {
		return ErrConflict
	}
	m.mcps[s.ID] = cloneMCPServer(s)
	return nil
}

func (m *Memory) GetMCPServer(ctx context.Context, id string) (MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.mcps[id]
	if !ok {
		return MCPServer{}, ErrNotFound
	}
	return cloneMCPServer(s), nil
}

func (m *Memory) ListMCPServers(ctx context.Context, onlyEnabled bool) ([]MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MCPServer, 0, len(m.mcps))
	for _, s := range m.mcps {
		if onlyEnabled && !s.Enabled {
			continue
		}
		out = append(out, cloneMCPServer(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) UpdateMCPServer(ctx context.Context, s MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mcps[s.ID]; !ok {
		return ErrNotFound
	}
	m.mcps[s.ID] = cloneMCPServer(s)
	return nil
}

func (m *Memory) DeleteMCPServer(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mcps[id]; !ok {
		return ErrNotFound
	}
	delete(m.mcps, id)
	for key, pref := range m.mcpPrefs {
		if pref.MCPID == id {
			delete(m.mcpPrefs, key)
		}
	}
	return nil
}

func (m *Memory) SetUserMCPPreference(ctx context.Context, pref UserMCPPreference) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mcps[pref.MCPID]; !ok {
		return ErrNotFound
	}
	m.mcpPrefs[mcpPrefKey(pref.UserID, pref.MCPID)] = pref
	return nil
}

func (m *Memory) ListUserMCPPreferences(ctx context.Context, userID string) ([]UserMCPPreference, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]UserMCPPreference, 0)
	for _, pref := range m.mcpPrefs {
		if pref.UserID == userID {
			out = append(out, pref)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MCPID < out[j].MCPID })
	return out, nil
}

func (m *Memory) DeleteUserMCPPreference(ctx context.Context, userID, mcpID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mcpPrefs, mcpPrefKey(userID, mcpID))
	return nil
}

// ---- LLM model configuration ----

func (m *Memory) CreateLLMProvider(ctx context.Context, p LLMProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.llmProviders[p.ID]; ok {
		return ErrConflict
	}
	m.llmProviders[p.ID] = p
	return nil
}

func (m *Memory) GetLLMProvider(ctx context.Context, id string) (LLMProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.llmProviders[id]
	if !ok {
		return LLMProvider{}, ErrNotFound
	}
	return p, nil
}

func (m *Memory) ListLLMProviders(ctx context.Context) ([]LLMProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LLMProvider, 0, len(m.llmProviders))
	for _, p := range m.llmProviders {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) UpdateLLMProvider(ctx context.Context, p LLMProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.llmProviders[p.ID]; !ok {
		return ErrNotFound
	}
	m.llmProviders[p.ID] = p
	return nil
}

func (m *Memory) DeleteLLMProvider(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.llmProviders[id]; !ok {
		return ErrNotFound
	}
	for _, route := range m.llmModels {
		if route.ProviderID == id {
			return ErrConflict
		}
	}
	delete(m.llmProviders, id)
	return nil
}

func (m *Memory) CreateLLMModelRoute(ctx context.Context, route LLMModelRoute) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.llmModels[route.Alias]; ok {
		return ErrConflict
	}
	if _, ok := m.llmProviders[route.ProviderID]; !ok {
		return ErrNotFound
	}
	if route.IsDefault {
		for alias, existing := range m.llmModels {
			existing.IsDefault = false
			m.llmModels[alias] = existing
		}
	}
	m.llmModels[route.Alias] = route
	return nil
}

func (m *Memory) GetLLMModelRoute(ctx context.Context, alias string) (LLMModelRoute, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	route, ok := m.llmModels[alias]
	if !ok {
		return LLMModelRoute{}, ErrNotFound
	}
	return route, nil
}

func (m *Memory) ListLLMModelRoutes(ctx context.Context) ([]LLMModelRoute, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LLMModelRoute, 0, len(m.llmModels))
	for _, route := range m.llmModels {
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].Alias < out[j].Alias
	})
	return out, nil
}

func (m *Memory) UpdateLLMModelRoute(ctx context.Context, route LLMModelRoute) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.llmModels[route.Alias]; !ok {
		return ErrNotFound
	}
	if _, ok := m.llmProviders[route.ProviderID]; !ok {
		return ErrNotFound
	}
	if route.IsDefault {
		for alias, existing := range m.llmModels {
			if alias == route.Alias {
				continue
			}
			existing.IsDefault = false
			m.llmModels[alias] = existing
		}
	}
	m.llmModels[route.Alias] = route
	return nil
}

func (m *Memory) DeleteLLMModelRoute(ctx context.Context, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.llmModels[alias]; !ok {
		return ErrNotFound
	}
	delete(m.llmModels, alias)
	return nil
}

// ---- Scheduled system tasks ----

func cloneTask(t ScheduledTask) ScheduledTask {
	t.ScheduleSpec = append([]byte(nil), t.ScheduleSpec...)
	t.ConfigJSON = append([]byte(nil), t.ConfigJSON...)
	return t
}

func cloneRunEvent(e ScheduledTaskRunEvent) ScheduledTaskRunEvent {
	e.DataJSON = append([]byte(nil), e.DataJSON...)
	return e
}

func (m *Memory) CreateScheduledTask(ctx context.Context, task ScheduledTask, attachments []ScheduledTaskAttachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[task.ID]; ok {
		return ErrConflict
	}
	m.tasks[task.ID] = cloneTask(task)
	for _, att := range attachments {
		if _, ok := m.attachments[att.ID]; ok {
			return ErrConflict
		}
		m.attachments[att.ID] = att
	}
	return nil
}

func (m *Memory) GetScheduledTask(ctx context.Context, id string) (ScheduledTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return ScheduledTask{}, ErrNotFound
	}
	return cloneTask(task), nil
}

func (m *Memory) GetScheduledTaskForOwner(ctx context.Context, id, ownerUserID string) (ScheduledTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok || task.OwnerType != "user" || task.OwnerUserID != ownerUserID {
		return ScheduledTask{}, ErrNotFound
	}
	return cloneTask(task), nil
}

func (m *Memory) ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		out = append(out, cloneTask(task))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (m *Memory) ListScheduledTasksForOwner(ctx context.Context, ownerUserID string) ([]ScheduledTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTask, 0)
	for _, task := range m.tasks {
		if task.OwnerType == "user" && task.OwnerUserID == ownerUserID {
			out = append(out, cloneTask(task))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (m *Memory) UpdateScheduledTask(ctx context.Context, task ScheduledTask, replaceAttachments bool, attachments []ScheduledTaskAttachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[task.ID]; !ok {
		return ErrNotFound
	}
	m.tasks[task.ID] = cloneTask(task)
	if replaceAttachments {
		for id, att := range m.attachments {
			if att.TaskID == task.ID {
				delete(m.attachments, id)
			}
		}
		for _, att := range attachments {
			m.attachments[att.ID] = att
		}
	}
	return nil
}

func (m *Memory) DeleteScheduledTask(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[id]; !ok {
		return ErrNotFound
	}
	delete(m.tasks, id)
	for attID, att := range m.attachments {
		if att.TaskID == id {
			delete(m.attachments, attID)
		}
	}
	for runID, run := range m.runs {
		if run.TaskID == id {
			delete(m.runs, runID)
			delete(m.runEvents, runID)
		}
	}
	return nil
}

func (m *Memory) DeleteScheduledTaskForOwner(ctx context.Context, id, ownerUserID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok || task.OwnerType != "user" || task.OwnerUserID != ownerUserID {
		return ErrNotFound
	}
	delete(m.tasks, id)
	for attID, att := range m.attachments {
		if att.TaskID == id {
			delete(m.attachments, attID)
		}
	}
	for runID, run := range m.runs {
		if run.TaskID == id {
			delete(m.runs, runID)
			delete(m.runEvents, runID)
		}
	}
	return nil
}

func (m *Memory) ListScheduledTaskAttachments(ctx context.Context, taskID string) ([]ScheduledTaskAttachment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTaskAttachment, 0)
	for _, att := range m.attachments {
		if att.TaskID == taskID {
			out = append(out, att)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) ListDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]ScheduledTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTask, 0)
	for _, task := range m.tasks {
		if task.Status != "active" || task.NextRunAt.IsZero() || task.NextRunAt.After(now) {
			continue
		}
		out = append(out, cloneTask(task))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextRunAt.Before(out[j].NextRunAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) TryStartScheduledTaskRun(ctx context.Context, taskID string, run ScheduledTaskRun, nextRunAt time.Time) (ScheduledTask, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return ScheduledTask{}, false, ErrNotFound
	}
	if task.Status != "active" || task.NextRunAt.IsZero() || task.NextRunAt.After(run.ScheduledFor) {
		return ScheduledTask{}, false, nil
	}
	for _, existing := range m.runs {
		if existing.TaskID == taskID && (existing.Status == "queued" || existing.Status == "running") {
			return ScheduledTask{}, false, nil
		}
	}
	if _, ok := m.runs[run.ID]; ok {
		return ScheduledTask{}, false, ErrConflict
	}
	task.NextRunAt = nextRunAt
	task.UpdatedAt = run.UpdatedAt
	if task.OwnerType == "user" && task.ConversationID == "" {
		task.ConversationID = "sched-" + task.ID
	}
	m.tasks[taskID] = cloneTask(task)
	m.runs[run.ID] = run
	return cloneTask(task), true, nil
}

func (m *Memory) GetScheduledTaskRun(ctx context.Context, id string) (ScheduledTaskRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	run, ok := m.runs[id]
	if !ok {
		return ScheduledTaskRun{}, ErrNotFound
	}
	return run, nil
}

func (m *Memory) ListScheduledTaskRuns(ctx context.Context, taskID, status string, limit int) ([]ScheduledTaskRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTaskRun, 0)
	for _, run := range m.runs {
		if taskID != "" && run.TaskID != taskID {
			continue
		}
		if status != "" && run.Status != status {
			continue
		}
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) HeartbeatScheduledTaskRun(ctx context.Context, id, workerID string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return false, ErrNotFound
	}
	if run.Status != "running" || (workerID != "" && run.WorkerID != workerID) {
		return false, nil
	}
	run.UpdatedAt = now
	m.runs[id] = run
	return true, nil
}

func (m *Memory) ExpireStaleScheduledTaskRuns(ctx context.Context, before, now time.Time, errText string, limit int) ([]ScheduledTaskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidates := make([]ScheduledTaskRun, 0)
	for _, run := range m.runs {
		updatedAt := run.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = run.CreatedAt
		}
		if run.Status == "running" && !updatedAt.IsZero() && updatedAt.Before(before) {
			candidates = append(candidates, run)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].UpdatedAt.Before(candidates[j].UpdatedAt) })
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	expired := make([]ScheduledTaskRun, 0, len(candidates))
	for _, run := range candidates {
		run.Status = "error"
		run.Error = errText
		run.FinishedAt = now
		run.UpdatedAt = now
		m.runs[run.ID] = run
		task, ok := m.tasks[run.TaskID]
		if !ok {
			return nil, ErrNotFound
		}
		task.LastRunAt = now
		task.LastStatus = run.Status
		task.LastError = run.Error
		task.UpdatedAt = now
		if task.ScheduleKind == "once" && task.NextRunAt.IsZero() {
			task.Status = "completed"
		}
		task.RunCount++
		m.tasks[task.ID] = cloneTask(task)
		expired = append(expired, run)
	}
	return expired, nil
}

func (m *Memory) UpdateScheduledTaskRun(ctx context.Context, run ScheduledTaskRun, taskNextRunAt time.Time, terminal bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[run.ID]; !ok {
		return ErrNotFound
	}
	m.runs[run.ID] = run
	if terminal {
		task, ok := m.tasks[run.TaskID]
		if !ok {
			return ErrNotFound
		}
		task.LastRunAt = run.FinishedAt
		task.LastStatus = run.Status
		task.LastError = run.Error
		task.UpdatedAt = run.UpdatedAt
		task.NextRunAt = taskNextRunAt
		if task.ScheduleKind == "once" && taskNextRunAt.IsZero() {
			task.Status = "completed"
		}
		task.RunCount++
		m.tasks[task.ID] = cloneTask(task)
	}
	return nil
}

func (m *Memory) AppendScheduledTaskRunEvent(ctx context.Context, event ScheduledTaskRunEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[event.RunID]; !ok {
		return ErrNotFound
	}
	m.runEventSeq++
	event.ID = m.runEventSeq
	m.runEvents[event.RunID] = append(m.runEvents[event.RunID], cloneRunEvent(event))
	return nil
}

func (m *Memory) ListScheduledTaskRunEvents(ctx context.Context, runID string) ([]ScheduledTaskRunEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.runEvents[runID]
	out := make([]ScheduledTaskRunEvent, 0, len(events))
	for _, event := range events {
		out = append(out, cloneRunEvent(event))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// ---- Audit ----

func (m *Memory) AppendAudit(ctx context.Context, e AuditEntry) error {
	detail := map[string]any{}
	detail["legacy_entry"] = true
	if e.Detail != "" {
		detail["detail"] = e.Detail
	}
	return m.AppendAuditEvent(ctx, AuditEvent{
		At:           e.At,
		ActorType:    "admin",
		ActorUserID:  e.Actor,
		ActorEmail:   e.Actor,
		Action:       e.Action,
		ResourceType: auditResourceType(e.Action),
		ResourceID:   e.Resource,
		Result:       "success",
		Metadata:     detail,
	})
}

func (m *Memory) AppendAuditEvent(ctx context.Context, e AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditSeq++
	e.ID = m.auditSeq
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.Result == "" {
		e.Result = "success"
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	m.audit = append(m.audit, e)
	return nil
}

func (m *Memory) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	events, err := m.ListAuditEvents(ctx, AuditEventQuery{Limit: limit, LegacyOnly: true})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, 0)
	for _, e := range events {
		if isLegacyAuditEvent(e) {
			out = append(out, legacyAuditEntry(e))
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *Memory) ListAuditEvents(ctx context.Context, q AuditEventQuery) ([]AuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.audit)
	limit := q.Limit
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]AuditEvent, 0, limit)
	skipped := 0
	// newest first
	for i := n - 1; i >= 0 && len(out) < limit; i-- {
		if auditMatches(m.audit[i], q) {
			if skipped < q.Offset {
				skipped++
				continue
			}
			out = append(out, cloneAuditEvent(m.audit[i]))
		}
	}
	return out, nil
}

func auditMatches(e AuditEvent, q AuditEventQuery) bool {
	if q.LegacyOnly && !isLegacyAuditEvent(e) {
		return false
	}
	if q.ActorUserID != "" && e.ActorUserID != q.ActorUserID {
		return false
	}
	if q.ActorEmail != "" && e.ActorEmail != q.ActorEmail {
		return false
	}
	if q.Action != "" && e.Action != q.Action {
		return false
	}
	if q.ResourceType != "" && e.ResourceType != q.ResourceType {
		return false
	}
	if q.ResourceID != "" && e.ResourceID != q.ResourceID {
		return false
	}
	if q.Result != "" && e.Result != q.Result {
		return false
	}
	if q.RequestID != "" && e.RequestID != q.RequestID {
		return false
	}
	if q.TraceID != "" && e.TraceID != q.TraceID {
		return false
	}
	if !q.Since.IsZero() && e.At.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && e.At.After(q.Until) {
		return false
	}
	return true
}

func cloneAuditEvent(e AuditEvent) AuditEvent {
	if e.Metadata != nil {
		m := make(map[string]any, len(e.Metadata))
		for k, v := range e.Metadata {
			m[k] = v
		}
		e.Metadata = m
	}
	return e
}

func (m *Memory) ListTraceEvents(ctx context.Context, q TraceEventQuery) ([]TraceEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if q.TraceID == "" {
		return []TraceEvent{}, nil
	}
	limit := q.Limit
	if limit <= 0 || limit > len(m.traceEvents) {
		limit = len(m.traceEvents)
	}
	out := make([]TraceEvent, 0, limit)
	for _, event := range m.traceEvents {
		if event.TraceID != q.TraceID {
			continue
		}
		out = append(out, cloneTraceEvent(event))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func cloneTraceEvent(e TraceEvent) TraceEvent {
	if e.Metadata != nil {
		m := make(map[string]any, len(e.Metadata))
		for k, v := range e.Metadata {
			m[k] = v
		}
		e.Metadata = m
	}
	return e
}
