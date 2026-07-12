package chatrun

import (
	"context"
	"sync"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

type Memory struct {
	mu    sync.Mutex
	runs  map[string]Run
	convo convo.Store
}

func NewMemory(conversations convo.Store) *Memory {
	return &Memory{runs: make(map[string]Run), convo: conversations}
}

func (m *Memory) Start(ctx context.Context, in StartInput) (StartResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.runs {
		if run.UserID == in.Run.UserID && run.ConversationID == in.Run.ConversationID &&
			run.ClientRequestID != "" && run.ClientRequestID == in.Run.ClientRequestID {
			return StartResult{Run: run}, nil
		}
	}
	if err := m.convo.UpsertConversation(ctx, in.Conversation); err != nil {
		if err == convo.ErrNotFound {
			return StartResult{}, ErrNotFound
		}
		return StartResult{}, err
	}
	for _, run := range m.runs {
		if run.ConversationID == in.Run.ConversationID && run.Status == StatusRunning {
			return StartResult{Run: run}, ErrConflict
		}
	}
	if err := m.convo.InsertMessage(ctx, in.UserMessage); err != nil {
		return StartResult{}, err
	}
	m.runs[in.Run.ID] = in.Run
	return StartResult{Run: in.Run, Created: true}, nil
}

func (m *Memory) GetOwned(_ context.Context, runID, userID string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok || run.UserID != userID {
		return Run{}, ErrNotFound
	}
	return run, nil
}

func (m *Memory) Active(_ context.Context, conversationID, userID string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.runs {
		if run.ConversationID == conversationID && run.UserID == userID && run.Status == StatusRunning {
			return run, nil
		}
	}
	return Run{}, ErrNotFound
}

func (m *Memory) SaveDraft(ctx context.Context, runID, userID string, message convo.Message) error {
	m.mu.Lock()
	run, ok := m.runs[runID]
	if !ok || run.UserID != userID || run.Status != StatusRunning {
		m.mu.Unlock()
		return ErrNotFound
	}
	run.LastActivityAt = time.Now().UTC()
	m.runs[runID] = run
	m.mu.Unlock()
	return m.convo.UpsertMessage(ctx, message)
}

func (m *Memory) Finalize(ctx context.Context, in FinalizeInput) (Run, error) {
	m.mu.Lock()
	run, ok := m.runs[in.RunID]
	if !ok || run.UserID != in.UserID {
		m.mu.Unlock()
		return Run{}, ErrNotFound
	}
	if IsTerminal(run.Status) {
		m.mu.Unlock()
		return run, nil
	}
	now := time.Now().UTC()
	run.Status = in.Status
	run.ErrorCode = in.ErrorCode
	run.CompletedAt = &now
	run.LastActivityAt = now
	m.runs[in.RunID] = run
	m.mu.Unlock()
	if in.AssistantMessage != nil {
		if err := m.convo.UpsertMessage(ctx, *in.AssistantMessage); err != nil {
			return Run{}, err
		}
	}
	if in.Reveal {
		_ = m.convo.RevealConversation(ctx, run.ConversationID, run.UserID, in.ConversationTitle, now)
	}
	return run, nil
}

func (m *Memory) InterruptRunning(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for id, run := range m.runs {
		if run.Status != StatusRunning {
			continue
		}
		run.Status = StatusInterrupted
		run.ErrorCode = "GATEWAY_RESTARTED"
		run.CompletedAt = &now
		run.LastActivityAt = now
		m.runs[id] = run
		count++
	}
	return count, nil
}

func (m *Memory) Close() {}
