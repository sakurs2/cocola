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
	questions         map[string]Question
	questionVersions  map[string]int
	unavailableModels map[string]bool
	convo             convo.Store
}

func NewMemory(conversations convo.Store) *Memory {
	return &Memory{
		runs: make(map[string]Run), plans: make(map[string]Plan),
		versions: make(map[string]int), questions: make(map[string]Question),
		questionVersions: make(map[string]int), unavailableModels: make(map[string]bool),
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
	for _, question := range m.questions {
		if question.ConversationID == in.Run.ConversationID &&
			(question.Status == QuestionStatusPending || question.Status == QuestionStatusAnswering) {
			return StartResult{}, ErrQuestionPending
		}
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
	run.ToolCallCount = in.ToolCallCount
	run.LLMCallCount = in.LLMCallCount
	run.CompletedAt = &now
	if !run.StartedAt.IsZero() && !now.Before(run.StartedAt) {
		run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
	}
	run.LastActivityAt = now
	var plan *Plan
	var question *Question
	var answeredQuestion *Question
	var restoredQuestion *Question
	var supersededPlanID string
	for id, existing := range m.questions {
		if existing.AnswerRunID == run.ID && existing.Status == QuestionStatusAnswering {
			if in.RuntimeAccepted {
				existing.Status = QuestionStatusAnswered
				answeredQuestion = &existing
			} else {
				existing.Status = QuestionStatusPending
				existing.AnswerRunID = ""
				existing.Answer = nil
				existing.AnsweredBy = ""
				existing.AnsweredAt = nil
				restoredQuestion = &existing
			}
			existing.UpdatedAt = now
			m.questions[id] = existing
			_ = m.updateQuestionMessageStatus(ctx, existing)
		}
	}
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
	if in.QuestionCandidate != nil && in.Status == StatusWaitingInput {
		candidate := in.QuestionCandidate
		if !validQuestionCandidate(candidate) {
			m.mu.Unlock()
			return FinalizeResult{}, ErrQuestionState
		}
		for _, existing := range m.questions {
			if existing.ConversationID == run.ConversationID &&
				(existing.Status == QuestionStatusPending ||
					existing.Status == QuestionStatusAnswering) {
				m.mu.Unlock()
				return FinalizeResult{}, ErrQuestionPending
			}
		}
		version := m.questionVersions[run.ConversationID] + 1
		m.questionVersions[run.ConversationID] = version
		value := Question{
			ID: candidate.ID, ConversationID: run.ConversationID, Version: version,
			Status: QuestionStatusPending, SourceRunID: run.ID,
			InteractionMode: normalizeInteractionMode(candidate.InteractionMode),
			RuntimeID:       candidate.RuntimeID, ModelRouteID: candidate.ModelRouteID,
			ModelAlias: candidate.ModelAlias, SkillID: candidate.SkillID,
			Text: candidate.Text, Options: append([]convo.QuestionOption(nil), candidate.Options...),
			CreatedAt: now, UpdatedAt: now,
		}
		m.questions[value.ID] = value
		if in.AssistantMessage == nil {
			in.AssistantMessage = &convo.Message{
				ID: run.ID + "-assistant", ConversationID: run.ConversationID,
				Role: "assistant", CreatedAt: now,
			}
		}
		in.AssistantMessage.Parts = append(in.AssistantMessage.Parts, questionPart(value))
		question = &value
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
	return FinalizeResult{
		Run: run, Plan: plan, Question: question, AnsweredQuestion: answeredQuestion,
		RestoredQuestion: restoredQuestion, SupersededPlanID: supersededPlanID,
	}, nil
}

func (m *Memory) GetQuestion(
	ctx context.Context,
	conversationID, questionID, userID string,
) (Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.convo.GetConversation(ctx, conversationID, userID); err != nil {
		return Question{}, ErrNotFound
	}
	question, ok := m.questions[questionID]
	if !ok || question.ConversationID != conversationID {
		return Question{}, ErrNotFound
	}
	return question, nil
}

func (m *Memory) ListQuestions(
	ctx context.Context,
	conversationID, userID string,
) ([]Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.convo.GetConversation(ctx, conversationID, userID); err != nil {
		return nil, ErrNotFound
	}
	out := make([]Question, 0)
	for _, question := range m.questions {
		if question.ConversationID == conversationID {
			out = append(out, question)
		}
	}
	return out, nil
}

func (m *Memory) ListRuns(
	ctx context.Context,
	conversationID, userID string,
) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.convo.GetConversation(ctx, conversationID, userID); err != nil {
		return nil, ErrNotFound
	}
	out := make([]Run, 0)
	for _, run := range m.runs {
		if run.ConversationID == conversationID && run.UserID == userID {
			out = append(out, run)
		}
	}
	return out, nil
}

func (m *Memory) StartQuestionAnswer(
	ctx context.Context,
	in QuestionAnswerInput,
) (QuestionAnswerResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, err := m.convo.GetConversation(ctx, in.ConversationID, in.UserID)
	if err != nil {
		return QuestionAnswerResult{}, ErrNotFound
	}
	for _, existing := range m.runs {
		if existing.ConversationID == in.ConversationID && existing.UserID == in.UserID &&
			existing.ClientRequestID != "" &&
			existing.ClientRequestID == in.Run.ClientRequestID {
			question, exists := m.questions[in.QuestionID]
			if !exists || question.AnswerRunID != existing.ID {
				return QuestionAnswerResult{}, ErrQuestionNotCurrent
			}
			return QuestionAnswerResult{
				Run: existing, Conversation: conversation, Question: question,
			}, nil
		}
	}
	question, ok := m.questions[in.QuestionID]
	if !ok || question.ConversationID != in.ConversationID {
		return QuestionAnswerResult{}, ErrNotFound
	}
	if question.Version != in.ExpectedVersion {
		return QuestionAnswerResult{}, ErrQuestionNotCurrent
	}
	if question.Status != QuestionStatusPending {
		return QuestionAnswerResult{}, ErrQuestionState
	}
	if question.ModelRouteID == "" || m.unavailableModels[question.ModelRouteID] {
		return QuestionAnswerResult{}, ErrQuestionModelUnavailable
	}
	for _, existing := range m.runs {
		if existing.ConversationID == in.ConversationID && existing.Status == StatusRunning {
			return QuestionAnswerResult{Run: existing, Conversation: conversation}, ErrConflict
		}
	}
	run := in.Run
	run.ConversationID = in.ConversationID
	run.UserID = in.UserID
	run.InteractionMode = normalizeInteractionMode(question.InteractionMode)
	m.runs[run.ID] = run
	if err := m.convo.InsertMessage(ctx, in.UserMessage); err != nil {
		delete(m.runs, run.ID)
		return QuestionAnswerResult{}, err
	}
	now := in.AnsweredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	question.Status = QuestionStatusAnswering
	question.AnswerRunID = run.ID
	question.Answer = &in.Answer
	question.AnsweredBy = in.UserID
	question.AnsweredAt = &now
	question.UpdatedAt = now
	m.questions[question.ID] = question
	if err := m.updateQuestionMessageStatus(ctx, question); err != nil {
		return QuestionAnswerResult{}, err
	}
	return QuestionAnswerResult{
		Run: run, Conversation: conversation, Question: question, Created: true,
	}, nil
}

func (m *Memory) AcceptQuestionAnswer(
	ctx context.Context,
	runID, questionID, userID string,
	now time.Time,
) (Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok || run.UserID != userID || run.Status != StatusRunning {
		return Question{}, ErrNotFound
	}
	question, ok := m.questions[questionID]
	if !ok || question.ConversationID != run.ConversationID ||
		question.AnswerRunID != runID {
		return Question{}, ErrNotFound
	}
	if question.Status == QuestionStatusAnswered {
		return question, nil
	}
	if question.Status != QuestionStatusAnswering {
		return Question{}, ErrQuestionState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	question.Status = QuestionStatusAnswered
	question.UpdatedAt = now
	previous := m.questions[question.ID]
	m.questions[question.ID] = question
	if err := m.updateQuestionMessageStatus(ctx, question); err != nil {
		m.questions[question.ID] = previous
		return Question{}, err
	}
	return question, nil
}

func (m *Memory) CancelQuestion(
	ctx context.Context,
	conversationID, questionID, userID string,
	expectedVersion int,
	now time.Time,
) (Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.convo.GetConversation(ctx, conversationID, userID); err != nil {
		return Question{}, ErrNotFound
	}
	question, ok := m.questions[questionID]
	if !ok || question.ConversationID != conversationID {
		return Question{}, ErrNotFound
	}
	if question.Version != expectedVersion {
		return Question{}, ErrQuestionNotCurrent
	}
	if question.Status != QuestionStatusPending {
		return Question{}, ErrQuestionState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	question.Status = QuestionStatusCancelled
	question.UpdatedAt = now
	m.questions[question.ID] = question
	if err := m.updateQuestionMessageStatus(ctx, question); err != nil {
		return Question{}, err
	}
	return question, nil
}

func (m *Memory) updateQuestionMessageStatus(ctx context.Context, question Question) error {
	sourceRun := m.runs[question.SourceRunID]
	messages, err := m.convo.GetMessages(ctx, question.ConversationID, sourceRun.UserID)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.ID != question.SourceRunID+"-assistant" {
			continue
		}
		for i := range message.Parts {
			if message.Parts[i].Type == convo.PartQuestion &&
				message.Parts[i].QuestionID == question.ID {
				message.Parts[i].Status = question.Status
				message.Parts[i].QuestionAnswer = question.Answer
			}
		}
		return m.convo.UpsertMessage(ctx, message)
	}
	return nil
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

func (m *Memory) ListPlans(
	ctx context.Context,
	conversationID, userID string,
) ([]Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.convo.GetConversation(ctx, conversationID, userID); err != nil {
		return nil, ErrNotFound
	}
	plans := make([]Plan, 0)
	for _, plan := range m.plans {
		if plan.ConversationID == conversationID {
			plans = append(plans, plan)
		}
	}
	return plans, nil
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
	for _, question := range m.questions {
		if question.ConversationID == in.ConversationID &&
			(question.Status == QuestionStatusPending ||
				question.Status == QuestionStatusAnswering) {
			return PlanExecutionResult{}, ErrQuestionPending
		}
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
		for questionID, question := range m.questions {
			if question.AnswerRunID != run.ID || question.Status != QuestionStatusAnswering {
				continue
			}
			question.Status = QuestionStatusAnswered
			question.UpdatedAt = now
			m.questions[questionID] = question
			_ = m.updateQuestionMessageStatus(context.Background(), question)
		}
		count++
	}
	return count, nil
}

func (m *Memory) Close() {}
