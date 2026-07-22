package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

const (
	defaultAgentMaxTurns = int32(200)
	defaultToolTimeout   = 10 * time.Minute
	defaultSSEPing       = 15 * time.Second
	defaultMergeWindow   = 100 * time.Millisecond
	defaultDraftInterval = time.Second
	defaultFinalizeRetry = time.Second
	draftFailureBudget   = 30 * time.Second
	finalizeAttemptLimit = 3 * time.Second
	finalizeMaxAttempts  = 4
	subscriberBuffer     = 64
)

type RunConfig struct {
	AgentMaxTurns int32
	ToolTimeout   time.Duration
	PingEvery     time.Duration
	MergeWindow   time.Duration
	DraftInterval time.Duration
	FinalizeRetry time.Duration
}

type runController struct {
	store               chatrun.Store
	agentMaxTurns       int32
	toolTimeout         time.Duration
	pingEvery           time.Duration
	mergeWindow         time.Duration
	draftInterval       time.Duration
	finalizeRetry       time.Duration
	mutationMu          sync.Mutex
	mu                  sync.Mutex
	live                map[string]*liveRun
	shutting            atomic.Bool
	databaseUnavailable atomic.Bool
	stop                chan struct{}
	stopOnce            sync.Once
}

type liveRun struct {
	run                chatrun.Run
	identity           auth.Identity
	request            chatRequest
	query              agent.Query
	policy             executionPolicy
	traceCtx           context.Context
	traceRun           traceevents.Run
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan struct{}
	mu                 sync.Mutex
	reducer            *convo.Reducer
	subs               map[chan agent.Event]struct{}
	cancelled          bool
	interrupt          bool
	status             string
	recalledMemoryURIs []string
	version            uint64
}

func newRunController(store chatrun.Store, cfg RunConfig) *runController {
	if cfg.AgentMaxTurns <= 0 {
		cfg.AgentMaxTurns = defaultAgentMaxTurns
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = defaultToolTimeout
	}
	if cfg.PingEvery <= 0 {
		cfg.PingEvery = defaultSSEPing
	}
	if cfg.MergeWindow <= 0 {
		cfg.MergeWindow = defaultMergeWindow
	}
	if cfg.DraftInterval <= 0 {
		cfg.DraftInterval = defaultDraftInterval
	}
	if cfg.FinalizeRetry <= 0 {
		cfg.FinalizeRetry = defaultFinalizeRetry
	}
	return &runController{
		store: store, agentMaxTurns: cfg.AgentMaxTurns, toolTimeout: cfg.ToolTimeout,
		pingEvery:   cfg.PingEvery,
		mergeWindow: cfg.MergeWindow, draftInterval: cfg.DraftInterval,
		finalizeRetry: cfg.FinalizeRetry,
		live:          make(map[string]*liveRun), stop: make(chan struct{}),
	}
}

