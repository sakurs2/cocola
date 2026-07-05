package convo

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is the in-process Store used for tests and zero-dependency dev boots
// (no COCOLA_PG_DSN). Value semantics: returned slices are freshly built so
// callers cannot mutate shared state. Not durable — data is lost on restart.
type Memory struct {
	mu    sync.RWMutex
	convs map[string]Conversation
	msgs  map[string][]Message // conversation_id -> messages (append order)
	arts  map[string]Artifact  // artifact_id -> metadata
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		convs: make(map[string]Conversation),
		msgs:  make(map[string][]Message),
		arts:  make(map[string]Artifact),
	}
}

func (m *Memory) UpsertConversation(_ context.Context, c Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ChatType == "" {
		c.ChatType = "chat"
	}
	if existing, ok := m.convs[c.ID]; ok {
		// Refresh updated_at only; keep the original title (MVP: never overwrite).
		existing.UpdatedAt = c.UpdatedAt
		m.convs[c.ID] = existing
		return nil
	}
	m.convs[c.ID] = c
	return nil
}

func (m *Memory) RevealConversation(_ context.Context, convID, userID, title string, updatedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[convID]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	if title != "" {
		c.Title = title
	}
	c.Hidden = false
	c.UpdatedAt = updatedAt
	m.convs[convID] = c
	return nil
}

func (m *Memory) InsertMessage(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs[msg.ConversationID] = append(m.msgs[msg.ConversationID], msg)
	return nil
}

func (m *Memory) ListConversations(_ context.Context, userID string) ([]Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Conversation, 0)
	for _, c := range m.convs {
		if c.UserID == userID && !c.Hidden {
			out = append(out, c)
		}
	}
	// Most-recently-updated first; ties broken by id for determinism.
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (m *Memory) GetConversation(_ context.Context, convID, userID string) (Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.convs[convID]
	if !ok || c.UserID != userID {
		return Conversation{}, ErrNotFound
	}
	return c, nil
}

func (m *Memory) GetMessages(_ context.Context, convID, userID string) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.convs[convID]
	if !ok || c.UserID != userID {
		return nil, ErrNotFound
	}
	src := m.msgs[convID]
	out := make([]Message, len(src))
	copy(out, src)
	return out, nil
}

func (m *Memory) RenameConversation(_ context.Context, convID, userID, title string) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[convID]
	if !ok || c.UserID != userID {
		return Conversation{}, ErrNotFound
	}
	c.Title = title
	m.convs[convID] = c
	return c, nil
}

func (m *Memory) DeleteConversation(_ context.Context, convID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[convID]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	delete(m.convs, convID)
	delete(m.msgs, convID)
	for id, a := range m.arts {
		if a.ConversationID == convID {
			delete(m.arts, id)
		}
	}
	return nil
}

func (m *Memory) UpsertArtifact(_ context.Context, a Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.arts[a.ID] = a
	return nil
}

func (m *Memory) GetArtifact(_ context.Context, convID, artifactID, userID string) (Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.convs[convID]
	if !ok || c.UserID != userID {
		return Artifact{}, ErrNotFound
	}
	a, ok := m.arts[artifactID]
	if !ok || a.ConversationID != convID || a.UserID != userID {
		return Artifact{}, ErrNotFound
	}
	return a, nil
}
