package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	feishuconnector "github.com/cocola-project/cocola/apps/gateway/internal/channel/feishu"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

const (
	defaultAgentMaxTurns = int32(200)
	defaultToolTimeout   = 10 * time.Minute
	defaultSSEPing       = 15 * time.Second
	defaultMergeWindow   = 100 * time.Millisecond
	defaultDraftInterval = time.Second
	defaultFinalizeRetry = time.Second
	feishuCredentialWait = 5 * time.Second
	draftFailureBudget   = 30 * time.Second
	finalizeAttemptLimit = 3 * time.Second
	finalizeMaxAttempts  = 4
	finalizeRecoveryMax  = 30 * time.Second
	subscriberBuffer     = 64
	maxWikiRefsPerTurn   = 20
	maxWikiBytesPerTurn  = int64(100 << 20)
	maxChatRequestBytes  = int64(48 << 20)
	maxChatAttachments   = 8
	maxChatAttachment    = int64(32 << 20)
	maxChatAttachmentSum = int64(32 << 20)
	emptyAgentResponse   = "EMPTY_AGENT_RESPONSE"
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
	conversationGate    *conversationRunGate
	mu                  sync.Mutex
	live                map[string]*liveRun
	shutting            atomic.Bool
	databaseUnavailable atomic.Bool
	stop                chan struct{}
	stopOnce            sync.Once
}

type conversationRunGate struct {
	mu      sync.Mutex
	entries map[string]*conversationRunGateEntry
}

type conversationRunGateEntry struct {
	mu   sync.Mutex
	refs int
}

func newConversationRunGate() *conversationRunGate {
	return &conversationRunGate{entries: make(map[string]*conversationRunGateEntry)}
}