func (a *API) chat(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil {
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "chat run store is not configured")
		return
	}
	if a.runs.shutting.Load() {
		writeErr(w, http.StatusServiceUnavailable, "SHUTTING_DOWN", "gateway is shutting down")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	var req chatRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" || strings.TrimSpace(req.SessionID) == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "prompt and session_id are required")
		return
	}
	req.RuntimeID = strings.TrimSpace(req.RuntimeID)
	req.FolderID = strings.TrimSpace(req.FolderID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.SkillID = strings.TrimSpace(req.SkillID)
	if req.SkillID != "" && !validSkillID(req.SkillID) {
		writeErr(w, http.StatusBadRequest, "INVALID_SKILL_ID", "skill_id is invalid")
		return
	}
	if req.RuntimeID != "" {
		if _, supported := a.runtimeByID[req.RuntimeID]; !supported {
			writeErr(w, http.StatusBadRequest, "UNSUPPORTED_RUNTIME", "agent runtime is not supported")
			return
		}
	}
	if req.FolderID != "" && req.ProjectID != "" {
		writeErr(w, http.StatusConflict, "FOLDER_PROJECT_CONFLICT", "a conversation cannot belong to both a folder and a project")
		return
	}
	if req.ProjectID != "" {
		if a.projects == nil {
			writeErr(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found")
			return
		}
		projectValue, projectErr := a.projects.ValidateReady(r.Context(), project.Identity{
			TenantID: identity.TenantID, UserID: identity.UserID, Email: identity.Email,
			Name: identity.Name, Username: identity.Username,
		}, req.ProjectID)
		if errors.Is(projectErr, project.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found")
			return
		}
		if errors.Is(projectErr, project.ErrInvalidArgument) {
			writeErr(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "project_id is invalid")
			return
		}
		if errors.Is(projectErr, project.ErrProjectNotReady) {
			writeErr(w, http.StatusConflict, "PROJECT_NOT_READY", "project is not ready")
			return
		}
		if errors.Is(projectErr, project.ErrConnectionRequired) || errors.Is(projectErr, project.ErrInstallationRequired) {
			writeErr(w, http.StatusConflict, "GITHUB_CONNECTION_REQUIRED", "connect GitHub and grant this repository access")
			return
		}
		if errors.Is(projectErr, project.ErrDisabled) {
			writeErr(w, http.StatusConflict, "GITHUB_DISABLED", "GitHub Projects are disabled")
			return
		}
		if projectErr != nil {
			writeErr(w, http.StatusServiceUnavailable, "PROJECT_UNAVAILABLE", "could not validate project")
			return
		}
		if req.RuntimeID == "" {
			req.RuntimeID = projectValue.RuntimeID
		}
	}
	if chatTypeForConversation(req) == "scheduled_task" {
		if req.FolderID != "" || req.ProjectID != "" {
			writeErr(w, http.StatusConflict, "FOLDER_UNSUPPORTED_CONVERSATION_TYPE", "scheduled task conversations cannot be moved into folders")
			return
		}
		if req.RuntimeID == "" {
			req.RuntimeID = convo.DefaultRuntimeID
		}
	}
	if _, ok := w.(http.Flusher); !ok {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "streaming unsupported")
		return
	}
	startedAt := chatStartedAt(r).UTC()
	runID := tracing.TraceID(r.Context())
	if runID == "" {
		runID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	requestID := strings.TrimSpace(req.ClientRequestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	rootSpanID := traceevents.NewSpanID()
	source := "interactive"
	if chatTypeForConversation(req) == "scheduled_task" {
		source = "scheduled_task"
	}
	run := chatrun.Run{
		ID: runID, RootSpanID: rootSpanID, ConversationID: req.SessionID,
		ConversationTitle: titleForConversation(req), UserID: identity.UserID,
		Source: source, ModelRouteID: effectiveModelRouteID(req), ModelAlias: strings.TrimSpace(req.ModelAlias),
		ClientRequestID: requestID, Status: chatrun.StatusRunning,
		StartedAt: startedAt, LastActivityAt: startedAt,
	}
	a.runs.mutationMu.Lock()
	result, err := a.runs.store.Start(r.Context(), chatrun.StartInput{
		Run: run,
		Conversation: convo.Conversation{
			ID: req.SessionID, UserID: identity.UserID, TenantID: identity.TenantID,
			Title: titleForConversation(req), ChatType: chatTypeForConversation(req),
			FolderID: req.FolderID, ProjectID: req.ProjectID, Hidden: req.DeferConversationVisibilityUntilDone, RuntimeID: req.RuntimeID,
			CreatedAt: startedAt, UpdatedAt: startedAt,
		},
		UserMessage: convo.Message{
			ID: runID + "-user", ConversationID: req.SessionID, Role: "user",
			Parts:    []convo.Part{{Type: convo.PartText, Text: req.Prompt}},
			Metadata: userMetadata(req), CreatedAt: startedAt,
		},
	})
	var live *liveRun
	if err == nil {
		run = result.Run
		req.RuntimeID = result.Conversation.RuntimeID
		req.FolderID = result.Conversation.FolderID
		req.ProjectID = result.Conversation.ProjectID
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
	if errors.Is(err, chatrun.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
		return
	}
	if errors.Is(err, chatrun.ErrConflict) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]string{
				"code": "RUN_IN_PROGRESS", "message": "conversation already has an active run",
			},
			"run_id": result.Run.ID,
		})
		return
	}
	if errors.Is(err, chatrun.ErrRuntimeMismatch) {
		writeErr(w, http.StatusConflict, "RUNTIME_MISMATCH", "conversation runtime cannot be changed")
		return
	}
	if errors.Is(err, chatrun.ErrFolderNotFound) {
		writeErr(w, http.StatusNotFound, "FOLDER_NOT_FOUND", "folder not found")
		return
	}
	if errors.Is(err, chatrun.ErrFolderMismatch) {
		writeErr(w, http.StatusConflict, "FOLDER_MISMATCH", "conversation folder cannot be changed by a chat request")
		return
	}
	if errors.Is(err, chatrun.ErrProjectNotFound) {
		writeErr(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found")
		return
	}
	if errors.Is(err, chatrun.ErrProjectNotReady) {
		writeErr(w, http.StatusConflict, "PROJECT_NOT_READY", "project is not ready")
		return
	}
	if errors.Is(err, chatrun.ErrProjectMismatch) {
		writeErr(w, http.StatusConflict, "PROJECT_MISMATCH", "conversation project cannot be changed")
		return
	}
	if errors.Is(err, chatrun.ErrProjectSingleTask) {
		writeErr(w, http.StatusConflict, "LOCAL_PROJECT_SINGLE_TASK", "local projects use one persistent task")
		return
	}
	if err != nil {
		a.runs.databaseUnavailable.Store(true)
		a.log.Warn("chat run start failed: " + err.Error())
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "could not start run")
		return
	}
	a.runs.databaseUnavailable.Store(false)
	w.Header().Set("x-cocola-run-id", run.ID)
	if live == nil {
		a.streamStoredRun(w, r, run)
		return
	}
	snapshot, updates, unsubscribe := live.subscribe()
	if result.Created {
		go a.executeLiveRun(live)
	}
	a.serveRunSubscription(w, r, run.ID, snapshot, updates, unsubscribe)
}

