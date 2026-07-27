package agentprofile

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var validAvatarKeys = map[string]struct{}{
	"sparkle": {}, "robot": {}, "code": {}, "chart": {},
	"document": {}, "search": {}, "briefcase": {}, "support": {},
}

var validAvatarColors = map[string]struct{}{
	"slate": {}, "blue": {}, "cyan": {}, "emerald": {},
	"amber": {}, "orange": {}, "rose": {}, "violet": {},
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) List(ctx context.Context, id Identity) ([]Agent, error) {
	if !validIdentity(id) {
		return nil, ErrInvalidArgument
	}
	return s.store.List(ctx, id)
}

func (s *Service) Get(ctx context.Context, id Identity, agentID string) (Agent, error) {
	if !validIdentity(id) || !validUUID(agentID) {
		return Agent{}, ErrInvalidArgument
	}
	return s.store.Get(ctx, id, agentID)
}

func (s *Service) GetActive(ctx context.Context, id Identity, agentID string) (Agent, error) {
	agent, err := s.Get(ctx, id, agentID)
	if err != nil {
		return Agent{}, err
	}
	if agent.Status != StatusActive {
		return Agent{}, ErrArchived
	}
	return agent, nil
}

func (s *Service) Create(ctx context.Context, id Identity, input CreateInput) (Agent, error) {
	if !validIdentity(id) {
		return Agent{}, ErrInvalidArgument
	}
	input = normalizeCreate(input)
	if !validInput(
		input.Name, input.Description, input.Instructions, input.AvatarKey,
		input.AvatarColor, input.RuntimeID, input.ModelRouteID, input.ModelAlias,
	) {
		return Agent{}, ErrInvalidArgument
	}
	now := s.now()
	return s.store.Create(ctx, Agent{
		ID: uuid.NewString(), TenantID: id.TenantID, OwnerUserID: id.UserID,
		Name: input.Name, Description: input.Description, Instructions: input.Instructions,
		AvatarKey: input.AvatarKey, AvatarColor: input.AvatarColor,
		RuntimeID: input.RuntimeID, ModelRouteID: input.ModelRouteID, ModelAlias: input.ModelAlias,
		Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Service) Update(
	ctx context.Context,
	id Identity,
	agentID string,
	input UpdateInput,
) (Agent, error) {
	if !validIdentity(id) || !validUUID(agentID) || input.Version <= 0 {
		return Agent{}, ErrInvalidArgument
	}
	input = normalizeUpdate(input)
	if !validInput(
		input.Name, input.Description, input.Instructions, input.AvatarKey,
		input.AvatarColor, input.RuntimeID, input.ModelRouteID, input.ModelAlias,
	) {
		return Agent{}, ErrInvalidArgument
	}
	return s.store.Update(ctx, id, Agent{
		ID: agentID, Name: input.Name, Description: input.Description,
		Instructions: input.Instructions, AvatarKey: input.AvatarKey,
		AvatarColor: input.AvatarColor, RuntimeID: input.RuntimeID,
		ModelRouteID: input.ModelRouteID, ModelAlias: input.ModelAlias,
		UpdatedAt: s.now(),
	}, input.Version)
}

func (s *Service) Archive(
	ctx context.Context,
	id Identity,
	agentID string,
	version int64,
) (Agent, error) {
	if !validIdentity(id) || !validUUID(agentID) || version <= 0 {
		return Agent{}, ErrInvalidArgument
	}
	return s.store.Archive(ctx, id, agentID, version, s.now())
}

func normalizeCreate(input CreateInput) CreateInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.AvatarKey = strings.TrimSpace(input.AvatarKey)
	input.AvatarColor = strings.TrimSpace(input.AvatarColor)
	input.RuntimeID = strings.TrimSpace(input.RuntimeID)
	input.ModelRouteID = strings.TrimSpace(input.ModelRouteID)
	input.ModelAlias = strings.TrimSpace(input.ModelAlias)
	if input.AvatarKey == "" {
		input.AvatarKey = "sparkle"
	}
	if input.AvatarColor == "" {
		input.AvatarColor = "blue"
	}
	return input
}

func normalizeUpdate(input UpdateInput) UpdateInput {
	normalized := normalizeCreate(CreateInput{
		Name: input.Name, Description: input.Description, Instructions: input.Instructions,
		AvatarKey: input.AvatarKey, AvatarColor: input.AvatarColor, RuntimeID: input.RuntimeID,
		ModelRouteID: input.ModelRouteID, ModelAlias: input.ModelAlias,
	})
	input.Name = normalized.Name
	input.Description = normalized.Description
	input.AvatarKey = normalized.AvatarKey
	input.AvatarColor = normalized.AvatarColor
	input.RuntimeID = normalized.RuntimeID
	input.ModelRouteID = normalized.ModelRouteID
	input.ModelAlias = normalized.ModelAlias
	return input
}

func validInput(name, description, instructions, avatarKey, avatarColor, runtimeID, modelRouteID, modelAlias string) bool {
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > MaxNameCharacters ||
		utf8.RuneCountInString(description) > MaxDescriptionCharacters ||
		len(instructions) > MaxInstructionsBytes || strings.ContainsRune(instructions, '\x00') {
		return false
	}
	if _, ok := validAvatarKeys[avatarKey]; !ok {
		return false
	}
	if _, ok := validAvatarColors[avatarColor]; !ok {
		return false
	}
	return validIdentifier(runtimeID, MaxRuntimeIDCharacters) &&
		validIdentifier(modelRouteID, MaxModelIDCharacters) &&
		validIdentifier(modelAlias, MaxModelIDCharacters)
}

func validIdentifier(value string, maxCharacters int) bool {
	length := utf8.RuneCountInString(value)
	return length >= 1 && length <= maxCharacters &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func validIdentity(id Identity) bool {
	return strings.TrimSpace(id.UserID) != ""
}

func validUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}
