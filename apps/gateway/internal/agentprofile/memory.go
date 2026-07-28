package agentprofile

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

type Memory struct {
	mu     sync.RWMutex
	agents map[string]Agent
}

var _ Store = (*Memory)(nil)

func NewMemory() *Memory {
	return &Memory{agents: make(map[string]Agent)}
}

func (m *Memory) Close() {}

func (m *Memory) List(_ context.Context, id Identity) ([]Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Agent, 0)
	for _, value := range m.agents {
		if value.TenantID == id.TenantID && value.OwnerUserID == id.UserID &&
			value.Status == StatusActive {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	for index := range result {
		result[index] = cloneAgent(result[index])
	}
	return result, nil
}

func (m *Memory) Get(_ context.Context, id Identity, agentID string) (Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.agents[agentID]
	if !ok || value.TenantID != id.TenantID || value.OwnerUserID != id.UserID {
		return Agent{}, ErrNotFound
	}
	return cloneAgent(value), nil
}

func (m *Memory) Create(_ context.Context, value Agent) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[value.ID]; ok || m.nameExists(value, "") {
		return Agent{}, ErrConflict
	}
	value = cloneAgent(value)
	m.agents[value.ID] = value
	return cloneAgent(value), nil
}

func (m *Memory) Update(_ context.Context, id Identity, value Agent, expected int64) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.agents[value.ID]
	if !ok || current.TenantID != id.TenantID || current.OwnerUserID != id.UserID {
		return Agent{}, ErrNotFound
	}
	if current.Status != StatusActive {
		return Agent{}, ErrArchived
	}
	if current.Version != expected {
		return Agent{}, ErrVersionConflict
	}
	candidate := value
	candidate.TenantID = current.TenantID
	candidate.OwnerUserID = current.OwnerUserID
	candidate.Status = current.Status
	candidate.Version = current.Version + 1
	candidate.KnowledgeRevision = current.KnowledgeRevision
	if !knowledgeSourcesEqual(current.KnowledgeSources, candidate.KnowledgeSources) {
		candidate.KnowledgeRevision++
	}
	candidate.CreatedAt = current.CreatedAt
	if m.nameExists(candidate, candidate.ID) {
		return Agent{}, ErrConflict
	}
	candidate = cloneAgent(candidate)
	m.agents[value.ID] = candidate
	return cloneAgent(candidate), nil
}

func knowledgeSourcesEqual(left, right []KnowledgeSource) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (m *Memory) Archive(
	_ context.Context,
	id Identity,
	agentID string,
	expected int64,
	now time.Time,
) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.agents[agentID]
	if !ok || current.TenantID != id.TenantID || current.OwnerUserID != id.UserID {
		return Agent{}, ErrNotFound
	}
	if current.Version != expected {
		return Agent{}, ErrVersionConflict
	}
	if current.Status == StatusArchived {
		return cloneAgent(current), nil
	}
	current.Status = StatusArchived
	current.Version++
	current.UpdatedAt = now
	current.ArchivedAt = &now
	m.agents[agentID] = current
	return cloneAgent(current), nil
}

func cloneAgent(value Agent) Agent {
	value.SkillIDs = append([]string(nil), value.SkillIDs...)
	value.KnowledgeSources = append([]KnowledgeSource(nil), value.KnowledgeSources...)
	return value
}

func (m *Memory) nameExists(candidate Agent, excludeID string) bool {
	for _, existing := range m.agents {
		if existing.ID != excludeID && existing.TenantID == candidate.TenantID &&
			existing.OwnerUserID == candidate.OwnerUserID && existing.Status == StatusActive &&
			strings.EqualFold(existing.Name, candidate.Name) {
			return true
		}
	}
	return false
}