func (g *conversationRunGate) lock(conversationID string) func() {
	g.mu.Lock()
	entry := g.entries[conversationID]
	if entry == nil {
		entry = &conversationRunGateEntry{}
		g.entries[conversationID] = entry
	}
	entry.refs++
	g.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		g.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(g.entries, conversationID)
		}
		g.mu.Unlock()
	}
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
	planContent        string
	workspaceRevision  string
	questionText       string
	questionOptions    []convo.QuestionOption
	runtimeAccepted    bool
	runtimeErrorCode   string
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
		finalizeRetry:    cfg.FinalizeRetry,
		conversationGate: newConversationRunGate(),
		live:             make(map[string]*liveRun), stop: make(chan struct{}),
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
	r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeErr(
				w,
				http.StatusRequestEntityTooLarge,
				"CHAT_REQUEST_TOO_LARGE",
				"chat request body exceeds the 48 MiB limit",
			)
			return
		}
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeErr(
				w,
				http.StatusRequestEntityTooLarge,
				"CHAT_REQUEST_TOO_LARGE",
				"chat request body exceeds the 48 MiB limit",
			)
			return
		}
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" || strings.TrimSpace(req.SessionID) == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "prompt and session_id are required")
		return
	}
	if len(req.Attachments) > maxChatAttachments {
		writeErr(
			w,
			http.StatusRequestEntityTooLarge,
			"ATTACHMENT_LIMIT_EXCEEDED",
			"too many attachments in one chat turn",
		)
		return
	}
	if err := decodeChatAttachments(req.Attachments, maxChatAttachment, maxChatAttachmentSum); err != nil {
		if errors.Is(err, errChatAttachmentLimit) {
			writeErr(
				w,
				http.StatusRequestEntityTooLarge,
				"ATTACHMENT_LIMIT_EXCEEDED",
				"attachments exceed the 32 MiB per-turn limit",
			)
			return
		}
		writeErr(w, http.StatusBadRequest, "INVALID_ATTACHMENT", "attachment content is not valid base64")
		return
	}
	req.RuntimeID = strings.TrimSpace(req.RuntimeID)
	req.InteractionMode = strings.TrimSpace(req.InteractionMode)
	req.RevisionOfPlanID = strings.TrimSpace(req.RevisionOfPlanID)
	req.ReasoningEffort = strings.TrimSpace(req.ReasoningEffort)
	if !validReasoningEffort(req.ReasoningEffort) {
		writeErr(w, http.StatusBadRequest, "UNSUPPORTED_REASONING_EFFORT", "reasoning_effort is not supported")
		return
	}
	if req.InteractionMode == "" {
		req.InteractionMode = chatrun.InteractionModeExecute
	}
	if req.InteractionMode != chatrun.InteractionModeExecute &&
		req.InteractionMode != chatrun.InteractionModePlan {
		writeErr(w, http.StatusBadRequest, "UNSUPPORTED_INTERACTION_MODE", "interaction_mode must be execute or plan")
		return
	}
	if (req.RevisionOfPlanID == "") != (req.ExpectedPlanVersion == 0) {
		writeErr(w, http.StatusBadRequest, "INVALID_PLAN_REVISION", "revision plan id and version must be provided together")
		return
	}
	if req.RevisionOfPlanID != "" {
		if _, err := uuid.Parse(req.RevisionOfPlanID); err != nil || req.ExpectedPlanVersion <= 0 {
			writeErr(w, http.StatusBadRequest, "INVALID_PLAN_REVISION", "revision plan id or version is invalid")
			return
		}
		if req.InteractionMode != chatrun.InteractionModePlan {
			writeErr(w, http.StatusBadRequest, "INVALID_PLAN_REVISION", "plan revisions require Plan mode")
			return
		}
	}
	req.FolderID = strings.TrimSpace(req.FolderID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ProjectBaseRef = strings.TrimSpace(req.ProjectBaseRef)
	req.SkillID = strings.TrimSpace(req.SkillID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	if req.SkillID != "" && !validSkillID(req.SkillID) {
		writeErr(w, http.StatusBadRequest, "INVALID_SKILL_ID", "skill_id is invalid")
		return
	}
	if len(req.WikiRefs) > maxWikiRefsPerTurn {
		writeErr(
			w,
			http.StatusRequestEntityTooLarge,
			"WIKI_REFERENCE_LIMIT_EXCEEDED",
			"too many Wiki files were referenced in one turn",
		)
		return
	}
	wikiNodeIDs := make([]string, 0, len(req.WikiRefs))
	seenWikiNodeIDs := make(map[string]struct{}, len(req.WikiRefs))
	if len(req.WikiRefs) > 0 {
		for _, reference := range req.WikiRefs {
			nodeID := strings.TrimSpace(reference.NodeID)
			parsedNodeID, err := uuid.Parse(nodeID)
			if err != nil || len(nodeID) != 36 {
				writeErr(w, http.StatusBadRequest, "INVALID_WIKI_REFERENCE", "Wiki reference id must be a UUID")
				return
			}
			nodeID = parsedNodeID.String()
			if _, duplicate := seenWikiNodeIDs[nodeID]; duplicate {
				continue
			}
			seenWikiNodeIDs[nodeID] = struct{}{}
			wikiNodeIDs = append(wikiNodeIDs, nodeID)
		}
	}
	if err := a.resolveChatAgent(r.Context(), identity, &req); err != nil {
		switch {
		case errors.Is(err, agentprofile.ErrInvalidArgument):
			writeErr(w, http.StatusBadRequest, "INVALID_AGENT_ID", "agent_id is invalid")
		case errors.Is(err, agentprofile.ErrNotFound):
			writeErr(w, http.StatusNotFound, "AGENT_NOT_FOUND", "Agent not found")
		case errors.Is(err, agentprofile.ErrArchived):
			writeErr(w, http.StatusConflict, "AGENT_ARCHIVED", "Agent is archived")
		case errors.Is(err, convo.ErrAgentMismatch):
			writeErr(w, http.StatusConflict, "AGENT_MISMATCH", "conversation Agent cannot be changed")
		default:
			a.log.Warn("chat Agent resolution failed: " + strings.ReplaceAll(err.Error(), "\n", " "))
			writeErr(w, http.StatusServiceUnavailable, "AGENTS_UNAVAILABLE", "could not resolve Agent")
		}
		return
	}
	agentWikiCount := 0
	for _, source := range req.AgentKnowledgeSources {
		if source.Type == agentprofile.KnowledgeTypeCocolaWiki {
			agentWikiCount++
		}
	}
	if len(wikiNodeIDs)+agentWikiCount > maxWikiRefsPerTurn {
		writeErr(
			w,
			http.StatusRequestEntityTooLarge,
			"WIKI_REFERENCE_LIMIT_EXCEEDED",
			"Wiki references exceed the per-turn file limit",
		)
		return
	}
	if len(wikiNodeIDs)+len(req.AgentKnowledgeSources) > 0 {
		if a.wiki == nil || a.store == nil {
			if len(wikiNodeIDs) > 0 {
				writeErr(w, http.StatusServiceUnavailable, "WIKI_UNAVAILABLE", "Wiki is not configured")
				return
			}
		}
		wikiIdentity := wiki.Identity{
			TenantID: identity.TenantID,
			UserID:   identity.UserID,
		}
		var totalBytes int64
		if len(wikiNodeIDs) > 0 {
			nodes, versions, resolveErr := a.wiki.ResolveCurrent(r.Context(), wikiIdentity, wikiNodeIDs)
			if errors.Is(resolveErr, wiki.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "WIKI_REFERENCE_NOT_FOUND", "a referenced Wiki file no longer exists")
				return
			}
			if resolveErr != nil || len(nodes) != len(versions) {
				writeErr(w, http.StatusServiceUnavailable, "WIKI_UNAVAILABLE", "could not resolve Wiki references")
				return
			}
			for index := range nodes {
				reference, nextTotal, ok := resolvedWikiReference(nodes[index], versions[index], totalBytes)
				if !ok {
					writeErr(
						w,
						http.StatusRequestEntityTooLarge,
						"WIKI_REFERENCE_LIMIT_EXCEEDED",
						"referenced Wiki files exceed the per-turn size limit",
					)
					return
				}
				totalBytes = nextTotal
				req.WikiReferences = append(req.WikiReferences, reference)
			}
		}
		for _, source := range req.AgentKnowledgeSources {
			entry := agent.AgentKnowledgeEntry{
				SourceID: agentprofile.KnowledgeSourceID(source),
				Source: agent.AgentKnowledgeSource{
					Type: source.Type, Label: source.Label, URL: source.URL, NodeID: source.NodeID,
				},
				State: agent.KnowledgeSourceReady,
			}
			if source.Type != agentprofile.KnowledgeTypeCocolaWiki {
				req.AgentKnowledgeEntries = append(req.AgentKnowledgeEntries, entry)
				continue
			}
			if a.wiki == nil || a.store == nil {
				entry.State = agent.KnowledgeSourceTemporarilyUnavailable
				req.AgentKnowledgeEntries = append(req.AgentKnowledgeEntries, entry)
				continue
			}
			node, version, resolveErr := a.wiki.GetCurrent(r.Context(), wikiIdentity, source.NodeID)
			switch {
			case errors.Is(resolveErr, wiki.ErrNotFound):
				entry.State = agent.KnowledgeSourceUnavailable
			case resolveErr != nil:
				entry.State = agent.KnowledgeSourceTemporarilyUnavailable
			default:
				reference, nextTotal, ok := resolvedWikiReference(node, version, totalBytes)
				if !ok {
					entry.State = agent.KnowledgeSourceUnavailable
					break
				}
				totalBytes = nextTotal
				entry.WikiReference = &reference
			}
			req.AgentKnowledgeEntries = append(req.AgentKnowledgeEntries, entry)
		}
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
	if req.ProjectBaseRef != "" && req.ProjectID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "project_base_ref requires project_id")
		return
	}
	if len(req.ProjectBaseRef) > 1024 || strings.ContainsAny(req.ProjectBaseRef, "\x00\r\n") {
		writeErr(w, http.StatusBadRequest, "INVALID_PROJECT_BASE_REF", "project_base_ref is invalid")
		return
	}
	var projectBaseRef string
	var projectBaseSHA string
	if req.ProjectID != "" {
		if a.projects == nil {
			writeErr(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found")
			return
		}
		taskBase, projectErr := a.projects.PrepareTaskBase(r.Context(), project.Identity{
			TenantID: identity.TenantID, UserID: identity.UserID, Email: identity.Email,
			Name: identity.Name, Username: identity.Username,
		}, req.ProjectID, req.SessionID, req.ProjectBaseRef)
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
		if errors.Is(projectErr, project.ErrBaseRefNotFound) {
			writeErr(w, http.StatusUnprocessableEntity, "PROJECT_BASE_REF_NOT_FOUND", "the selected branch no longer exists")
			return
		}
		if errors.Is(projectErr, project.ErrBaseRefMismatch) {
			writeErr(w, http.StatusConflict, "PROJECT_BASE_MISMATCH", "a project task base branch cannot be changed")
			return
		}
		if errors.Is(projectErr, project.ErrConflict) {
			writeErr(w, http.StatusConflict, "PROJECT_MISMATCH", "conversation project cannot be changed")
			return
		}
		if projectErr != nil {
			writeErr(w, http.StatusServiceUnavailable, "PROJECT_UNAVAILABLE", "could not validate project")
			return
		}
		projectBaseRef, projectBaseSHA = taskBase.Ref, taskBase.SHA
		if req.RuntimeID == "" {
			req.RuntimeID = taskBase.Project.RuntimeID
		}
	}
	if chatTypeForConversation(req) == "scheduled_task" {
		if req.InteractionMode == chatrun.InteractionModePlan {
			writeErr(w, http.StatusConflict, "PLAN_MODE_UNSUPPORTED", "Plan mode is not supported for scheduled tasks.")
			return
		}
		if req.FolderID != "" || req.ProjectID != "" {
			writeErr(w, http.StatusConflict, "FOLDER_UNSUPPORTED_CONVERSATION_TYPE", "scheduled task conversations cannot be moved into folders")
			return
		}
	}
	if req.RuntimeID == "" {
		req.RuntimeID = a.productConfig.AgentRuntime.DefaultID
	}
	if req.InteractionMode == chatrun.InteractionModePlan && req.RuntimeID != "claude-code" {
		writeErr(w, http.StatusConflict, "PLAN_MODE_UNSUPPORTED", "Plan mode is supported only for Claude Code conversations.")
		return
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
		ReasoningEffort: req.ReasoningEffort,
		ClientRequestID: requestID, InteractionMode: req.InteractionMode, Status: chatrun.StatusRunning,
		StartedAt: startedAt, LastActivityAt: startedAt,
	}
	unlockConversation := a.runs.conversationGate.lock(req.SessionID)
	a.runs.mutationMu.Lock()
	result, err := a.runs.store.Start(r.Context(), chatrun.StartInput{
		Run: run,
		Conversation: convo.Conversation{
			ID: req.SessionID, UserID: identity.UserID, TenantID: identity.TenantID,
			Title: titleForConversation(req), ChatType: chatTypeForConversation(req),
			FolderID: req.FolderID, ProjectID: req.ProjectID, Hidden: req.DeferConversationVisibilityUntilDone, RuntimeID: req.RuntimeID,
			AgentID: req.AgentID, AgentVersion: agentSnapshotVersion(req.AgentSnapshot),
			AgentSnapshot: req.AgentSnapshot, ChannelConnectorID: req.ChannelConnectorID,
			CreatedAt: startedAt, UpdatedAt: startedAt,
		},
		UserMessage: convo.Message{
			ID: runID + "-user", ConversationID: req.SessionID, Role: "user",
			Parts:    userMessageParts(req),
			Metadata: userMetadata(req), CreatedAt: startedAt,
		},
		ProjectBaseRef:              projectBaseRef,
		ProjectBaseSHA:              projectBaseSHA,
		RevisionPlanID:              req.RevisionOfPlanID,
		ExpectedRevisionPlanVersion: req.ExpectedPlanVersion,
	})
	var live *liveRun
	if err == nil {
		run = result.Run
		req.RuntimeID = result.Conversation.RuntimeID
		req.FolderID = result.Conversation.FolderID
		req.ProjectID = result.Conversation.ProjectID
		req.AgentID = result.Conversation.AgentID
		req.AgentSnapshot = result.Conversation.AgentSnapshot
		req.ChannelConnectorID = result.Conversation.ChannelConnectorID
		if req.AgentSnapshot != nil {
			req.RuntimeID = req.AgentSnapshot.RuntimeID
			req.ModelRouteID = req.AgentSnapshot.ModelRouteID
			req.ModelAlias = req.AgentSnapshot.ModelAlias
		}
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
	if errors.Is(err, chatrun.ErrAgentMismatch) {
		writeErr(w, http.StatusConflict, "AGENT_MISMATCH", "conversation Agent cannot be changed")
		return
	}
	if errors.Is(err, chatrun.ErrAgentArchived) {
		writeErr(w, http.StatusConflict, "AGENT_ARCHIVED", "Agent is archived")
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
	if errors.Is(err, chatrun.ErrQuestionPending) {
		writeErr(
			w,
			http.StatusConflict,
			"QUESTION_PENDING",
			"Answer or cancel Claude's pending question before starting another run.",
		)
		return
	}
	if errors.Is(err, chatrun.ErrPlanNotCurrent) {
		writeErr(w, http.StatusConflict, "PLAN_NOT_CURRENT", "This plan is no longer current. Review the latest plan before revising it.")
		return
	}
	if errors.Is(err, chatrun.ErrPlanState) {
		writeErr(w, http.StatusConflict, "PLAN_STATE", "This plan can no longer be revised.")
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

func resolvedWikiReference(
	node wiki.Node,
	version wiki.Version,
	totalBytes int64,
) (agent.WikiReference, int64, bool) {
	if version.SizeBytes < 0 || version.SizeBytes > maxWikiBytesPerTurn-totalBytes {
		return agent.WikiReference{}, totalBytes, false
	}
	return agent.WikiReference{
		NodeID: node.ID, VersionID: version.ID, Revision: version.Revision,
		LogicalPath: node.LogicalPath, Filename: node.Name,
		Mime: version.MimeType, ObjectKey: version.ObjectKey,
		Size: version.SizeBytes, SHA256: version.SHA256,
	}, totalBytes + version.SizeBytes, true
}

func (a *API) resolveChatAgent(
	ctx context.Context,
	identity auth.Identity,
	req *chatRequest,
) error {
	var existing convo.Conversation
	existingFound := false
	if a.convo != nil {
		value, err := a.convo.GetConversation(ctx, req.SessionID, identity.UserID)
		switch {
		case err == nil:
			existing = value
			existingFound = true
		case errors.Is(err, convo.ErrNotFound):
		default:
			return err
		}
	}
	if existingFound {
		if existing.AgentID != req.AgentID {
			return convo.ErrAgentMismatch
		}
		if existing.AgentID == "" {
			req.AgentSnapshot = nil
			return nil
		}
		if existing.AgentSnapshot == nil ||
			existing.AgentSnapshot.ID != existing.AgentID ||
			existing.AgentSnapshot.Version != existing.AgentVersion {
			return errors.New("conversation Agent snapshot is invalid")
		}
		if a.agents == nil {
			return errors.New("Agent service is unavailable")
		}
		current, err := a.agents.GetActive(ctx, agentprofile.Identity{
			TenantID: identity.TenantID, UserID: identity.UserID,
		}, existing.AgentID)
		if err != nil {
			if errors.Is(err, agentprofile.ErrNotFound) ||
				errors.Is(err, agentprofile.ErrArchived) ||
				errors.Is(err, agentprofile.ErrInvalidArgument) {
				return err
			}
			snapshot := *existing.AgentSnapshot
			applyAgentSnapshot(req, &snapshot)
			applySnapshotAgentKnowledge(req, snapshot)
			req.ChannelConnectorID = existing.ChannelConnectorID
			return nil
		}
		snapshot := *existing.AgentSnapshot
		applyAgentSnapshot(req, &snapshot)
		applyLiveAgentKnowledge(req, current)
		req.ChannelConnectorID = existing.ChannelConnectorID
		return nil
	}
	if req.AgentID == "" {
		req.AgentSnapshot = nil
		return nil
	}
	if a.agents == nil {
		return errors.New("Agent service is unavailable")
	}
	value, err := a.agents.GetActive(ctx, agentprofile.Identity{
		TenantID: identity.TenantID, UserID: identity.UserID,
	}, req.AgentID)
	if err != nil {
		return err
	}
	snapshot := value.Snapshot()
	applyAgentSnapshot(req, &snapshot)
	applyLiveAgentKnowledge(req, value)
	if a.feishu != nil {
		connectorID, connectorErr := a.feishu.ConnectorID(ctx, feishuconnector.Identity{
			TenantID: identity.TenantID, UserID: identity.UserID,
		}, req.AgentID)
		if connectorErr != nil {
			return connectorErr
		}
		req.ChannelConnectorID = connectorID
	}
	return nil
}

func (a *API) writeConversationAgentError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, agentprofile.ErrArchived):
		writeErr(w, http.StatusConflict, "AGENT_ARCHIVED", "Agent is archived")
	case errors.Is(err, agentprofile.ErrNotFound):
		writeErr(w, http.StatusNotFound, "AGENT_NOT_FOUND", "Agent not found")
	default:
		a.log.Warn("conversation Agent validation failed: " + strings.ReplaceAll(err.Error(), "\n", " "))
		writeErr(w, http.StatusServiceUnavailable, "AGENTS_UNAVAILABLE", "could not resolve Agent")
	}
	return true
}

func applyAgentSnapshot(req *chatRequest, snapshot *agentprofile.Snapshot) {
	req.AgentID = snapshot.ID
	req.AgentSnapshot = snapshot
	req.RuntimeID = snapshot.RuntimeID
	req.ModelRouteID = snapshot.ModelRouteID
	req.ModelAlias = snapshot.ModelAlias
	req.ModelLabel = snapshot.ModelAlias
	req.ModelProvider = ""
	req.ModelFamily = ""
	req.ModelIconSlug = ""
	req.ModelIcon = nil
}

func applyLiveAgentKnowledge(req *chatRequest, value agentprofile.Agent) {
	req.AgentKnowledgeRevision = value.KnowledgeRevision
	req.AgentKnowledgeSources = append(
		[]agentprofile.KnowledgeSource(nil),
		value.KnowledgeSources...,
	)
}

func applySnapshotAgentKnowledge(req *chatRequest, snapshot agentprofile.Snapshot) {
	req.AgentKnowledgeRevision = snapshot.KnowledgeRevision
	req.AgentKnowledgeSources = append(
		[]agentprofile.KnowledgeSource(nil),
		snapshot.KnowledgeSources...,
	)
}

func (a *API) ensureConversationAgentActive(
	ctx context.Context,
	identity auth.Identity,
	conversation convo.Conversation,
) error {
	if conversation.AgentID == "" {
		return nil
	}
	if a.agents == nil {
		return errors.New("Agent service is unavailable")
	}
	_, err := a.agents.GetActive(ctx, agentprofile.Identity{
		TenantID: identity.TenantID, UserID: identity.UserID,
	}, conversation.AgentID)
	return err
}

func agentSnapshotVersion(snapshot *agentprofile.Snapshot) int64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.Version
}

