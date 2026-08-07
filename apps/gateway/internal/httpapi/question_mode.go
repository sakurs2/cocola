package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

type questionAnswerRequest struct {
	ExpectedVersion int                  `json:"expected_version"`
	Answer          convo.QuestionAnswer `json:"answer"`
	ClientRequestID string               `json:"client_request_id"`
}

type questionCancelRequest struct {
	ExpectedVersion int `json:"expected_version"`
}

func decodeQuestionAnswer(w http.ResponseWriter, r *http.Request) (questionAnswerRequest, bool) {
	var input questionAnswerRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return questionAnswerRequest{}, false
	}
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.Answer.OptionID = strings.TrimSpace(input.Answer.OptionID)
	input.Answer.Text = strings.TrimSpace(input.Answer.Text)
	if input.ExpectedVersion <= 0 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "expected_version must be positive")
		return questionAnswerRequest{}, false
	}
	if _, err := uuid.Parse(input.ClientRequestID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "client_request_id must be a UUID")
		return questionAnswerRequest{}, false
	}
	if input.Answer.OptionID == "" && input.Answer.Text == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "answer must select an option or include text")
		return questionAnswerRequest{}, false
	}
	if len(input.Answer.Text) > 16<<10 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "answer text is too large")
		return questionAnswerRequest{}, false
	}
	return input, true
}

func questionAnswerText(question chatrun.Question, answer convo.QuestionAnswer) (string, bool) {
	label := ""
	if answer.OptionID != "" {
		for _, option := range question.Options {
			if option.ID == answer.OptionID {
				label = option.Label
				break
			}
		}
		if label == "" {
			return "", false
		}
	}
	if label != "" && answer.Text != "" {
		return label + "\n\n" + answer.Text, true
	}
	if label != "" {
		return label, true
	}
	return answer.Text, answer.Text != ""
}