func (a *API) newLiveRun(r *http.Request, identity auth.Identity, req chatRequest, run chatrun.Run) *liveRun {
	ctx, cancel := context.WithCancel(context.Background())
	traceCtx := context.WithValue(context.Background(), conversationRootSpanKey{}, run.RootSpanID)
	traceRun := a.startConversationRun(traceCtx, identity, req, run.ID, run.RootSpanID, run.StartedAt)
	return &liveRun{
		run: run, identity: identity, request: req, traceCtx: traceCtx, traceRun: traceRun,
		policy: a.runs.executionPolicy(r.Context()), ctx: ctx, cancel: cancel,
		done: make(chan struct{}), reducer: convo.NewReducer(),
		subs: make(map[chan agent.Event]struct{}), status: chatrun.StatusRunning,
	}
}

func (c *runController) getLive(runID string) *liveRun {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live[runID]
}

func (r *liveRun) subscribe() (agent.Event, <-chan agent.Event, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	updates := make(chan agent.Event, subscriberBuffer)
	if chatrun.IsTerminal(r.status) {
		updates <- agent.Event{Kind: "done", Data: terminalRunData(r.run)}
		close(updates)
	} else {
		r.subs[updates] = struct{}{}
	}
	parts, _ := json.Marshal(r.reducer.Parts())
	snapshot := agent.Event{Kind: "snapshot", Data: map[string]string{
		"parts": string(parts), "status": r.status,
	}}
	return snapshot, updates, func() {
		r.mu.Lock()
		delete(r.subs, updates)
		r.mu.Unlock()
	}
}

func runDurationMS(run chatrun.Run) (int64, bool) {
	if run.StartedAt.IsZero() || run.CompletedAt == nil || run.CompletedAt.Before(run.StartedAt) {
		return 0, false
	}
	return run.CompletedAt.Sub(run.StartedAt).Milliseconds(), true
}

func terminalRunData(run chatrun.Run) map[string]string {
	data := map[string]string{"status": run.Status}
	if durationMS, ok := runDurationMS(run); ok {
		data["duration_ms"] = strconv.FormatInt(durationMS, 10)
	}
	return data
}

func (r *liveRun) publish(event agent.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for subscriber := range r.subs {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(r.subs, subscriber)
		}
	}
}

func (r *liveRun) apply(event agent.Event) {
	r.mu.Lock()
	r.reducer.Apply(event.Kind, event.Data)
	r.version++
	r.mu.Unlock()
}

func (r *liveRun) updateMemoryRecall(result memory.RecallResult) {
	data := map[string]string{"status": result.Status}
	if result.Count > 0 {
		data["count"] = strconv.Itoa(result.Count)
	}
	if result.ErrorCode != "" {
		data["error_code"] = result.ErrorCode
	}
	if result.Context != "" {
		data["content"] = result.Context
	}
	event := agent.Event{Kind: "memory_recall", Data: data}
	r.apply(event)
	r.publish(event)
}

func (r *liveRun) outputVersion() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

func (r *liveRun) parts() []convo.Part {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]convo.Part(nil), r.reducer.Parts()...)
}

