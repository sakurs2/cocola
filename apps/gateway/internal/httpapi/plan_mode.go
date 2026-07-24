package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

type planActionRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	ClientRequestID string `json:"client_request_id,omitempty"`
}

func decodePlanAction(w http.ResponseWriter, r *http.Request, requireRequestID bool) (planActionRequest, bool) {
	var input planActionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return planActionRequest{}, false
	}
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.ExpectedVersion <= 0 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "expected_version must be positive")
		return planActionRequest{}, false
	}
	if requireRequestID {
		if _, err := uuid.Parse(input.ClientRequestID); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "client_request_id must be a UUID")
			return planActionRequest{}, false
		}
	}
	return input, true
}

func approvedPlanPrompt(plan chatrun.Plan) string {
	return fmt.Sprintf(
		"The user approved Cocola Plan %s version %d. Execute the approved plan now in the same "+
			"Claude session. Do not create or revise a plan unless execution is blocked.\n\n"+
			"<approved_cocola_plan>\n%s\n</approved_cocola_plan>",
		plan.ID,
		plan.Version,
		plan.ContentMarkdown,
	)
}

func projectAgentContext(value project.ProjectContext) agent.ProjectContext {
	return agent.ProjectContext{
		ProjectID: value.ProjectID, RepositoryID: value.RepositoryExternalID,
		CloneURL: value.CloneURL, DefaultBranch: value.DefaultBranch,
		BaseRef: value.BaseRef, BaseSHA: value.BaseSHA, TaskBranch: value.BranchName,
		GitAuthorName: value.GitAuthorName, GitAuthorEmail: value.GitAuthorEmail,
		RepositoryProvider: value.RepositoryProvider,
		RepositoryFullName: value.RepositoryFullName,
		CredentialMode:     value.CredentialMode,
	}
}