func agentInstructionsLength(snapshot *agentprofile.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	return len(snapshot.Instructions)
}

func userMessageParts(req chatRequest) []convo.Part {
	parts := []convo.Part{{Type: convo.PartText, Text: req.Prompt}}
	for index, attachment := range req.Attachments {
		mimeType := strings.TrimSpace(attachment.Mime)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		parts = append(parts, convo.Part{
			Type: convo.PartFile, ID: fmt.Sprintf("user-attachment-%d", index),
			Filename: attachment.Filename, MimeType: mimeType, Size: int64(len(attachment.Content)),
		})
	}
	for _, reference := range req.WikiReferences {
		parts = append(parts, convo.Part{
			Type: convo.PartWikiFile, ID: reference.VersionID,
			Filename: reference.Filename, MimeType: reference.Mime, Size: reference.Size,
			DownloadURL: "/api/wiki/versions/" + reference.VersionID + "/download",
			WikiNodeID:  reference.NodeID, WikiVersionID: reference.VersionID,
			LogicalPath: reference.LogicalPath, Revision: reference.Revision,
		})
	}
	return parts
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

func runSummaryData(run chatrun.Run, modelLabel string) map[string]string {
	modelLabel = strings.TrimSpace(modelLabel)
	if modelLabel == "" {
		modelLabel = run.ModelAlias
	}
	data := map[string]string{
		"run_id": run.ID, "status": run.Status, "model_label": modelLabel,
		"tool_call_count": strconv.FormatInt(run.ToolCallCount, 10),
		"llm_call_count":  strconv.FormatInt(run.LLMCallCount, 10),
	}
	if run.DurationMS > 0 {
		data["duration_ms"] = strconv.FormatInt(run.DurationMS, 10)
	} else if durationMS, ok := runDurationMS(run); ok {
		data["duration_ms"] = strconv.FormatInt(durationMS, 10)
	}
	if run.ErrorCode != "" {
		data["error_code"] = run.ErrorCode
	}
	return data
}

func questionOptions(raw string) ([]convo.QuestionOption, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []any
	if err := json.Unmarshal([]byte(raw), &values); err != nil || len(values) > 8 {
		return nil, errors.New("invalid question options")
	}
	seen := make(map[string]struct{}, len(values))
	options := make([]convo.QuestionOption, 0, len(values))
	for i, value := range values {
		var label string
		switch typed := value.(type) {
		case string:
			label = strings.TrimSpace(typed)
		case map[string]any:
			label, _ = typed["label"].(string)
			label = strings.TrimSpace(label)
		default:
			return nil, errors.New("invalid question option")
		}
		if label == "" || len(label) > 1<<10 {
			return nil, errors.New("invalid question option")
		}
		if _, exists := seen[label]; exists {
			return nil, errors.New("duplicate question option")
		}
		seen[label] = struct{}{}
		options = append(options, convo.QuestionOption{
			ID: "option-" + strconv.Itoa(i+1), Label: label,
		})
	}
	return options, nil
}

func normalizeStructuredResultEvent(event agent.Event) (agent.Event, bool) {
	renderer := strings.TrimSpace(event.Data["renderer"])
	switch renderer {
	case "summary", "table", "list", "metrics":
	default:
		return agent.Event{}, false
	}
	version, err := strconv.Atoi(event.Data["renderer_version"])
	if err != nil || version != 1 {
		return agent.Event{}, false
	}
	contractHash := strings.TrimSpace(event.Data["contract_hash"])
	if !validSHA256Identifier(contractHash) {
		return agent.Event{}, false
	}
	raw := strings.TrimSpace(event.Data["data"])
	if raw == "" || len(raw) > 128<<10 {
		return agent.Event{}, false
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil ||
		!validStructuredValue(value, 0) ||
		!validStructuredRendererShape(renderer, value) {
		return agent.Event{}, false
	}
	data := cloneStringMap(event.Data)
	data["renderer"] = renderer
	data["renderer_version"] = "1"
	data["contract_hash"] = contractHash
	data["data"] = raw
	data["title"] = strings.TrimSpace(data["title"])
	if len(data["title"]) > 4<<10 {
		return agent.Event{}, false
	}
	return agent.Event{Kind: "structured_result_ready", Data: data}, true
}

func validSHA256Identifier(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validStructuredRendererShape(renderer string, value any) bool {
	root, ok := value.(map[string]any)
	if !ok {
		return false
	}
	switch renderer {
	case "table":
		columns, columnsOK := root["columns"].([]any)
		rows, rowsOK := root["rows"].([]any)
		return columnsOK && rowsOK && len(columns) <= 20 && len(rows) <= 200
	case "list":
		items, itemsOK := root["items"].([]any)
		return itemsOK && len(items) <= 200
	case "metrics":
		metrics := root["metrics"]
		if values, ok := metrics.([]any); ok {
			return len(values) <= 20
		}
		if values, ok := metrics.(map[string]any); ok {
			return len(values) <= 20
		}
		return len(root) <= 21
	default:
		return true
	}
}

func validStructuredValue(value any, depth int) bool {
	if depth > 12 {
		return false
	}
	switch typed := value.(type) {
	case nil, bool, float64:
		return true
	case string:
		return len(typed) <= 4<<10
	case []any:
		if len(typed) > 200 {
			return false
		}
		for _, item := range typed {
			if !validStructuredValue(item, depth+1) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(typed) > 200 {
			return false
		}
		for key, item := range typed {
			if len(key) > 256 || !validStructuredValue(item, depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
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

func hasMeaningfulAssistantOutput(parts []convo.Part) bool {
	for _, part := range parts {
		switch part.Type {
		case convo.PartText:
			if strings.TrimSpace(part.Text) != "" {
				return true
			}
		case convo.PartToolCall, convo.PartFile, convo.PartWikiFile,
			convo.PartSCMApproval, convo.PartPlan, convo.PartQuestion, convo.PartStructured:
			return true
		}
	}
	return false
}

func (r *liveRun) setPlanCandidate(content, workspaceRevision string) {
	r.mu.Lock()
	r.planContent = content
	r.workspaceRevision = workspaceRevision
	r.mu.Unlock()
}

func (r *liveRun) planCandidate() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.planContent, r.workspaceRevision
}

func (r *liveRun) setQuestionCandidate(question string, options []convo.QuestionOption) {
	r.mu.Lock()
	r.questionText = question
	r.questionOptions = append([]convo.QuestionOption(nil), options...)
	r.mu.Unlock()
}

func (r *liveRun) questionCandidate() (string, []convo.QuestionOption) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.questionText, append([]convo.QuestionOption(nil), r.questionOptions...)
}

func (r *liveRun) markRuntimeAccepted() {
	r.mu.Lock()
	r.runtimeAccepted = true
	r.mu.Unlock()
}

func (r *liveRun) wasRuntimeAccepted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimeAccepted
}

func (r *liveRun) setRuntimeErrorCode(code string) {
	if !validRunErrorCode(code) {
		return
	}
	r.mu.Lock()
	r.runtimeErrorCode = code
	r.mu.Unlock()
}

func (r *liveRun) getRuntimeErrorCode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimeErrorCode
}

func validRunErrorCode(code string) bool {
	if code == "" || len(code) > 80 {
		return false
	}
	for _, char := range code {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' {
			continue
		}
		return false
	}
	return true
}

func (a *API) executeLiveRun(live *liveRun) {
	defer live.cancel()
	defer close(live.done)
	var projectContext *agent.ProjectContext
	var scmToken string
	var projectBrokerCredential string
	var skillBrokerCredential string
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
					BaseRef: value.BaseRef, BaseSHA: value.BaseSHA, TaskBranch: value.BranchName,
					GitAuthorName: value.GitAuthorName, GitAuthorEmail: value.GitAuthorEmail,
					RepositoryProvider: value.RepositoryProvider,
					RepositoryFullName: value.RepositoryFullName,
					CredentialMode:     value.CredentialMode,
				}
				scmToken = token
				if live.request.InteractionMode != chatrun.InteractionModePlan &&
					value.RepositoryExternalID > 0 && a.projects.GitHubAgentWriteEnabled() {
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
	if a.skillBroker != nil && live.request.InteractionMode != chatrun.InteractionModePlan {
		var err error
		skillBrokerCredential, err = a.skillBroker.Issue(
			live.identity.TenantID, live.identity.UserID, live.request.SessionID, live.run.ID,
		)
		if err != nil {
			projectSetupErr = err
		}
	}
	larkCredential := agent.LarkRuntimeCredential{}
	if effectiveInteractionMode(live.request) != chatrun.InteractionModePlan {
		larkCredential = a.resolveLarkRuntimeCredential(
			live.ctx,
			live.identity,
			live.request.ChannelConnectorID,
			feishuCredentialWait,
		)
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
		InteractionMode:      agent.InteractionMode(effectiveInteractionMode(live.request)),
		RequireSessionResume: live.request.RequireSessionResume,
		Prompt:               agentPrompt(live.request), SandboxID: live.request.SandboxID,
		MaxTurns:            effectiveMaxTurns(live.request.MaxTurns, live.policy.agentMaxTurns),
		ModelRouteID:        effectiveModelRouteID(live.request),
		ReasoningEffort:     live.request.ReasoningEffort,
		AllowWorkspaceReset: live.request.AllowWorkspaceReset,
		MemoryContext:       memoryContext,
		TraceID:             live.run.ID, ParentSpanID: conversationRootSpan(live.traceCtx),
		Source:           live.run.Source,
		SandboxAuthToken: a.mintSandboxToken(live.identity), Attachments: attachments,
		WikiReferences: live.request.WikiReferences,
		SCMToken:       scmToken, ProjectBrokerCredential: projectBrokerCredential,
		SkillBrokerCredential: skillBrokerCredential,
		LarkCredential:        larkCredential,
		Project:               projectContext,
		Agent:                 runtimeAgentContext(live.request.AgentSnapshot),
		AgentKnowledge:        runtimeAgentKnowledgeContext(live.request),
	}
	coalescer := memoryEventCoalescer{run: live, window: a.runs.mergeWindow}
	var sawError bool
	var ttftMS int64
	var toolCalls int64
	var llmCalls int64
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
			if event.Kind == "result" {
				if count, parseErr := strconv.ParseInt(event.Data["num_turns"], 10, 64); parseErr == nil &&
					count > llmCalls {
					llmCalls = count
				}
			}
			if event.Kind == "run_accepted" {
				if live.request.QuestionID == "" {
					return nil
				}
				// Once the runtime has accepted the resumed user turn, retrying the
				// same answer could repeat side effects. Record that boundary before
				// the database call so finalization remains conservative if storage
				// becomes unavailable after the SDK accepts the turn.
				live.markRuntimeAccepted()
				question, acceptErr := a.runs.store.AcceptQuestionAnswer(
					live.ctx,
					live.run.ID,
					live.request.QuestionID,
					live.identity.UserID,
					time.Now().UTC(),
				)
				if acceptErr != nil {
					return fmt.Errorf("persist accepted question answer: %w", acceptErr)
				}
				answerJSON, _ := json.Marshal(question.Answer)
				live.publish(agent.Event{Kind: "question_status", Data: map[string]string{
					"id": question.ID, "status": question.Status, "answer": string(answerJSON),
				}})
				return nil
			}
			if event.Kind == "plan_ready" {
				live.setPlanCandidate(event.Data["content_markdown"], event.Data["workspace_revision"])
				return nil
			}
			if event.Kind == "question_required" {
				options, parseErr := questionOptions(event.Data["options"])
				if parseErr != nil || strings.TrimSpace(event.Data["question"]) == "" {
					sawError = true
					errorEvent := agent.Event{Kind: "error", Data: map[string]string{
						"code":  "QUESTION_OUTPUT_INVALID",
						"error": "Claude returned an invalid question.",
					}}
					_ = coalescer.Push(errorEvent)
					return nil
				}
				live.setQuestionCandidate(strings.TrimSpace(event.Data["question"]), options)
				return nil
			}
			if event.Kind == "structured_result_ready" {
				event.Data["run_id"] = live.run.ID
				if normalized, valid := normalizeStructuredResultEvent(event); valid {
					event = normalized
				} else {
					sawError = true
					event = agent.Event{Kind: "error", Data: map[string]string{
						"code":  "STRUCTURED_RESULT_INVALID",
						"error": "Claude returned an invalid structured result.",
					}}
				}
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
				if code := strings.TrimSpace(event.Data["code"]); code != "" {
					live.setRuntimeErrorCode(code)
					if strings.HasPrefix(code, "PROJECT_") {
						a.persistProjectBootstrapFailure(live, code)
					}
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
		if runtimeCode := live.getRuntimeErrorCode(); runtimeCode != "" {
			errorCode = runtimeCode
		}
	}
	questionText, questionOptions := live.questionCandidate()
	if status == chatrun.StatusSuccess && questionText != "" {
		status = chatrun.StatusWaitingInput
	}
	planContent, workspaceRevision := live.planCandidate()
	emptyResponse := status == chatrun.StatusSuccess && planContent == "" &&
		!hasMeaningfulAssistantOutput(live.parts())
	if emptyResponse {
		status, errorCode = chatrun.StatusError, emptyAgentResponse
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
	if emptyResponse {
		errorData := map[string]string{
			"error": "The agent completed without returning an answer.",
			"code":  errorCode,
		}
		live.apply(agent.Event{Kind: "error", Data: errorData})
		live.publish(agent.Event{Kind: "error", Data: errorData})
	} else if stepTimeout != nil && !cancelled {
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
	metadata["run_id"] = live.run.ID
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
	var planCandidate *chatrun.PlanCandidate
	if planContent != "" && status == chatrun.StatusSuccess &&
		live.request.InteractionMode == chatrun.InteractionModePlan {
		planCandidate = &chatrun.PlanCandidate{
			ID: uuid.NewString(), RuntimeID: live.request.RuntimeID,
			ModelRouteID: effectiveModelRouteID(live.request), ModelAlias: live.request.ModelAlias,
			ReasoningEffort: live.request.ReasoningEffort,
			ContentMarkdown: planContent, WorkspaceRevision: workspaceRevision,
		}
	}
	var questionCandidate *chatrun.QuestionCandidate
	if questionText != "" && status == chatrun.StatusWaitingInput {
		questionCandidate = &chatrun.QuestionCandidate{
			ID: uuid.NewString(), RuntimeID: live.request.RuntimeID,
			ModelRouteID: effectiveModelRouteID(live.request), ModelAlias: live.request.ModelAlias,
			ReasoningEffort: live.request.ReasoningEffort,
			SkillID:         live.request.SkillID, InteractionMode: effectiveInteractionMode(live.request),
			Text: questionText, Options: questionOptions,
		}
	}
	finalizeInput := chatrun.FinalizeInput{
		RunID: live.run.ID, UserID: live.run.UserID, Status: status, ErrorCode: errorCode,
		AssistantMessage: message, Reveal: live.request.DeferConversationVisibilityUntilDone,
		ConversationTitle: titleForConversation(live.request), CompletedAt: completedAt,
		ToolCallCount: toolCalls, LLMCallCount: llmCalls, PlanCandidate: planCandidate,
		QuestionCandidate: questionCandidate, RuntimeAccepted: live.wasRuntimeAccepted(),
	}
	finalizedResult, finalized := a.finalizeRun(finalizeInput)
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
	if !finalized {
		finalizedResult, finalized = a.recoverRunFinalization(finalizeInput)
	}
	if finalized {
		// Broker validity is persisted separately from this process-local execution
		// map, so other Gateway replicas observe revocation before local teardown.
		// Keep the live entry until the durable terminal state is confirmed.
		a.runs.mu.Lock()
		delete(a.runs.live, live.run.ID)
		a.runs.mu.Unlock()

		finalizedRun := finalizedResult.Run
		status, errorCode = finalizedRun.Status, finalizedRun.ErrorCode
		a.finishConversationRun(
			live.traceCtx, live.traceRun, status, errorCode,
			effectiveInteractionMode(live.request), ttftMS, toolCalls,
		)
		live.mu.Lock()
		finalizedRun.DurationMS = durationMS
		finalizedRun.ToolCallCount = toolCalls
		live.status = status
		live.run = finalizedRun
		live.mu.Unlock()
		if finalizedResult.SupersededPlanID != "" {
			live.publish(agent.Event{Kind: "plan_status", Data: map[string]string{
				"id": finalizedResult.SupersededPlanID, "status": chatrun.PlanStatusSuperseded,
			}})
		}
		if finalizedResult.Plan != nil {
			plan := finalizedResult.Plan
			if live.request.InteractionMode == chatrun.InteractionModePlan {
				planEvent := agent.Event{Kind: "plan_ready", Data: map[string]string{
					"id": plan.ID, "version": strconv.Itoa(plan.Version), "status": plan.Status,
					"content_markdown": plan.ContentMarkdown,
				}}
				live.apply(planEvent)
				live.publish(planEvent)
			} else {
				live.publish(agent.Event{Kind: "plan_status", Data: map[string]string{
					"id": plan.ID, "status": plan.Status,
				}})
			}
		}
		if finalizedResult.AnsweredQuestion != nil {
			question := finalizedResult.AnsweredQuestion
			answerJSON, _ := json.Marshal(question.Answer)
			live.publish(agent.Event{Kind: "question_status", Data: map[string]string{
				"id": question.ID, "status": question.Status, "answer": string(answerJSON),
			}})
		}
		if finalizedResult.RestoredQuestion != nil {
			question := finalizedResult.RestoredQuestion
			live.publish(agent.Event{Kind: "question_status", Data: map[string]string{
				"id": question.ID, "status": question.Status, "answer": "null",
			}})
		}
		if finalizedResult.Question != nil {
			question := finalizedResult.Question
			optionsJSON, _ := json.Marshal(question.Options)
			questionEvent := agent.Event{Kind: "question_ready", Data: map[string]string{
				"id": question.ID, "version": strconv.Itoa(question.Version),
				"status": question.Status, "question": question.Text,
				"options": string(optionsJSON),
			}}
			live.apply(questionEvent)
			live.publish(questionEvent)
		}
		summaryEvent := agent.Event{
			Kind: "run_summary", Data: runSummaryData(finalizedRun, live.request.ModelLabel),
		}
		live.apply(summaryEvent)
		live.publish(summaryEvent)
		live.publish(agent.Event{Kind: "done", Data: terminalRunData(finalizedRun)})
		if a.memory != nil && status == chatrun.StatusSuccess &&
			live.request.InteractionMode != chatrun.InteractionModePlan {
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

func agentPrompt(req chatRequest) string {
	prompt := strings.TrimSpace(req.Prompt)
	if strings.TrimSpace(req.RevisionOfPlanID) == "" {
		return prompt
	}
	return fmt.Sprintf(
		"Revise the current implementation plan using the user's requested changes below. Return a complete replacement plan for review.\n\nRequested changes:\n%s",
		prompt,
	)
}

func runtimeAgentContext(snapshot *agentprofile.Snapshot) *agent.AgentContext {
	if snapshot == nil {
		return nil
	}
	return &agent.AgentContext{
		ID: snapshot.ID, Version: snapshot.Version, Name: snapshot.Name,
		Instructions:    snapshot.Instructions,
		SkillCatalogIDs: append([]string(nil), snapshot.SkillIDs...),
	}
}

func runtimeAgentKnowledgeContext(req chatRequest) *agent.AgentKnowledgeContext {
	if req.AgentSnapshot == nil {
		return nil
	}
	return &agent.AgentKnowledgeContext{
		AgentID:  req.AgentSnapshot.ID,
		Revision: req.AgentKnowledgeRevision,
		Entries:  append([]agent.AgentKnowledgeEntry(nil), req.AgentKnowledgeEntries...),
	}
}

func (a *API) resolveLarkRuntimeCredential(
	ctx context.Context,
	identity auth.Identity,
	connectorID string,
	timeout time.Duration,
) agent.LarkRuntimeCredential {
	credential := agent.LarkRuntimeCredential{
		Status: feishuconnector.RuntimeCredentialMissing,
	}
	if a.feishu == nil {
		return credential
	}
	if strings.TrimSpace(connectorID) == "" {
		return credential
	}
	resolveCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolved, err := a.feishu.RuntimeCredentialByID(
		resolveCtx,
		feishuconnector.Identity{
			TenantID: identity.TenantID,
			UserID:   identity.UserID,
		},
		connectorID,
	)
	if err != nil {
		credential.Status = feishuconnector.RuntimeCredentialUnavailable
		a.log.Warn("Feishu runtime credential is temporarily unavailable")
		return credential
	}
	credential.Status = resolved.Status
	if resolved.Status == feishuconnector.RuntimeCredentialReady {
		credential.AppID = resolved.AppID
		credential.Brand = resolved.Brand
		credential.TenantAccessToken = resolved.TenantAccessToken
	}
	return credential
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

var (
	errInvalidChatAttachment = errors.New("invalid chat attachment")
	errChatAttachmentLimit   = errors.New("chat attachment limit exceeded")
)

func decodeChatAttachments(attachments []attachmentDTO, maxFileBytes, maxTotalBytes int64) error {
	var totalBytes int64
	for i := range attachments {
		content, err := base64.StdEncoding.DecodeString(attachments[i].ContentB64)
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidChatAttachment, err)
		}
		size := int64(len(content))
		if size > maxFileBytes || size > maxTotalBytes-totalBytes {
			return errChatAttachmentLimit
		}
		totalBytes += size
		attachments[i].Content = content
		attachments[i].ContentB64 = ""
	}
	return nil
}

func (a *API) prepareRunAttachments(ctx context.Context, req chatRequest) []agent.Attachment {
	attachments := make([]agent.Attachment, 0, len(req.Attachments))
	for _, dto := range req.Attachments {
		attachment := agent.Attachment{
			Filename: dto.Filename, Content: dto.Content, Mime: dto.Mime, Size: int64(len(dto.Content)),
		}
		if a.store != nil {
			key := objectKey(req.SessionID, attachment.Filename)
			if err := a.store.Put(ctx, key, dto.Content, attachment.Mime); err != nil {
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
	metadata["run_id"] = live.run.ID
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

func interruptedFinalization(input chatrun.FinalizeInput) chatrun.FinalizeInput {
	fallback := input
	fallback.Status = chatrun.StatusInterrupted
	fallback.ErrorCode = "FINALIZATION_FAILED"
	fallback.AssistantMessage = nil
	fallback.PlanCandidate = nil
	fallback.QuestionCandidate = nil
	fallback.Reveal = false
	return fallback
}

func (a *API) finalizeRun(input chatrun.FinalizeInput) (chatrun.FinalizeResult, bool) {
	var lastErr error
	for attempt := 1; attempt <= finalizeMaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), finalizeAttemptLimit)
		result, err := a.runs.store.Finalize(ctx, input)
		cancel()
		if err == nil {
			a.runs.databaseUnavailable.Store(false)
			return result, true
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
			return chatrun.FinalizeResult{}, false
		case <-timer.C:
		}
	}

	// A malformed assistant payload or a concurrently removed conversation must
	// not leave an immortal running row. Make one final, message-free transition
	// to interrupted. A total database outage may still reject this write; in
	// that case readiness stays failed and executeLiveRun keeps the local run
	// until recoverRunFinalization confirms the durable terminal state.
	fallback := interruptedFinalization(input)
	ctx, cancel := context.WithTimeout(context.Background(), finalizeAttemptLimit)
	result, fallbackErr := a.runs.store.Finalize(ctx, fallback)
	cancel()
	if fallbackErr == nil {
		a.runs.databaseUnavailable.Store(false)
		a.log.Warn("chat run output could not be finalized; saved interrupted terminal state: " + lastErr.Error())
		return result, true
	}
	a.log.Warn(fmt.Sprintf("chat run finalization abandoned after %d attempts: %v; fallback: %v",
		finalizeMaxAttempts, lastErr, fallbackErr))
	return chatrun.FinalizeResult{}, false
}

func (a *API) recoverRunFinalization(input chatrun.FinalizeInput) (chatrun.FinalizeResult, bool) {
	fallback := interruptedFinalization(input)
	delay := a.runs.finalizeRetry
	if delay <= 0 {
		delay = defaultFinalizeRetry
	}
	if delay > finalizeRecoveryMax {
		delay = finalizeRecoveryMax
	}
	for {
		timer := time.NewTimer(delay)
		select {
		case <-a.runs.stop:
			timer.Stop()
			return chatrun.FinalizeResult{}, false
		case <-timer.C:
		}

		ctx, cancel := context.WithTimeout(context.Background(), finalizeAttemptLimit)
		result, err := a.runs.store.Finalize(ctx, input)
		cancel()
		if err == nil {
			a.runs.databaseUnavailable.Store(false)
			a.log.Warn("chat run finalization recovered")
			return result, true
		}

		ctx, cancel = context.WithTimeout(context.Background(), finalizeAttemptLimit)
		result, fallbackErr := a.runs.store.Finalize(ctx, fallback)
		cancel()
		if fallbackErr == nil {
			a.runs.databaseUnavailable.Store(false)
			a.log.Warn("chat run finalization recovered with interrupted terminal state: " + err.Error())
			return result, true
		}
		a.runs.databaseUnavailable.Store(true)
		a.log.Warn(fmt.Sprintf("chat run finalization recovery failed; retrying: %v; fallback: %v",
			err, fallbackErr))
		if delay < finalizeRecoveryMax {
			delay *= 2
			if delay > finalizeRecoveryMax {
				delay = finalizeRecoveryMax
			}
		}
	}
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
	if chatrun.IsTerminal(run.Status) {
		message := convo.Message{Parts: parts}
		upsertRunSummaryPart(&message, run)
		parts = message.Parts
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