func (a *API) executeLiveRun(live *liveRun) {
	defer live.cancel()
	defer close(live.done)
	var projectContext *agent.ProjectContext
	var scmToken string
	var projectBrokerCredential string
	var projectSetupErr error
	if live.request.ProjectID != "" {
		if a.projects == nil {
			projectSetupErr = project.ErrDisabled
		} else {
			value, token, err := a.projects.ProjectContext(live.ctx, project.Identity{
				TenantID: live.identity.TenantID, UserID: live.identity.UserID, Email: live.identity.Email,
				Name: live.identity.Name, Username: live.identity.Username,
			}, live.request.SessionID)
			if err != nil {
				projectSetupErr = err
			} else {
				projectContext = &agent.ProjectContext{
					ProjectID: value.ProjectID, RepositoryID: value.RepositoryExternalID,
					CloneURL: value.CloneURL, DefaultBranch: value.DefaultBranch,
					BaseSHA: value.BaseSHA, TaskBranch: value.BranchName,
					GitAuthorName: value.GitAuthorName, GitAuthorEmail: value.GitAuthorEmail,
					RepositoryProvider: value.RepositoryProvider,
					RepositoryFullName: value.RepositoryFullName,
					CredentialMode:     value.CredentialMode,
				}
				scmToken = token
				if value.RepositoryExternalID > 0 && a.projects.GitHubAgentWriteEnabled() {
					projectBrokerCredential, err = a.projects.IssueBrokerCredential(live.ctx,
						project.Identity{TenantID: live.identity.TenantID, UserID: live.identity.UserID},
						live.request.SessionID, live.run.ID)
					if err != nil && value.RepositoryProvider == project.ProviderGitHub {
						projectSetupErr = err
					}
				}
			}
		}
	}
	attachments := a.prepareRunAttachments(live.ctx, live.request)
	memoryContext := ""
	if a.memory != nil && chatTypeForConversation(live.request) != "scheduled_task" {
		recall := a.memory.Recall(
			live.ctx,
			memory.Identity{TenantID: live.identity.TenantID, UserID: live.identity.UserID},
			live.request.Prompt,
			func() {
				live.updateMemoryRecall(memory.RecallResult{Status: memory.RecallStatusRunning})
			},
		)
		memoryContext = recall.Context
		live.recalledMemoryURIs = recall.URIs
		live.updateMemoryRecall(recall)
	}
	live.query = agent.Query{
		UserID: live.identity.UserID, SessionID: live.request.SessionID,
		RuntimeID: live.request.RuntimeID, SkillID: live.request.SkillID,
		Prompt: live.request.Prompt, SandboxID: live.request.SandboxID,
		MaxTurns:            effectiveMaxTurns(live.request.MaxTurns, live.policy.agentMaxTurns),
		ModelRouteID:        effectiveModelRouteID(live.request),
		AllowWorkspaceReset: live.request.AllowWorkspaceReset,
		MemoryContext:       memoryContext,
		TraceID:             live.run.ID, ParentSpanID: conversationRootSpan(live.traceCtx),
		SandboxAuthToken: a.mintSandboxToken(live.identity), Attachments: attachments,
		SCMToken: scmToken, ProjectBrokerCredential: projectBrokerCredential,
		Project: projectContext,
	}
	coalescer := memoryEventCoalescer{run: live, window: a.runs.mergeWindow}
	var sawError bool
	var ttftMS int64
	var toolCalls int64
	draftContext, stopDrafts := context.WithCancel(context.Background())
	draftResult := make(chan error, 1)
	go func() { draftResult <- a.persistRunDrafts(draftContext, live) }()
	streamStarted := time.Now()
	watchdog := newToolStepWatchdog(live.policy.toolTimeout, live.cancel)
	err := projectSetupErr
	if err == nil {
		err = a.streamer.Stream(live.ctx, live.query, func(event agent.Event) error {
			if event.Kind == "trace" {
				a.recordAgentTrace(live.traceCtx, live.run.ID, event.Data)
				return nil
			}
			if event.Kind == "done" {
				return nil
			}
			if event.Kind == "git_snapshot" {
				a.persistProjectSnapshot(live, event)
				return nil
			}
			if event.Kind == "file" {
				artifactCtx, cancelArtifact := context.WithTimeout(context.Background(), 5*time.Second)
				event = a.registerArtifact(
					artifactCtx, live.identity, live.request.SessionID, event,
				)
				cancelArtifact()
			}
			if event.Kind == "text" && ttftMS == 0 {
				ttftMS = time.Since(streamStarted).Milliseconds()
			}
			if event.Kind == "tool_use" {
				toolCalls++
			}
			watchdog.Observe(event)
			if event.Kind == "error" {
				sawError = true
				if code := strings.TrimSpace(event.Data["code"]); strings.HasPrefix(code, "PROJECT_") {
					a.persistProjectBootstrapFailure(live, code)
				}
			}
			if err := coalescer.Push(event); err != nil {
				return err
			}
			return nil
		})
	}
	watchdog.Close()
	stepTimeout := watchdog.Failure()
	coalescer.Flush()
	stopDrafts()
	if draftErr := <-draftResult; draftErr != nil {
		err = draftErr
	}

	status, errorCode := chatrun.StatusSuccess, ""
	live.mu.Lock()
	cancelled, interrupted := live.cancelled, live.interrupt
	live.mu.Unlock()
	if cancelled {
		status, errorCode = chatrun.StatusCancelled, "USER_CANCELLED"
	} else if stepTimeout != nil {
		status, errorCode = chatrun.StatusError, "STEP_TIMEOUT"
	} else if interrupted || agent.IsRuntimeInterruption(err) {
		status, errorCode = chatrun.StatusInterrupted, "RUNTIME_INTERRUPTED"
	} else if err != nil || sawError {
		status, errorCode = chatrun.StatusError, projectRunErrorCode(err)
	}
	if status == chatrun.StatusCancelled || status == chatrun.StatusInterrupted {
		notice := "Run was cancelled."
		if status == chatrun.StatusInterrupted {
			notice = "Run was interrupted before completion."
		}
		noticeEvent := agent.Event{Kind: "text", Data: map[string]string{"text": "\n\n" + notice}}
		live.apply(noticeEvent)
		live.publish(noticeEvent)
	}
	if stepTimeout != nil && !cancelled {
		errorData := map[string]string{
			"error": fmt.Sprintf("tool step %q timed out after %s", stepTimeout.Name, stepTimeout.Limit),
			"code":  errorCode,
		}
		live.apply(agent.Event{Kind: "error", Data: errorData})
		live.publish(agent.Event{Kind: "error", Data: errorData})
	} else if err != nil && !cancelled && status != chatrun.StatusInterrupted {
		errorData := map[string]string{"error": safeBackgroundRunError(err), "code": errorCode}
		live.apply(agent.Event{Kind: "error", Data: errorData})
		live.publish(agent.Event{Kind: "error", Data: errorData})
	}
	completedAt := time.Now().UTC()
	durationMS, hasDuration := runDurationMS(chatrun.Run{
		StartedAt: live.run.StartedAt, CompletedAt: &completedAt,
	})
	metadata := assistantMetadata(live.request)
	metadata["partial"] = false
	if hasDuration {
		metadata["duration_ms"] = durationMS
	}
	if status == chatrun.StatusInterrupted {
		metadata["interrupted"] = true
	}
	message := &convo.Message{
		ID: live.run.ID + "-assistant", ConversationID: live.run.ConversationID,
		Role: "assistant", Parts: live.parts(), Metadata: metadata, CreatedAt: completedAt,
	}
	if len(message.Parts) == 0 {
		message = nil
	}
	finalizedRun, finalized := a.finalizeRun(chatrun.FinalizeInput{
		RunID: live.run.ID, UserID: live.run.UserID, Status: status, ErrorCode: errorCode,
		AssistantMessage: message, Reveal: live.request.DeferConversationVisibilityUntilDone,
		ConversationTitle: titleForConversation(live.request), CompletedAt: completedAt,
	})
	if a.projects != nil && live.request.ProjectID != "" {
		revokeCtx, cancelRevoke := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.projects.RevokeBrokerRun(revokeCtx, project.Identity{
			TenantID: live.identity.TenantID, UserID: live.identity.UserID,
		}, live.run.ID); err != nil && !errors.Is(err, project.ErrNotFound) {
			a.log.Warn("project broker run revocation failed: " + err.Error())
		}
		if err := a.projects.RevokeRunTokenLeases(revokeCtx, project.Identity{
			TenantID: live.identity.TenantID, UserID: live.identity.UserID,
		}, live.run.ID); err != nil && !errors.Is(err, project.ErrNotFound) {
			a.log.Warn("project token lease revocation failed: " + err.Error())
		}
		cancelRevoke()
	}
	// Broker validity is persisted separately from this process-local execution
	// map, so other Gateway replicas observe revocation before local teardown.
	a.runs.mu.Lock()
	delete(a.runs.live, live.run.ID)
	a.runs.mu.Unlock()
	if finalized {
		status, errorCode = finalizedRun.Status, finalizedRun.ErrorCode
		a.finishConversationRun(live.traceCtx, live.traceRun, status, errorCode, ttftMS, toolCalls)
		live.mu.Lock()
		live.status = status
		live.run = finalizedRun
		live.mu.Unlock()
		live.publish(agent.Event{Kind: "done", Data: terminalRunData(finalizedRun)})
		if a.memory != nil {
			captureCtx, cancelCapture := context.WithTimeout(context.Background(), 5*time.Second)
			if err := a.memory.ScheduleCapture(captureCtx, memory.CaptureInput{
				RunID: finalizedRun.ID, TenantID: live.identity.TenantID,
				UserID: live.identity.UserID, ConversationID: finalizedRun.ConversationID,
				Source: finalizedRun.Source, RecalledURIs: live.recalledMemoryURIs,
			}); err != nil {
				a.log.Warn("memory capture scheduling failed: " + err.Error())
			}
			cancelCapture()
		}
	}
	live.mu.Lock()
	for subscriber := range live.subs {
		close(subscriber)
		delete(live.subs, subscriber)
	}
	live.mu.Unlock()
}

