package chatrun

import (
	"context"
	"sync"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

type Memory struct {
	mu                sync.Mutex
	runs              map[string]Run
	plans             map[string]Plan
	versions          map[string]int
	unavailableModels map[string]bool
	convo             convo.Store
}

func NewMemory(conversations convo.Store) *Memory {
	return &Memory{
		runs: make(map[string]Run), plans: make(map[string]Plan),
		versions: make(map[string]int), unavailableModels: make(map[string]bool),
		convo: conversations,
	}
}

func (m *Memory) Start(ctx context.Context, in StartInput) (StartResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in.Run.InteractionMode = normalizeInteractionMode(in.Run.InteractionMode)
	effective := in.Conversation
	existing, err := m.convo.GetConversation(ctx, in.Conversation.ID, in.Conversation.UserID)
	if err == nil {
		if effective.RuntimeID != "" && effective.RuntimeID != existing.RuntimeID {
			return StartResult{}, ErrRuntimeMismatch
		}
		effective = existing
		if in.Conversation.ProjectID != "" && in.Conversation.ProjectID != existing.ProjectID {
			return StartResult{}, ErrProjectMismatch
		}
		effective.UpdatedAt = in.Conversation.UpdatedAt
	} else if err == convo.ErrNotFound {
		if effective.RuntimeID == "" {
			effective.RuntimeID = convo.DefaultRuntimeID
		}
		if effective.FolderID != "" {
			if _, folderErr := m.convo.GetFolder(ctx, effective.FolderID, effective.UserID); folderErr != nil {
				return StartResult{}, ErrFolderNotFound
			}
		}
		if effective.FolderID != "" && effective.ProjectID != "" {
			return StartResult{}, ErrProjectMismatch
		}
	} else {
		return StartResult{}, err
	}
	for _, run := range m.runs {
		if run.UserID == in.Run.UserID && run.ConversationID == in.Run.ConversationID &&
			run.ClientRequestID != "" && run.ClientRequestID == in.Run.ClientRequestID {
			return StartResult{Run: run, Conversation: effective}, nil
		}
	}
	if err == nil && in.Conversation.FolderID != "" && in.Conversation.FolderID != effective.FolderID {
		return StartResult{}, ErrFolderMismatch
	}
	if err := m.convo.UpsertConversation(ctx, effective); err != nil {
		if err == convo.ErrNotFound {
			return StartResult{}, ErrNotFound
		}
		if err == convo.ErrRuntimeMismatch {
			return StartResult{}, ErrRuntimeMismatch
		}
		return StartResult{}, err
	}
	for _, run := range m.runs {
		if run.ConversationID == in.Run.ConversationID && run.Status == StatusRunning {
			return StartResult{Run: run, Conversation: effective}, ErrConflict
		}
	}
	if err := m.convo.InsertMessage(ctx, in.UserMessage); err != nil {
		return StartResult{}, err
	}
	m.runs[in.Run.ID] = in.Run
	return StartResult{Run: in.Run, Conversation: effective, Created: true}, nil
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

func (m *Memory) GetRequest(
	_ context.Context,
	conversationID, userID, clientRequestID string,
) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.runs {
		if run.ConversationID == conversationID && run.UserID == userID &&
			run.ClientRequestID != "" && run.ClientRequestID == clientRequestID {
			return run, nil
		}
	}
	return Run{}, ErrNotFound
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

func (m *Memory) Finalize(ctx context.Context, in FinalizeInput) (FinalizeResult, error) {
	m.mu.Lock()
	run, ok := m.runs[in.RunID]
	if !ok || run.UserID != in.UserID {
		m.mu.Unlock()
		return FinalizeResult{}, ErrNotFound
	}
	if IsTerminal(run.Status) {
		m.mu.Unlock()
		return FinalizeResult{Run: run}, nil
	}
	now := in.CompletedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	run.Status = in.Status
	run.ErrorCode = in.ErrorCode
	run.CompletedAt = &now
	run.LastActivityAt = now
	var plan *Plan
	var supersededPlanID string
	if in.PlanCandidate != nil && run.InteractionMode == InteractionModePlan &&
		in.Status == StatusSuccess {
		candidate := in.PlanCandidate
		if candidate.ID == "" || candidate.ContentMarkdown == "" ||
			len(candidate.ContentMarkdown) > 128<<10 {
			m.mu.Unlock()
			return FinalizeResult{}, ErrPlanState
		}
		for id, existing := range m.plans {
			if existing.ConversationID == run.ConversationID &&
				(existing.Status == PlanStatusReady || existing.Status == PlanStatusStopped) {
				existing.Status = PlanStatusSuperseded
				existing.UpdatedAt = now
				m.plans[id] = existing
				supersededPlanID = id
				_ = m.updatePlanMessageStatus(ctx, existing)
			}
		}
		version := m.versions[run.ConversationID] + 1
		m.versions[run.ConversationID] = version
		value := Plan{
			ID: candidate.ID, ConversationID: run.ConversationID, Version: version,
			Status: PlanStatusReady, SourceRunID: run.ID, RuntimeID: candidate.RuntimeID,
			ModelRouteID: candidate.ModelRouteID, ModelAlias: candidate.ModelAlias,
			ContentMarkdown:   candidate.ContentMarkdown,
			WorkspaceRevision: candidate.WorkspaceRevision, CreatedAt: now, UpdatedAt: now,
		}
		m.plans[value.ID] = value
		run.PlanID = value.ID
		if in.AssistantMessage == nil {
			in.AssistantMessage = &convo.Message{
				ID: run.ID + "-assistant", ConversationID: run.ConversationID,
				Role: "assistant", CreatedAt: now,
			}
		}
		in.AssistantMessage.Parts = append(in.AssistantMessage.Parts, planPart(value))
		plan = &value
	}
	if run.PlanID != "" && run.InteractionMode == InteractionModeExecute {
		value, exists := m.plans[run.PlanID]
		if !exists {
			m.mu.Unlock()
			return FinalizeResult{}, ErrNotFound
		}
		switch in.Status {
		case StatusSuccess:
			value.Status = PlanStatusCompleted
		case StatusCancelled, StatusInterrupted:
			value.Status = PlanStatusStopped
		default:
			value.Status = PlanStatusFailed
		}
		value.UpdatedAt = now
		m.plans[value.ID] = value
		_ = m.updatePlanMessageStatus(ctx, value)
		plan = &value
	}
	m.runs[in.RunID] = run
	m.mu.Unlock()
	if in.AssistantMessage != nil {
		if err := m.convo.UpsertMessage(ctx, *in.AssistantMessage); err != nil {
			return FinalizeResult{}, err
		}
	}
	if in.Reveal {
		_ = m.convo.RevealConversation(ctx, run.ConversationID, run.UserID, in.ConversationTitle, now)
	}
	return FinalizeResult{Run: run, Plan: plan, SupersededPlanID: supersededPlanID}, nil
}

func (m *Memory) GetPlan(
	_ context.Context,
	conversationID, planID, userID string,
) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	plan, ok := m.plans[planID]
	if !ok || plan.ConversationID != conversationID {
		return Plan{}, ErrNotFound
	}
	conversation, err := m.convo.GetConversation(context.Background(), conversationID, userID)
	if err != nil || conversation.UserID != userID {
		return Plan{}, ErrNotFound
	}
	return plan, nil
}

func (m *Memory) StartPlanExecution(
	ctx context.Context,
	in PlanExecutionInput,
) (PlanExecutionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, err := m.convo.GetConversation(ctx, in.ConversationID, in.UserID)
	if err != nil {
		return PlanExecutionResult{}, ErrNotFound
	}
	for _, run := range m.runs {
		if run.UserID == in.UserID && run.ConversationID == in.ConversationID &&
			run.ClientRequestID != "" && run.ClientRequestID == in.Run.ClientRequestID {
			plan := m.plans[in.PlanID]
			return PlanExecutionResult{
				Run: run, Conversation: conversation, Plan: plan,
			}, nil
		}
	}
	plan, ok := m.plans[in.PlanID]
	if !ok || plan.ConversationID != in.ConversationID {
		return PlanExecutionResult{}, ErrNotFound
	}
	if plan.Version != in.ExpectedVersion || m.versions[in.ConversationID] != plan.Version {
		return PlanExecutionResult{}, ErrPlanNotCurrent
	}
	if plan.Status != PlanStatusReady && plan.Status != PlanStatusStopped {
		return PlanExecutionResult{}, ErrPlanState
	}
	if plan.ModelRouteID == "" || m.unavailableModels[plan.ModelRouteID] {
		return PlanExecutionResult{}, ErrPlanModelUnavailable
	}
	for _, run := range m.runs {
		if run.ConversationID == in.ConversationID && run.Status == StatusRunning {
			return PlanExecutionResult{}, ErrConflict
		}
	}
	now := in.ApprovedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if plan.Status == PlanStatusReady {
		plan.ApprovedBy = in.UserID
		plan.ApprovedAt = &now
	}
	plan.Status = PlanStatusExecuting
	plan.UpdatedAt = now
	m.plans[plan.ID] = plan
	run := in.Run
	run.ConversationID = in.ConversationID
	run.UserID = in.UserID
	run.InteractionMode = InteractionModeExecute
	run.PlanID = plan.ID
	m.runs[run.ID] = run
	_ = m.updatePlanMessageStatus(ctx, plan)
	return PlanExecutionResult{
		Run: run, Conversation: conversation, Plan: plan, Created: true,
	}, nil
}

func (m *Memory) CancelPlan(
	ctx context.Context,
	conversationID, planID, userID string,
	expectedVersion int,
	now time.Time,
) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.convo.GetConversation(ctx, conversationID, userID); err != nil {
		return Plan{}, ErrNotFound
	}
	plan, ok := m.plans[planID]
	if !ok || plan.ConversationID != conversationID {
		return Plan{}, ErrNotFound
	}
	if plan.Version != expectedVersion {
		return Plan{}, ErrPlanNotCurrent
	}
	if plan.Status != PlanStatusReady && plan.Status != PlanStatusStopped {
		return Plan{}, ErrPlanState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	plan.Status = PlanStatusCancelled
	plan.UpdatedAt = now
	m.plans[plan.ID] = plan
	if err := m.updatePlanMessageStatus(ctx, plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (m *Memory) updatePlanMessageStatus(ctx context.Context, plan Plan) error {
	sourceRun, ok := m.runs[plan.SourceRunID]
	if !ok {
		return nil
	}
	messages, err := m.convo.GetMessages(ctx, plan.ConversationID, sourceRun.UserID)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.ID != plan.SourceRunID+"-assistant" {
			continue
		}
		for index := range message.Parts {
			if message.Parts[index].Type == convo.PartPlan &&
				message.Parts[index].PlanID == plan.ID {
				message.Parts[index].Status = plan.Status
			}
		}
		return m.convo.UpsertMessage(ctx, message)
	}
	return nil
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
		if run.PlanID != "" && run.InteractionMode == InteractionModeExecute {
			plan := m.plans[run.PlanID]
			plan.Status = PlanStatusStopped
			plan.UpdatedAt = now
			m.plans[plan.ID] = plan
			_ = m.updatePlanMessageStatus(context.Background(), plan)
		}
		count++
	}
	return count, nil
}

func (m *Memory) Close() {}