func (a *API) answerQuestion(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil || a.convo == nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "question answering is unavailable")
		return
	}
	if a.runs.shutting.Load() {
		writeErr(w, http.StatusServiceUnavailable, "SHUTTING_DOWN", "gateway is shutting down")
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "streaming unsupported")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	conversationID := strings.TrimSpace(r.PathValue("id"))
	questionID := strings.TrimSpace(r.PathValue("question_id"))
	if conversationID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id is required")
		return
	}
	if _, err := uuid.Parse(questionID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "question id must be a UUID")
		return
	}
	input, ok := decodeQuestionAnswer(w, r)
	if !ok {
		return
	}
	question, err := a.runs.store.GetQuestion(
		r.Context(), conversationID, questionID, identity.UserID,
	)
	if errors.Is(err, chatrun.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "QUESTION_NOT_FOUND", "question not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "question state is unavailable")
		return
	}
	_, requestErr := a.runs.store.GetRequest(
		r.Context(), conversationID, identity.UserID, input.ClientRequestID,
	)
	idempotentReplay := requestErr == nil
	if requestErr != nil && !errors.Is(requestErr, chatrun.ErrNotFound) {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "question state is unavailable")
		return
	}
	if _, available := a.runtimeByID[question.RuntimeID]; !available && !idempotentReplay {
		writeErr(
			w,
			http.StatusConflict,
			"QUESTION_RUNTIME_UNAVAILABLE",
			"The runtime used for this question is no longer available.",
		)
		return
	}
	answerText, valid := questionAnswerText(question, input.Answer)
	if !valid {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "selected option does not exist")
		return
	}
	conversation, err := a.convo.GetConversation(r.Context(), conversationID, identity.UserID)
	if errors.Is(err, convo.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "QUESTION_NOT_FOUND", "question not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "CHAT_HISTORY_UNAVAILABLE", "conversation state is unavailable")
		return
	}
	if a.writeConversationAgentError(
		w,
		a.ensureConversationAgentActive(r.Context(), identity, conversation),
	) {
		return
	}

	startedAt := chatStartedAt(r).UTC()
	runID := tracing.TraceID(r.Context())
	if runID == "" {
		runID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	run := chatrun.Run{
		ID: runID, RootSpanID: traceevents.NewSpanID(), ConversationID: conversationID,
		ConversationTitle: conversation.Title, UserID: identity.UserID, Source: "interactive",
		ModelRouteID: question.ModelRouteID, ModelAlias: question.ModelAlias,
		ReasoningEffort: question.ReasoningEffort,
		ClientRequestID: input.ClientRequestID, InteractionMode: question.InteractionMode,
		Status: chatrun.StatusRunning, StartedAt: startedAt, LastActivityAt: startedAt,
	}
	req := chatRequest{
		Prompt: answerText, SessionID: conversationID, RuntimeID: question.RuntimeID,
		InteractionMode: question.InteractionMode, ModelRouteID: question.ModelRouteID,
		ModelAlias: question.ModelAlias, ConversationTitle: conversation.Title,
		ReasoningEffort: question.ReasoningEffort,
		ProjectID:       conversation.ProjectID, SkillID: question.SkillID,
		ClientRequestID: input.ClientRequestID, RequireSessionResume: true,
		QuestionID:         question.ID,
		AgentID:            conversation.AgentID,
		AgentSnapshot:      conversation.AgentSnapshot,
		ChannelConnectorID: conversation.ChannelConnectorID,
	}
	userReq := req
	userReq.Prompt = answerText

	unlockConversation := a.runs.conversationGate.lock(conversationID)
	a.runs.mutationMu.Lock()
	result, startErr := a.runs.store.StartQuestionAnswer(r.Context(), chatrun.QuestionAnswerInput{
		Run: run, ConversationID: conversationID, UserID: identity.UserID,
		QuestionID: questionID, ExpectedVersion: input.ExpectedVersion,
		Answer: input.Answer, AnsweredAt: startedAt,
		UserMessage: convo.Message{
			ID: runID + "-user", ConversationID: conversationID, Role: "user",
			Parts:    []convo.Part{{Type: convo.PartText, Text: answerText}},
			Metadata: userMetadata(userReq), CreatedAt: startedAt,
		},
	})
	var live *liveRun
	if startErr == nil {
		run = result.Run
		if result.Created {
			live = a.newLiveRun(r, identity, req, run)
			a.runs.mu.Lock()
			a.runs.live[run.ID] = live
			a.runs.mu.Unlock()
		} else {
			live = a.runs.getLive(run.ID)
		}
	}
	a.runs.mutationMu.Unlock()
	unlockConversation()

	switch {
	case errors.Is(startErr, chatrun.ErrNotFound):
		writeErr(w, http.StatusNotFound, "QUESTION_NOT_FOUND", "question not found")
		return
	case errors.Is(startErr, chatrun.ErrQuestionNotCurrent),
		errors.Is(startErr, chatrun.ErrQuestionState):
		writeErr(w, http.StatusConflict, "QUESTION_NOT_CURRENT", "This question is no longer current.")
		return
	case errors.Is(startErr, chatrun.ErrQuestionModelUnavailable):
		writeErr(w, http.StatusConflict, "QUESTION_MODEL_UNAVAILABLE", "The model used for this question is no longer available.")
		return
	case errors.Is(startErr, chatrun.ErrAgentArchived):
		writeErr(w, http.StatusConflict, "AGENT_ARCHIVED", "Agent is archived")
		return
	case errors.Is(startErr, chatrun.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]string{
				"code": "RUN_IN_PROGRESS", "message": "conversation already has an active run",
			},
			"run_id": result.Run.ID,
		})
		return
	case startErr != nil:
		a.runs.databaseUnavailable.Store(true)
		a.log.Warn("question answer start failed: " + startErr.Error())
		writeErr(w, http.StatusServiceUnavailable, "QUESTION_ANSWER_FAILED", "Could not continue the conversation. Try again.")
		return
	}

	a.runs.databaseUnavailable.Store(false)
	w.Header().Set("x-cocola-run-id", run.ID)
	if live == nil {
		a.streamStoredRun(w, r, run)
		return
	}
	snapshot, updates, unsubscribe := live.subscribe()
	answerJSON, _ := json.Marshal(input.Answer)
	live.publish(agent.Event{Kind: "question_status", Data: map[string]string{
		"id": questionID, "status": chatrun.QuestionStatusAnswering,
		"answer": string(answerJSON),
	}})
	if result.Created {
		go a.executeLiveRun(live)
	}
	a.serveRunSubscription(w, r, run.ID, snapshot, updates, unsubscribe)
}

func (a *API) cancelQuestion(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "question state is unavailable")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	conversationID := strings.TrimSpace(r.PathValue("id"))
	questionID := strings.TrimSpace(r.PathValue("question_id"))
	if _, err := uuid.Parse(questionID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "question id must be a UUID")
		return
	}
	var input questionCancelRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ExpectedVersion <= 0 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "expected_version must be positive")
		return
	}
	question, err := a.runs.store.CancelQuestion(
		r.Context(), conversationID, questionID, identity.UserID,
		input.ExpectedVersion, time.Now().UTC(),
	)
	switch {
	case errors.Is(err, chatrun.ErrNotFound):
		writeErr(w, http.StatusNotFound, "QUESTION_NOT_FOUND", "question not found")
	case errors.Is(err, chatrun.ErrQuestionNotCurrent),
		errors.Is(err, chatrun.ErrQuestionState):
		writeErr(w, http.StatusConflict, "QUESTION_NOT_CURRENT", "This question is no longer current.")
	case err != nil:
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "question state is unavailable")
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"question": question,
			"event": map[string]any{
				"kind": "question_status",
				"data": map[string]string{
					"id": question.ID, "version": strconv.Itoa(question.Version),
					"status": question.Status,
				},
			},
		})
	}
}