func (a *API) persistProjectSnapshot(live *liveRun, event agent.Event) {
	if a.projects == nil || live.request.ProjectID == "" {
		return
	}
	raw := strings.TrimSpace(event.Data["snapshot_json"])
	if raw == "" {
		return
	}
	var snapshot project.GitSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		a.log.Warn("invalid git snapshot event")
		return
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.projects.SaveSnapshot(ctx, project.Identity{
		TenantID: live.identity.TenantID, UserID: live.identity.UserID,
	}, live.request.SessionID, snapshot, snapshot.HeadSHA, "ready"); err != nil {
		a.log.Warn("git snapshot persistence failed: " + err.Error())
	}
}

func (a *API) persistProjectBootstrapFailure(live *liveRun, code string) {
	if a.projects == nil || live.request.ProjectID == "" || !strings.HasPrefix(code, "PROJECT_") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.projects.MarkBootstrapFailed(ctx, project.Identity{
		TenantID: live.identity.TenantID, UserID: live.identity.UserID,
	}, live.request.SessionID, code); err != nil {
		a.log.Warn("project bootstrap failure persistence failed: " + err.Error())
	}
}

func validSkillID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for i, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			continue
		}
		if i > 0 && (char == '-' || char == '_' || char == '.') {
			continue
		}
		return false
	}
	return true
}

