// Package agentprofile owns user-created Agent definitions and immutable
// conversation snapshots of those definitions.
package agentprofile

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound        = errors.New("agentprofile: not found")
	ErrConflict        = errors.New("agentprofile: conflict")
	ErrVersionConflict = errors.New("agentprofile: version conflict")
	ErrInvalidArgument = errors.New("agentprofile: invalid argument")
	ErrInUse           = errors.New("agentprofile: in use")
	ErrArchived        = errors.New("agentprofile: archived")
)

const (
	StatusActive   = "active"
	StatusArchived = "archived"

	MaxNameCharacters        = 100
	MaxDescriptionCharacters = 500
	MaxInstructionsBytes     = 32 * 1024
	MaxRuntimeIDCharacters   = 256
	MaxModelIDCharacters     = 256
	MaxSkillIDs              = 32
	MaxKnowledgeSources      = 10
	MaxKnowledgeLabelChars   = 100
	MaxKnowledgeURLChars     = 2048
	MaxSuggestedPrompts      = 4
	MaxSuggestedTitleChars   = 80
	MaxSuggestedPromptBytes  = 4 * 1024
)

type Identity struct {
	TenantID string
	UserID   string
}

type Agent struct {
	ID                string            `json:"id"`
	TenantID          string            `json:"-"`
	OwnerUserID       string            `json:"-"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	Instructions      string            `json:"instructions"`
	AvatarKey         string            `json:"avatar_key"`
	AvatarColor       string            `json:"avatar_color"`
	RuntimeID         string            `json:"runtime_id"`
	ModelRouteID      string            `json:"model_route_id"`
	ModelAlias        string            `json:"model_alias"`
	SkillIDs          []string          `json:"skill_ids"`
	KnowledgeSources  []KnowledgeSource `json:"knowledge_sources"`
	KnowledgeRevision int64             `json:"knowledge_revision"`
	SuggestedPrompts  []SuggestedPrompt `json:"suggested_prompts"`
	Status            string            `json:"status"`
	Version           int64             `json:"version"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	ArchivedAt        *time.Time        `json:"archived_at,omitempty"`
}

type KnowledgeSource struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	URL    string `json:"url,omitempty"`
	NodeID string `json:"node_id,omitempty"`
}

type SuggestedPrompt struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// Snapshot is copied into a conversation on its first turn. Behavior fields
// remain immutable for that conversation. KnowledgeSources and
// KnowledgeRevision are retained only as a last-known-good fallback when the
// live Agent configuration cannot be read at the beginning of a later turn.
type Snapshot struct {
	ID                string            `json:"id"`
	Version           int64             `json:"version"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	Instructions      string            `json:"instructions"`
	AvatarKey         string            `json:"avatar_key"`
	AvatarColor       string            `json:"avatar_color"`
	RuntimeID         string            `json:"runtime_id"`
	ModelRouteID      string            `json:"model_route_id"`
	ModelAlias        string            `json:"model_alias"`
	SkillIDs          []string          `json:"skill_ids"`
	KnowledgeSources  []KnowledgeSource `json:"knowledge_sources"`
	KnowledgeRevision int64             `json:"knowledge_revision"`
	SuggestedPrompts  []SuggestedPrompt `json:"suggested_prompts"`
}

func (a Agent) Snapshot() Snapshot {
	return Snapshot{
		ID: a.ID, Version: a.Version, Name: a.Name, Description: a.Description,
		Instructions: a.Instructions, AvatarKey: a.AvatarKey, AvatarColor: a.AvatarColor,
		RuntimeID: a.RuntimeID, ModelRouteID: a.ModelRouteID, ModelAlias: a.ModelAlias,
		SkillIDs:          append([]string(nil), a.SkillIDs...),
		KnowledgeSources:  append([]KnowledgeSource(nil), a.KnowledgeSources...),
		KnowledgeRevision: a.KnowledgeRevision,
		SuggestedPrompts:  append([]SuggestedPrompt(nil), a.SuggestedPrompts...),
	}
}

type CreateInput struct {
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Instructions     string            `json:"instructions"`
	AvatarKey        string            `json:"avatar_key"`
	AvatarColor      string            `json:"avatar_color"`
	RuntimeID        string            `json:"runtime_id"`
	ModelRouteID     string            `json:"model_route_id"`
	ModelAlias       string            `json:"model_alias"`
	SkillIDs         []string          `json:"skill_ids"`
	KnowledgeSources []KnowledgeSource `json:"knowledge_sources"`
	SuggestedPrompts []SuggestedPrompt `json:"suggested_prompts"`
}

type UpdateInput struct {
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Instructions     string            `json:"instructions"`
	AvatarKey        string            `json:"avatar_key"`
	AvatarColor      string            `json:"avatar_color"`
	RuntimeID        string            `json:"runtime_id"`
	ModelRouteID     string            `json:"model_route_id"`
	ModelAlias       string            `json:"model_alias"`
	SkillIDs         []string          `json:"skill_ids"`
	KnowledgeSources []KnowledgeSource `json:"knowledge_sources"`
	SuggestedPrompts []SuggestedPrompt `json:"suggested_prompts"`
	Version          int64             `json:"version"`
}

type Store interface {
	List(context.Context, Identity) ([]Agent, error)
	Get(context.Context, Identity, string) (Agent, error)
	Create(context.Context, Agent) (Agent, error)
	Update(context.Context, Identity, Agent, int64) (Agent, error)
	Archive(context.Context, Identity, string, int64, time.Time) (Agent, error)
	Close()
}
