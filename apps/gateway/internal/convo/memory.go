package convo

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory is the in-process Store used by hermetic tests. Value semantics:
// returned slices are freshly built so
// callers cannot mutate shared state. Not durable — data is lost on restart.
type Memory struct {
	mu      sync.RWMutex
	convs   map[string]Conversation
	msgs    map[string][]Message // conversation_id -> messages (append order)
	arts    map[string]Artifact  // artifact_id -> metadata
	folders map[string]Folder
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		convs:   make(map[string]Conversation),
		msgs:    make(map[string][]Message),
		arts:    make(map[string]Artifact),
		folders: make(map[string]Folder),
	}
}

func (m *Memory) UpsertConversation(_ context.Context, c Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ChatType == "" {
		c.ChatType = "chat"
	}
	if c.RuntimeID == "" {
		c.RuntimeID = DefaultRuntimeID
	}
	if c.FolderID != "" && c.ProjectID != "" {
		return ErrUnsupportedChatType
	}
	if c.ProjectID != "" && c.ChatType != "chat" {
		return ErrUnsupportedChatType
	}
	if c.FolderID != "" {
		if c.ChatType != "chat" {
			return ErrUnsupportedChatType
		}
		if folder, ok := m.folders[c.FolderID]; !ok || folder.UserID != c.UserID {
			return ErrNotFound
		}
	}
	if existing, ok := m.convs[c.ID]; ok {
		if c.UserID != "" && existing.UserID != c.UserID {
			return ErrNotFound
		}
		if existing.RuntimeID != c.RuntimeID {
			return ErrRuntimeMismatch
		}
		if c.ProjectID != "" && existing.ProjectID != c.ProjectID {
			return ErrProjectMismatch
		}
		// Refresh updated_at only; keep the original title (MVP: never overwrite).
		existing.UpdatedAt = c.UpdatedAt
		m.convs[c.ID] = existing
		return nil
	}
	if c.UserID == "" {
		return ErrNotFound
	}
	m.convs[c.ID] = c
	return nil
}

func (m *Memory) ListFolders(_ context.Context, userID string) ([]Folder, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Folder, 0)
	for _, folder := range m.folders {
		if folder.UserID == userID {
			out = append(out, folder)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if left == right {
			return out[i].ID < out[j].ID
		}
		return left < right
	})
	return out, nil
}

func (m *Memory) GetFolder(_ context.Context, folderID, userID string) (Folder, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	folder, ok := m.folders[folderID]
	if !ok || folder.UserID != userID {
		return Folder{}, ErrNotFound
	}
	return folder, nil
}

func (m *Memory) CreateFolder(_ context.Context, folder Folder) (Folder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, err := normalizeFolderName(folder.Name)
	if err != nil {
		return Folder{}, err
	}
	folder.Name = name
	for _, existing := range m.folders {
		if existing.UserID == folder.UserID && strings.EqualFold(existing.Name, folder.Name) {
			return Folder{}, ErrFolderNameConflict
		}
	}
	m.folders[folder.ID] = folder
	return folder, nil
}

func (m *Memory) RenameFolder(_ context.Context, folderID, userID, name string, updatedAt time.Time) (Folder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, err := normalizeFolderName(name)
	if err != nil {
		return Folder{}, err
	}
	folder, ok := m.folders[folderID]
	if !ok || folder.UserID != userID {
		return Folder{}, ErrNotFound
	}
	for id, existing := range m.folders {
		if id != folderID && existing.UserID == userID && strings.EqualFold(existing.Name, name) {
			return Folder{}, ErrFolderNameConflict
		}
	}
	folder.Name = name
	folder.UpdatedAt = updatedAt
	m.folders[folderID] = folder
	return folder, nil
}

func (m *Memory) ListFolderConversationIDs(_ context.Context, folderID, userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	folder, ok := m.folders[folderID]
	if !ok || folder.UserID != userID {
		return nil, ErrNotFound
	}
	ids := make([]string, 0)
	for _, conversation := range m.convs {
		if conversation.UserID == userID && conversation.FolderID == folderID {
			ids = append(ids, conversation.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (m *Memory) DeleteFolder(_ context.Context, folderID, userID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	folder, ok := m.folders[folderID]
	if !ok || folder.UserID != userID {
		return nil, ErrNotFound
	}
	ids := make([]string, 0)
	for id, conversation := range m.convs {
		if conversation.UserID != userID || conversation.FolderID != folderID {
			continue
		}
		ids = append(ids, id)
		delete(m.convs, id)
		delete(m.msgs, id)
		for artifactID, artifact := range m.arts {
			if artifact.ConversationID == id {
				delete(m.arts, artifactID)
			}
		}
	}
	delete(m.folders, folderID)
	sort.Strings(ids)
	return ids, nil
}

func (m *Memory) MoveConversation(_ context.Context, convID, userID, folderID string, updatedAt time.Time) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.convs[convID]
	if !ok || conversation.UserID != userID {
		return Conversation{}, ErrNotFound
	}
	if conversation.ChatType != "chat" {
		return Conversation{}, ErrUnsupportedChatType
	}
	if conversation.ProjectID != "" {
		return Conversation{}, ErrUnsupportedChatType
	}
	if folderID != "" {
		folder, exists := m.folders[folderID]
		if !exists || folder.UserID != userID {
			return Conversation{}, ErrNotFound
		}
	}
	conversation.FolderID = folderID
	conversation.UpdatedAt = updatedAt
	m.convs[convID] = conversation
	return conversation, nil
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
	for _, existing := range m.msgs[msg.ConversationID] {
		if existing.ID == msg.ID {
			return nil
		}
	}
	m.msgs[msg.ConversationID] = append(m.msgs[msg.ConversationID], msg)
	return nil
}

func (m *Memory) UpsertMessage(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	messages := m.msgs[msg.ConversationID]
	for i := range messages {
		if messages[i].ID == msg.ID {
			messages[i] = msg
			m.msgs[msg.ConversationID] = messages
			return nil
		}
	}
	m.msgs[msg.ConversationID] = append(messages, msg)
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
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		leftRole, rightRole := messageRoleOrder(out[i].Role), messageRoleOrder(out[j].Role)
		if leftRole != rightRole {
			return leftRole < rightRole
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func messageRoleOrder(role string) int {
	switch role {
	case "user":
		return 0
	case "assistant":
		return 1
	default:
		return 2
	}
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