func (a *API) prepareRunAttachments(ctx context.Context, req chatRequest) []agent.Attachment {
	attachments := make([]agent.Attachment, 0, len(req.Attachments))
	for _, dto := range req.Attachments {
		content, err := base64.StdEncoding.DecodeString(dto.ContentB64)
		if err != nil {
			a.log.Warn("dropping attachment with invalid base64 content")
			continue
		}
		attachment := agent.Attachment{
			Filename: dto.Filename, Content: content, Mime: dto.Mime, Size: int64(len(content)),
		}
		if a.store != nil {
			key := objectKey(req.SessionID, attachment.Filename)
			if err := a.store.Put(ctx, key, content, attachment.Mime); err != nil {
				a.log.Warn("attachment object-store upload failed, delivering inline: " + err.Error())
			} else {
				attachment.OssKey = key
				if attachment.Size > a.inlineMaxBytes {
					attachment.Content = nil
				}
			}
		}
		attachments = append(attachments, attachment)
	}
	return attachments
}

func (a *API) saveRunDraft(live *liveRun) error {
	parts := live.parts()
	if len(parts) == 0 {
		return nil
	}
	metadata := assistantMetadata(live.request)
	metadata["partial"] = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.runs.store.SaveDraft(ctx, live.run.ID, live.run.UserID, convo.Message{
		ID: live.run.ID + "-assistant", ConversationID: live.run.ConversationID,
		Role: "assistant", Parts: parts, Metadata: metadata,
		CreatedAt: live.run.StartedAt.Add(time.Microsecond),
	})
}

func (a *API) persistRunDrafts(ctx context.Context, live *liveRun) error {
	ticker := time.NewTicker(a.runs.draftInterval)
	defer ticker.Stop()
	var failureSince time.Time
	var savedVersion uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			version := live.outputVersion()
			if version == savedVersion {
				continue
			}
			if err := a.saveRunDraft(live); err != nil {
				a.runs.databaseUnavailable.Store(true)
				if failureSince.IsZero() {
					failureSince = time.Now()
				}
				if time.Since(failureSince) >= draftFailureBudget {
					live.cancel()
					return fmt.Errorf("assistant draft unavailable for 30s: %w", err)
				}
				continue
			}
			a.runs.databaseUnavailable.Store(false)
			failureSince = time.Time{}
			savedVersion = version
		}
	}
}