func (a *API) validatePlanWorkspace(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	plan chatrun.Plan,
	projectID string,
) bool {
	if projectID == "" {
		return true
	}
	if a.projects == nil || a.gitInspector == nil || strings.TrimSpace(plan.WorkspaceRevision) == "" {
		return false
	}
	value, scmToken, err := a.projects.ProjectContext(ctx, project.Identity{
		TenantID: identity.TenantID, UserID: identity.UserID, Email: identity.Email,
		Name: identity.Name, Username: identity.Username,
	}, conversationID)
	if err != nil {
		return false
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	result, err := a.gitInspector.InspectWorkspaceGit(inspectCtx, agent.InspectRequest{
		UserID: identity.UserID, SessionID: conversationID, Operation: "status",
		SCMToken: scmToken, Project: projectAgentContext(value),
	})
	return err == nil &&
		strings.TrimSpace(result.Snapshot.WorkspaceRevision) != "" &&
		result.Snapshot.WorkspaceRevision == plan.WorkspaceRevision
}

func (a *API) streamExistingPlanExecution(
	w http.ResponseWriter,
	r *http.Request,
	run chatrun.Run,
	planID string,
) {
	if run.PlanID != planID {
		writeErr(
			w,
			http.StatusConflict,
			"IDEMPOTENCY_CONFLICT",
			"client_request_id was already used for a different plan execution",
		)
		return
	}
	w.Header().Set("x-cocola-run-id", run.ID)
	live := a.runs.getLive(run.ID)
	if live == nil {
		a.streamStoredRun(w, r, run)
		return
	}
	snapshot, updates, unsubscribe := live.subscribe()
	a.serveRunSubscription(w, r, run.ID, snapshot, updates, unsubscribe)
}

func (a *API) executePlan(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil || a.convo == nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "plan execution is unavailable")
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
	planID := strings.TrimSpace(r.PathValue("plan_id"))
	if conversationID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id is required")
		return
	}
	if _, err := uuid.Parse(planID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "plan id must be a UUID")
		return
	}
	input, ok := decodePlanAction(w, r, true)
	if !ok {
		return
	}
	existing, requestErr := a.runs.store.GetRequest(
		r.Context(), conversationID, identity.UserID, input.ClientRequestID,
	)
	if requestErr == nil {
		a.streamExistingPlanExecution(w, r, existing, planID)
		return
	}
	if !errors.Is(requestErr, chatrun.ErrNotFound) {
		a.runs.databaseUnavailable.Store(true)
		a.log.Warn("plan execution idempotency lookup failed: " + requestErr.Error())
		writeErr(
			w,
			http.StatusServiceUnavailable,
			"RUN_STORE_UNAVAILABLE",
			"plan execution state is unavailable",
		)
		return
	}
	plan, err := a.runs.store.GetPlan(r.Context(), conversationID, planID, identity.UserID)
	if errors.Is(err, chatrun.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "PLAN_NOT_FOUND", "plan not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "plan state is unavailable")
		return
	}
	if plan.RuntimeID != "claude-code" {
		writeErr(w, http.StatusConflict, "PLAN_MODE_UNSUPPORTED", "Plan mode is supported only for Claude Code conversations.")
		return
	}
	conversation, err := a.convo.GetConversation(r.Context(), conversationID, identity.UserID)
	if errors.Is(err, convo.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "PLAN_NOT_FOUND", "plan not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "CHAT_HISTORY_UNAVAILABLE", "conversation state is unavailable")
		return
	}

	unlockConversation := a.runs.conversationGate.lock(conversationID)
	conversationLocked := true
	defer func() {
		if conversationLocked {
			unlockConversation()
		}
	}()

	existing, requestErr = a.runs.store.GetRequest(
		r.Context(), conversationID, identity.UserID, input.ClientRequestID,
	)
	if requestErr == nil {
		unlockConversation()
		conversationLocked = false
		a.streamExistingPlanExecution(w, r, existing, planID)
		return
	}
	if !errors.Is(requestErr, chatrun.ErrNotFound) {
		a.runs.databaseUnavailable.Store(true)
		a.log.Warn("plan execution idempotency recheck failed: " + requestErr.Error())
		writeErr(
			w,
			http.StatusServiceUnavailable,
			"RUN_STORE_UNAVAILABLE",
			"plan execution state is unavailable",
		)
		return
	}

	active, activeErr := a.runs.store.Active(r.Context(), conversationID, identity.UserID)
	if activeErr == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]string{
				"code": "RUN_IN_PROGRESS", "message": "conversation already has an active run",
			},
			"run_id": active.ID,
		})
		return
	}
	if !errors.Is(activeErr, chatrun.ErrNotFound) {
		a.runs.databaseUnavailable.Store(true)
		a.log.Warn("plan execution active run check failed: " + activeErr.Error())
		writeErr(
			w,
			http.StatusServiceUnavailable,
			"RUN_STORE_UNAVAILABLE",
			"plan execution state is unavailable",
		)
		return
	}
	if !a.validatePlanWorkspace(r.Context(), identity, conversationID, plan, conversation.ProjectID) {
		writeErr(
			w,
			http.StatusConflict,
			"PLAN_WORKSPACE_CHANGED",
			"The workspace changed after this plan was created. Create a new plan before executing.",
		)
		return
	}

	startedAt := chatStartedAt(r).UTC()
	runID := tracing.TraceID(r.Context())
	if runID == "" {
		runID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	rootSpanID := traceevents.NewSpanID()
	run := chatrun.Run{
		ID: runID, RootSpanID: rootSpanID, ConversationID: conversationID,
		ConversationTitle: conversation.Title, UserID: identity.UserID, Source: "interactive",
		ModelRouteID: plan.ModelRouteID, ModelAlias: plan.ModelAlias,
		ClientRequestID: input.ClientRequestID, InteractionMode: chatrun.InteractionModeExecute,
		PlanID: plan.ID, Status: chatrun.StatusRunning,
		StartedAt: startedAt, LastActivityAt: startedAt,
	}
	req := chatRequest{
		Prompt: approvedPlanPrompt(plan), SessionID: conversationID,
		RuntimeID: plan.RuntimeID, InteractionMode: chatrun.InteractionModeExecute,
		ModelRouteID: plan.ModelRouteID, ModelAlias: plan.ModelAlias,
		ConversationTitle: conversation.Title, ProjectID: conversation.ProjectID,
		ClientRequestID: input.ClientRequestID, RequireSessionResume: true,
	}

	a.runs.mutationMu.Lock()
	result, startErr := a.runs.store.StartPlanExecution(r.Context(), chatrun.PlanExecutionInput{
		Run: run, ConversationID: conversationID, UserID: identity.UserID,
		ExpectedVersion: input.ExpectedVersion, PlanID: planID, ApprovedAt: startedAt,
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
	conversationLocked = false

	switch {
	case errors.Is(startErr, chatrun.ErrNotFound):
		writeErr(w, http.StatusNotFound, "PLAN_NOT_FOUND", "plan not found")
		return
	case errors.Is(startErr, chatrun.ErrPlanNotCurrent):
		writeErr(w, http.StatusConflict, "PLAN_NOT_CURRENT", "This plan is no longer current. Review the latest plan before executing.")
		return
	case errors.Is(startErr, chatrun.ErrPlanState):
		writeErr(w, http.StatusConflict, "PLAN_NOT_EXECUTABLE", "This plan cannot be executed in its current state.")
		return
	case errors.Is(startErr, chatrun.ErrPlanModelUnavailable):
		writeErr(
			w,
			http.StatusConflict,
			"PLAN_MODEL_UNAVAILABLE",
			"The model used for this plan is no longer available. Create a new plan.",
		)
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
		a.log.Warn("plan execution start failed: " + startErr.Error())
		writeErr(w, http.StatusServiceUnavailable, "PLAN_EXECUTION_FAILED", "Could not start plan execution. Try again.")
		return
	}

	a.runs.databaseUnavailable.Store(false)
	w.Header().Set("x-cocola-run-id", run.ID)
	if live == nil {
		a.streamStoredRun(w, r, run)
		return
	}
	snapshot, updates, unsubscribe := live.subscribe()
	live.publish(agent.Event{Kind: "plan_status", Data: map[string]string{
		"id": plan.ID, "version": strconv.Itoa(plan.Version), "status": chatrun.PlanStatusExecuting,
	}})
	if result.Created {
		go a.executeLiveRun(live)
	}
	a.serveRunSubscription(w, r, run.ID, snapshot, updates, unsubscribe)
}

func (a *API) cancelPlan(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "plan state is unavailable")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	conversationID := strings.TrimSpace(r.PathValue("id"))
	planID := strings.TrimSpace(r.PathValue("plan_id"))
	if _, err := uuid.Parse(planID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "plan id must be a UUID")
		return
	}
	input, ok := decodePlanAction(w, r, false)
	if !ok {
		return
	}
	plan, err := a.runs.store.CancelPlan(
		r.Context(), conversationID, planID, identity.UserID, input.ExpectedVersion, time.Now().UTC(),
	)
	switch {
	case errors.Is(err, chatrun.ErrNotFound):
		writeErr(w, http.StatusNotFound, "PLAN_NOT_FOUND", "plan not found")
	case errors.Is(err, chatrun.ErrPlanNotCurrent):
		writeErr(w, http.StatusConflict, "PLAN_NOT_CURRENT", "This plan is no longer current. Review the latest plan before executing.")
	case errors.Is(err, chatrun.ErrPlanState):
		writeErr(w, http.StatusConflict, "PLAN_NOT_CANCELLABLE", "This plan cannot be cancelled in its current state.")
	case err != nil:
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "plan state is unavailable")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
	}
}
