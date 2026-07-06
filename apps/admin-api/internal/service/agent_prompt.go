package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const GlobalAgentPromptID = "global"

type AgentPromptInput struct {
	Name    string
	Content string
	Enabled *bool
	Actor   string
}

type AgentPromptRuntimeConfig struct {
	SystemPrompt string              `json:"system_prompt"`
	Prompts      []AgentPromptMarker `json:"prompts"`
}

type AgentPromptMarker struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	ContentLength int    `json:"content_length"`
}

func (a *Admin) DefaultAgentPrompt(ctx context.Context) (store.AgentPrompt, error) {
	prompt, err := a.store.GetAgentPrompt(ctx, GlobalAgentPromptID)
	if err == nil {
		return prompt, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.AgentPrompt{}, err
	}
	now := a.now().UTC()
	return store.AgentPrompt{
		ID:        GlobalAgentPromptID,
		Name:      "Global System Prompt",
		Scope:     "global",
		Priority:  100,
		Version:   0,
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (a *Admin) UpdateGlobalAgentPrompt(ctx context.Context, in AgentPromptInput) (store.AgentPrompt, error) {
	content := strings.TrimSpace(in.Content)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Global System Prompt"
	}
	now := a.now().UTC()
	current, err := a.store.GetAgentPrompt(ctx, GlobalAgentPromptID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.AgentPrompt{}, err
	}
	enabled := false
	if errors.Is(err, store.ErrNotFound) {
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		prompt := store.AgentPrompt{
			ID:        GlobalAgentPromptID,
			Name:      name,
			Content:   content,
			Enabled:   enabled,
			Scope:     "global",
			Priority:  100,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: in.Actor,
			UpdatedBy: in.Actor,
		}
		if err := a.store.CreateAgentPrompt(ctx, prompt); err != nil {
			return store.AgentPrompt{}, err
		}
		a.audit(ctx, in.Actor, "agent_prompt.update", prompt.ID, agentPromptAuditDetail(prompt))
		return prompt, nil
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	} else {
		enabled = current.Enabled
	}
	current.Name = name
	current.Content = content
	current.Enabled = enabled
	current.Scope = "global"
	current.Priority = 100
	current.Version++
	current.UpdatedAt = now
	current.UpdatedBy = in.Actor
	if err := a.store.UpdateAgentPrompt(ctx, current); err != nil {
		return store.AgentPrompt{}, err
	}
	a.audit(ctx, in.Actor, "agent_prompt.update", current.ID, agentPromptAuditDetail(current))
	return current, nil
}

func (a *Admin) SetGlobalAgentPromptEnabled(ctx context.Context, enabled bool, actor string) (store.AgentPrompt, error) {
	current, err := a.DefaultAgentPrompt(ctx)
	if err != nil {
		return store.AgentPrompt{}, err
	}
	return a.UpdateGlobalAgentPrompt(ctx, AgentPromptInput{
		Name:    current.Name,
		Content: current.Content,
		Enabled: &enabled,
		Actor:   actor,
	})
}

func (a *Admin) EffectiveAgentPromptRuntimeConfig(ctx context.Context, userID string) (AgentPromptRuntimeConfig, error) {
	prompts, err := a.store.ListAgentPrompts(ctx, true)
	if err != nil {
		return AgentPromptRuntimeConfig{}, err
	}
	parts := make([]string, 0, len(prompts))
	markers := make([]AgentPromptMarker, 0, len(prompts))
	for _, prompt := range prompts {
		content := strings.TrimSpace(prompt.Content)
		if prompt.Scope != "global" || content == "" {
			continue
		}
		parts = append(parts, content)
		markers = append(markers, AgentPromptMarker{
			ID:            prompt.ID,
			Version:       prompt.Version,
			ContentLength: len(content),
		})
	}
	return AgentPromptRuntimeConfig{
		SystemPrompt: strings.Join(parts, "\n\n"),
		Prompts:      markers,
	}, nil
}

func agentPromptAuditDetail(prompt store.AgentPrompt) string {
	return "enabled=" + boolText(prompt.Enabled) + " version=" + int64Text(prompt.Version) +
		" content_length=" + intText(len(strings.TrimSpace(prompt.Content)))
}

func intText(v int) string { return strconv.Itoa(v) }

func int64Text(v int64) string { return strconv.FormatInt(v, 10) }