func (a *API) finalizeRun(input chatrun.FinalizeInput) (chatrun.Run, bool) {
	var lastErr error
	for attempt := 1; attempt <= finalizeMaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), finalizeAttemptLimit)
		run, err := a.runs.store.Finalize(ctx, input)
		cancel()
		if err == nil {
			a.runs.databaseUnavailable.Store(false)
			return run, true
		}
		lastErr = err
		a.runs.databaseUnavailable.Store(true)
		if attempt == finalizeMaxAttempts {
			break
		}
		a.log.Warn(fmt.Sprintf("chat run finalization failed; retrying (%d/%d): %v",
			attempt, finalizeMaxAttempts, err))
		timer := time.NewTimer(a.runs.finalizeRetry)
		select {
		case <-a.runs.stop:
			timer.Stop()
			return chatrun.Run{}, false
		case <-timer.C:
		}
	}

	// A malformed assistant payload or a concurrently removed conversation must
	// not leave an immortal running row. Make one final, message-free transition
	// to interrupted. A total database outage may still reject this write; in
	// that case readiness stays failed and startup recovery closes the stale row.
	fallback := input
	fallback.Status = chatrun.StatusInterrupted
	fallback.ErrorCode = "FINALIZATION_FAILED"
	fallback.AssistantMessage = nil
	fallback.Reveal = false
	ctx, cancel := context.WithTimeout(context.Background(), finalizeAttemptLimit)
	run, fallbackErr := a.runs.store.Finalize(ctx, fallback)
	cancel()
	if fallbackErr == nil {
		a.runs.databaseUnavailable.Store(false)
		a.log.Warn("chat run output could not be finalized; saved interrupted terminal state: " + lastErr.Error())
		return run, true
	}
	a.log.Warn(fmt.Sprintf("chat run finalization abandoned after %d attempts: %v; fallback: %v",
		finalizeMaxAttempts, lastErr, fallbackErr))
	return chatrun.Run{}, false
}

type memoryEventCoalescer struct {
	run     *liveRun
	window  time.Duration
	mu      sync.Mutex
	pending *agent.Event
	timer   *time.Timer
}

func (c *memoryEventCoalescer) Push(event agent.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := ""
	if event.Kind == "text" {
		key = "text"
	} else if event.Kind == "thinking" {
		key = "thinking"
	}
	if key == "" {
		c.flushLocked()
		c.run.apply(event)
		c.run.publish(event)
		return nil
	}
	if c.pending != nil && c.pending.Kind == event.Kind {
		c.pending.Data[key] += event.Data[key]
	} else {
		c.flushLocked()
		copy := agent.Event{Kind: event.Kind, Data: cloneStringMap(event.Data)}
		c.pending = &copy
	}
	if c.timer == nil {
		c.timer = time.AfterFunc(c.window, c.Flush)
	}
	return nil
}

func (c *memoryEventCoalescer) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
}

func (c *memoryEventCoalescer) flushLocked() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.pending == nil {
		return
	}
	event := *c.pending
	c.pending = nil
	c.run.apply(event)
	c.run.publish(event)
}

func cloneStringMap(data map[string]string) map[string]string {
	out := make(map[string]string, len(data))
	for key, value := range data {
		out[key] = value
	}
	return out
}

func safeBackgroundRunError(err error) string {
	if errors.Is(err, project.ErrConnectionRequired) || errors.Is(err, project.ErrInstallationRequired) {
		return "GitHub is disconnected or no longer grants access to this repository"
	}
	if errors.Is(err, project.ErrDisabled) {
		return "GitHub Projects are disabled"
	}
	if errors.Is(err, project.ErrProjectNotReady) {
		return "Project is not ready"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "agent request timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "agent run stopped"
	}
	return "agent run failed"
}

func projectRunErrorCode(err error) string {
	switch {
	case errors.Is(err, project.ErrConnectionRequired), errors.Is(err, project.ErrInstallationRequired):
		return "GITHUB_CONNECTION_REQUIRED"
	case errors.Is(err, project.ErrDisabled):
		return "GITHUB_DISABLED"
	case errors.Is(err, project.ErrProjectNotReady):
		return "PROJECT_NOT_READY"
	default:
		return "AGENT_ERROR"
	}
}

func (a *API) serveRunSubscription(
	w http.ResponseWriter,
	r *http.Request,
	runID string,
	snapshot agent.Event,
	updates <-chan agent.Event,
	unsubscribe func(),
) {
	defer unsubscribe()
	flusher := w.(http.Flusher)
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("connection", "keep-alive")
	w.Header().Set("x-accel-buffering", "no")
	w.Header().Set("x-cocola-run-id", runID)
	w.WriteHeader(http.StatusOK)
	if err := writeSSE(w, flusher, snapshot); err != nil {
		return
	}
	ping := time.NewTicker(a.runs.pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-updates:
			if !ok {
				return
			}
			if err := writeSSE(w, flusher, event); err != nil {
				return
			}
			if event.Kind == "done" {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (a *API) streamStoredRun(w http.ResponseWriter, r *http.Request, run chatrun.Run) {
	parts := []convo.Part{}
	if a.convo != nil {
		messages, err := a.convo.GetMessages(r.Context(), run.ConversationID, run.UserID)
		if err != nil {
			a.runs.databaseUnavailable.Store(true)
			a.log.Warn("stored chat run snapshot unavailable: " + err.Error())
			writeErr(w, http.StatusServiceUnavailable, "CHAT_HISTORY_UNAVAILABLE", "saved run output is unavailable")
			return
		}
		a.runs.databaseUnavailable.Store(false)
		for _, message := range messages {
			if message.ID == run.ID+"-assistant" {
				parts = message.Parts
				break
			}
		}
	}
	encodedParts, err := json.Marshal(parts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not encode saved run output")
		return
	}
	flusher := w.(http.Flusher)
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("x-cocola-run-id", run.ID)
	w.WriteHeader(http.StatusOK)
	_ = writeSSE(w, flusher, agent.Event{Kind: "snapshot", Data: map[string]string{
		"parts": string(encodedParts), "status": run.Status,
	}})
	_ = writeSSE(w, flusher, agent.Event{Kind: "done", Data: terminalRunData(run)})
}

func (a *API) streamRun(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "run not found")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	run, err := a.runs.store.GetOwned(r.Context(), r.PathValue("run_id"), identity.UserID)
	if errors.Is(err, chatrun.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "run not found")
		return
	}
	if err != nil {
		a.runs.databaseUnavailable.Store(true)
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "run state is unavailable")
		return
	}
	a.runs.databaseUnavailable.Store(false)
	live := a.runs.getLive(run.ID)
	if live == nil {
		if _, ok := w.(http.Flusher); !ok {
			writeErr(w, http.StatusInternalServerError, "INTERNAL", "streaming unsupported")
			return
		}
		a.streamStoredRun(w, r, run)
		return
	}
	snapshot, updates, unsubscribe := live.subscribe()
	a.serveRunSubscription(w, r, run.ID, snapshot, updates, unsubscribe)
}

func (a *API) activeRun(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "active run not found")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	run, err := a.runs.store.Active(r.Context(), r.URL.Query().Get("conversation_id"), identity.UserID)
	if errors.Is(err, chatrun.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "active run not found")
		return
	}
	if err != nil {
		a.runs.databaseUnavailable.Store(true)
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "run state is unavailable")
		return
	}
	a.runs.databaseUnavailable.Store(false)
	writeJSON(w, http.StatusOK, run)
}

func (a *API) cancelRun(w http.ResponseWriter, r *http.Request) {
	if a.runs == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "run not found")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	run, err := a.runs.store.GetOwned(r.Context(), r.PathValue("run_id"), identity.UserID)
	if errors.Is(err, chatrun.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "run not found")
		return
	}
	if err != nil {
		a.runs.databaseUnavailable.Store(true)
		writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "run state is unavailable")
		return
	}
	a.runs.databaseUnavailable.Store(false)
	if chatrun.IsTerminal(run.Status) {
		writeJSON(w, http.StatusOK, run)
		return
	}
	live := a.runs.getLive(run.ID)
	if live == nil {
		writeErr(w, http.StatusConflict, "RUN_NOT_LOCAL", "run is no longer executing")
		return
	}
	live.mu.Lock()
	live.cancelled = true
	live.mu.Unlock()
	live.cancel()
	writeJSON(w, http.StatusAccepted, run)
}

func (a *API) ShutdownRuns(ctx context.Context) error {
	if a.runs == nil {
		return nil
	}
	a.runs.shutting.Store(true)
	a.runs.mu.Lock()
	liveRuns := make([]*liveRun, 0, len(a.runs.live))
	for _, live := range a.runs.live {
		live.mu.Lock()
		live.interrupt = true
		live.mu.Unlock()
		live.cancel()
		liveRuns = append(liveRuns, live)
	}
	a.runs.mu.Unlock()
	for _, live := range liveRuns {
		select {
		case <-live.done:
		case <-ctx.Done():
			a.runs.stopOnce.Do(func() { close(a.runs.stop) })
			return ctx.Err()
		}
	}
	a.runs.stopOnce.Do(func() { close(a.runs.stop) })
	return nil
}

func (a *API) InterruptStaleRuns(ctx context.Context) error {
	if a.runs == nil {
		return nil
	}
	count, err := a.runs.store.InterruptRunning(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	if count > 0 {
		a.log.Warn(fmt.Sprintf("marked %d stale chat runs interrupted", count))
	}
	return nil
}
