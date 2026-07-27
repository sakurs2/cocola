package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

const (
	defaultReconcileInterval = 10 * time.Second
	defaultLeaseTTL          = 60 * time.Second
	defaultLeaseRenew        = 20 * time.Second
	inboxLeaseTTL            = 10 * time.Minute
	maxPendingMessages       = 5
	maxChatAttachments       = 8
	maxChatAttachmentBytes   = int64(32 << 20)
	maxReceiveCallback       = 2500 * time.Millisecond
	reactionRequestTimeout   = 3 * time.Second
	reactionAttemptBackoff   = 10 * time.Minute
	permissionNoticeInterval = 24 * time.Hour
	maxHistoryConversations  = 10
)

type ManagerConfig struct {
	Store             Store
	Service           *Service
	Factory           ChannelFactory
	AccountAuthorizer *AccountAuthorizer
	ChatClient        *ChatClient
	Logger            logger.Logger
	OwnerID           string
	ReconcileInterval time.Duration
	LeaseTTL          time.Duration
	LeaseRenew        time.Duration
	MediaHTTPClient   *http.Client
	PublicOrigins     string
}

type Manager struct {
	store       Store
	service     *Service
	factory     ChannelFactory
	accounts    *AccountAuthorizer
	chat        *ChatClient
	log         logger.Logger
	ownerID     string
	reconcile   time.Duration
	leaseTTL    time.Duration
	leaseRenew  time.Duration
	mediaHTTP   *http.Client
	settingsURL string
	now         func() time.Time

	mu      sync.Mutex
	runners map[string]*connectorRunner
	wg      sync.WaitGroup
}

func NewManager(config ManagerConfig) (*Manager, error) {
	switch {
	case config.Store == nil:
		return nil, errors.New("Feishu manager store is required")
	case config.Service == nil:
		return nil, errors.New("Feishu manager service is required")
	case config.Factory == nil:
		return nil, errors.New("Feishu channel factory is required")
	case config.AccountAuthorizer == nil:
		return nil, errors.New("Feishu account authorizer is required")
	case config.ChatClient == nil:
		return nil, errors.New("Feishu chat client is required")
	case config.Logger == nil:
		return nil, errors.New("Feishu manager logger is required")
	}
	if config.OwnerID == "" {
		config.OwnerID = uuid.NewString()
	}
	if config.ReconcileInterval <= 0 {
		config.ReconcileInterval = defaultReconcileInterval
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.LeaseRenew <= 0 {
		config.LeaseRenew = defaultLeaseRenew
	}
	config.Service.WithTokenHTTPClient(config.MediaHTTPClient)
	return &Manager{
		store: config.Store, service: config.Service, factory: config.Factory,
		accounts: config.AccountAuthorizer, chat: config.ChatClient,
		log: config.Logger, ownerID: config.OwnerID,
		reconcile: config.ReconcileInterval, leaseTTL: config.LeaseTTL,
		leaseRenew:  config.LeaseRenew,
		mediaHTTP:   config.MediaHTTPClient,
		settingsURL: ConnectorSettingsURL(config.PublicOrigins),
		now:         func() time.Time { return time.Now().UTC() },
		runners:     make(map[string]*connectorRunner),
	}, nil
}

func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(m.reconcile)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	m.reconcileConnectors(ctx)
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			m.wg.Wait()
			return
		case <-ticker.C:
			m.reconcileConnectors(ctx)
		case <-m.service.Changes():
			m.reconcileConnectors(ctx)
		case <-cleanup.C:
			cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := m.store.CleanupInbox(cleanupCtx, m.now().Add(-7*24*time.Hour)); err != nil {
				m.log.Warn("Feishu inbox cleanup failed: " + err.Error())
			}
			cancel()
		}
	}
}

func (m *Manager) reconcileConnectors(ctx context.Context) {
	m.stopInvalidRunners(ctx)
	now := m.now()
	connectors, err := m.store.ClaimConnectors(
		ctx,
		m.ownerID,
		now,
		now.Add(m.leaseTTL),
		100,
	)
	if err != nil {
		m.log.Warn("Feishu connector reconciliation failed: " + err.Error())
		return
	}
	for _, connector := range connectors {
		m.mu.Lock()
		_, running := m.runners[connector.ID]
		if !running {
			runnerCtx, cancel := context.WithCancel(ctx)
			runner := &connectorRunner{
				manager: m, connector: connector, ctx: runnerCtx, cancel: cancel,
				wake: make(chan struct{}, 1),
			}
			m.runners[connector.ID] = runner
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				runner.run()
			}()
		}
		m.mu.Unlock()
	}
}

func (m *Manager) stopInvalidRunners(ctx context.Context) {
	connectors, err := m.store.OwnedConnectors(ctx, m.ownerID)
	if err != nil {
		m.log.Warn("Feishu owned connector lookup failed: " + err.Error())
		return
	}
	owned := make(map[string]Connector, len(connectors))
	for _, connector := range connectors {
		owned[connector.ID] = connector
	}
	m.mu.Lock()
	runners := make([]*connectorRunner, 0, len(m.runners))
	for _, runner := range m.runners {
		runners = append(runners, runner)
	}
	m.mu.Unlock()
	for _, runner := range runners {
		connector, ok := owned[runner.connector.ID]
		if !ok || !connector.DesiredEnabled ||
			connector.Version != runner.connector.Version {
			runner.cancel()
		}
	}
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, runner := range m.runners {
		runner.cancel()
	}
}

func (m *Manager) removeRunner(id string, runner *connectorRunner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runners[id] == runner {
		delete(m.runners, id)
	}
}

type activeRun struct {
	cancel  context.CancelFunc
	runID   string
	stopped bool
}

type connectorRunner struct {
	manager   *Manager
	connector Connector
	channel   RuntimeChannel
	ctx       context.Context
	cancel    context.CancelFunc
	wake      chan struct{}

	activeMu sync.Mutex
	active   *activeRun

	failureMu sync.Mutex
	failures  int

	reactionMu             sync.Mutex
	reactionBackoffUntil   time.Time
	lastPermissionNoticeAt time.Time

	historyMu         sync.Mutex
	historySelections map[string][]string
}

func (r *connectorRunner) run() {
	defer r.manager.removeRunner(r.connector.ID, r)
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = r.manager.store.ReleaseConnectorLease(
			releaseCtx,
			r.connector.ID,
			r.manager.ownerID,
			r.manager.now(),
		)
		cancel()
	}()

	appSecret, err := r.manager.service.AppSecret(r.connector)
	if err != nil {
		r.markError("credential_decrypt_failed")
		return
	}
	r.channel, err = r.manager.factory.New(r.connector, appSecret)
	if err != nil {
		r.markError("channel_configuration_failed")
		return
	}
	ready := make(chan struct{})
	var readyOnce sync.Once
	r.channel.OnMessage(r.onMessage)
	r.channel.OnReady(func(identity BotIdentity) {
		if !r.onReady(identity) {
			r.cancel()
			return
		}
		readyOnce.Do(func() { close(ready) })
	})
	r.channel.OnError(func(channelErr error) {
		if r.ctx.Err() != nil {
			return
		}
		r.manager.log.Warn("Feishu channel error: " + boundedError(channelErr))
		r.failureMu.Lock()
		r.failures++
		failures := r.failures
		r.failureMu.Unlock()
		if failures >= 5 {
			r.markError("connection_failed")
			r.cancel()
			return
		}
		_ = r.manager.store.UpdateConnectorState(
			context.Background(),
			r.connector.ID,
			r.manager.ownerID,
			StatusConnecting,
			"",
			"",
			"connection_error",
			nil,
			r.manager.now(),
		)
	})

	started := make(chan error, 1)
	go func() {
		started <- r.channel.Start(r.ctx)
		close(started)
	}()
	leaseTicker := time.NewTicker(r.manager.leaseRenew)
	defer leaseTicker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = r.channel.Stop(stopCtx)
			select {
			case <-started:
			case <-stopCtx.Done():
				r.manager.log.Warn("Feishu channel did not stop before timeout")
			}
			cancel()
			return
		case startErr := <-started:
			if startErr != nil && !errors.Is(startErr, context.Canceled) {
				r.manager.log.Warn("Feishu channel start failed: " + boundedError(startErr))
				r.markError("connection_failed")
			}
			r.cancel()
		case <-leaseTicker.C:
			if !r.renewLease() {
				return
			}
		case <-ready:
			goto channelReady
		}
	}

channelReady:
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		r.workerLoop()
	}()
	for {
		select {
		case <-r.ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = r.channel.Stop(stopCtx)
			select {
			case <-started:
			case <-stopCtx.Done():
				r.manager.log.Warn("Feishu channel did not stop before timeout")
			}
			cancel()
			<-workerDone
			return
		case startErr := <-started:
			if startErr != nil && !errors.Is(startErr, context.Canceled) {
				r.manager.log.Warn("Feishu channel start failed: " + boundedError(startErr))
				r.markError("connection_failed")
			}
			r.cancel()
		case <-leaseTicker.C:
			if !r.renewLease() {
				return
			}
		}
	}
}

func (r *connectorRunner) renewLease() bool {
	now := r.manager.now()
	if err := r.manager.store.RenewConnectorLease(
		r.ctx,
		r.connector.ID,
		r.manager.ownerID,
		now,
		now.Add(r.manager.leaseTTL),
	); err != nil {
		r.cancel()
		return false
	}
	return true
}

func (r *connectorRunner) onReady(identity BotIdentity) bool {
	if r.ctx.Err() != nil {
		return false
	}
	r.failureMu.Lock()
	r.failures = 0
	r.failureMu.Unlock()
	status := StatusReady
	errorCode := ""
	if r.connector.OwnerOpenID == "" {
		status = StatusAwaitingBind
	} else if identity.OpenID == "" || identity.ActivateStatus != 2 {
		status = StatusActionRequired
		errorCode = "bot_not_active"
	}
	now := r.manager.now()
	connectedAt := &now
	stateCtx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	if err := r.manager.store.UpdateConnectorState(
		stateCtx,
		r.connector.ID,
		r.manager.ownerID,
		status,
		identity.OpenID,
		identity.Name,
		errorCode,
		connectedAt,
		now,
	); err != nil {
		if !errors.Is(err, ErrLeaseLost) {
			r.manager.log.Warn("Feishu ready state update failed: " + err.Error())
		}
		return false
	}
	return true
}

func (r *connectorRunner) markError(code string) {
	_ = r.manager.store.UpdateConnectorState(
		context.Background(),
		r.connector.ID,
		r.manager.ownerID,
		StatusError,
		"",
		"",
		code,
		nil,
		r.manager.now(),
	)
}

func (r *connectorRunner) onMessage(ctx context.Context, message RuntimeMessage) error {
	if message.ChatType != "p2p" ||
		message.MessageID == "" ||
		message.ChatID == "" ||
		message.SenderOpenID == "" {
		return nil
	}
	if r.connector.BotOpenID != "" && message.SenderOpenID == r.connector.BotOpenID {
		return nil
	}
	callbackCtx, cancel := context.WithTimeout(ctx, maxReceiveCallback)
	defer cancel()
	if r.connector.OwnerOpenID == "" {
		return r.handleBinding(callbackCtx, message)
	}
	if message.SenderOpenID != r.connector.OwnerOpenID {
		return nil
	}
	command := strings.TrimSpace(message.Text)
	if command == "/stop" {
		r.stopActive()
	}
	if (command == "/new" || isSwitchCommand(command)) && r.hasActiveRun() {
		r.sendAsync(message.ChatID, message.MessageID, "当前 Agent 仍在运行，请先发送 `/stop`。")
		return nil
	}
	eventID := strings.TrimSpace(message.EventID)
	if eventID == "" {
		eventID = "message:" + message.MessageID
	}
	priority := 0
	if command == "/stop" {
		priority = 100
	}
	now := r.manager.now()
	created, err := r.manager.store.EnqueueInbox(callbackCtx, InboxItem{
		ID: uuid.NewString(), ConnectorID: r.connector.ID,
		EventID: eventID, ExternalMessageID: message.MessageID,
		ExternalChatID: message.ChatID, Priority: priority,
		Payload: InboxPayload{
			EventID: eventID, MessageID: message.MessageID,
			ChatID: message.ChatID, ChatType: message.ChatType,
			SenderOpenID: message.SenderOpenID, Text: message.Text,
			ContentType: message.ContentType, Resources: message.Resources,
			CreateTimeMS: message.CreateTimeMS,
		},
		Status: InboxPending, NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}, maxPendingMessages)
	switch {
	case errors.Is(err, ErrQueueFull):
		r.sendAsync(message.ChatID, message.MessageID, "当前 Agent 正忙，请稍后重试。")
		return nil
	case err != nil:
		r.cancel()
		return err
	case created:
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

func (r *connectorRunner) handleBinding(
	ctx context.Context,
	message RuntimeMessage,
) error {
	fields := strings.Fields(strings.TrimSpace(message.Text))
	if len(fields) != 2 || fields[0] != "/bind" {
		r.sendAsync(
			message.ChatID,
			message.MessageID,
			"此机器人正在等待绑定。请从 Cocola Connector 页面复制 `/bind <code>` 后发送。",
		)
		return nil
	}
	_, err := r.manager.service.BindOwner(
		ctx,
		r.connector.ID,
		fields[1],
		message.SenderOpenID,
	)
	if err != nil {
		r.sendAsync(message.ChatID, message.MessageID, "绑定码无效或已过期，请在 Cocola 重新生成。")
		return nil
	}
	sendCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_ = r.channel.SendMarkdown(
		sendCtx,
		message.ChatID,
		message.MessageID,
		"绑定成功，现在可以直接向我发送消息。",
	)
	cancel()
	r.manager.service.notify()
	return nil
}

func (r *connectorRunner) sendAsync(chatID, replyMessageID, text string) {
	go func() {
		ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
		defer cancel()
		if err := r.channel.SendMarkdown(ctx, chatID, replyMessageID, text); err != nil {
			r.manager.log.Warn("Feishu outbound message failed: " + boundedError(err))
		}
	}()
}

func (r *connectorRunner) workerLoop() {
	downloader := NewBoundedDownloader(r.connector, r.manager.service, r.manager.mediaHTTP)
	for {
		r.drainInbox(downloader)
		nextAttempt, err := r.manager.store.NextInboxAttempt(r.ctx, r.connector.ID)
		var timer *time.Timer
		var timerC <-chan time.Time
		if err == nil {
			delay := time.Until(nextAttempt)
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			timerC = timer.C
		} else if !errors.Is(err, ErrNotFound) && r.ctx.Err() == nil {
			r.manager.log.Warn("Feishu inbox schedule lookup failed: " + err.Error())
			timer = time.NewTimer(time.Second)
			timerC = timer.C
		}
		select {
		case <-r.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-r.wake:
			if timer != nil {
				timer.Stop()
			}
		case <-timerC:
		}
	}
}

func (r *connectorRunner) drainInbox(downloader *BoundedDownloader) {
	for {
		if r.ctx.Err() != nil {
			return
		}
		now := r.manager.now()
		item, err := r.manager.store.ClaimNextInbox(
			r.ctx,
			r.connector.ID,
			r.manager.ownerID,
			now,
			now.Add(inboxLeaseTTL),
		)
		if errors.Is(err, ErrNotFound) {
			return
		}
		if err != nil {
			r.manager.log.Warn("Feishu inbox claim failed: " + err.Error())
			return
		}
		result := r.processInbox(item, downloader)
		now = r.manager.now()
		if result.RetryError != nil {
			if err := r.manager.store.RetryInbox(
				r.ctx,
				item.ID,
				r.manager.ownerID,
				item.Attempts,
				now.Add(retryDelay(item.Attempts)),
				result.ErrorCode,
				now,
			); err != nil {
				r.manager.log.Warn("Feishu inbox retry update failed: " + err.Error())
				return
			}
			continue
		}
		if result.Status == "" {
			result.Status = InboxDone
		}
		if err := r.manager.store.FinishInbox(
			r.ctx,
			item.ID,
			r.manager.ownerID,
			result.Status,
			now,
		); err != nil {
			r.manager.log.Warn("Feishu inbox completion failed: " + err.Error())
			return
		}
	}
}

type inboxResult struct {
	Status     string
	ErrorCode  string
	RetryError error
}

func (r *connectorRunner) processInbox(
	item InboxItem,
	downloader *BoundedDownloader,
) inboxResult {
	reactionID := r.beginProcessingReaction(item)
	defer r.finishProcessingReaction(item, reactionID)

	token, _, err := r.manager.accounts.Authorize(r.ctx, r.connector)
	if errors.Is(err, ErrAccountDisabled) {
		if sendErr := r.channel.SendMarkdown(
			r.ctx,
			item.ExternalChatID,
			item.ExternalMessageID,
			"Cocola 账号已停用或不存在，请返回 Cocola 检查账号状态。",
		); sendErr != nil {
			return retryResult("send_failed", sendErr)
		}
		return inboxResult{Status: InboxRejected, ErrorCode: "account_disabled"}
	}
	if err != nil {
		return retryResult("account_unavailable", err)
	}

	session, err := r.session(item.ExternalChatID)
	if err != nil {
		return retryResult("session_unavailable", err)
	}
	command := strings.TrimSpace(item.Payload.Text)
	switch command {
	case "/help":
		return r.sendDone(item, helpMessage())
	case "/history":
		return r.showHistory(item, token, session)
	case "/status":
		status := "idle"
		if r.hasActiveRun() {
			status = "running"
		} else if session.PendingQuestionID != "" {
			status = "waiting for your answer"
		}
		return r.sendDone(item, "Cocola Connector is ready. Agent status: **"+status+"**.")
	case "/new":
		if session.PendingQuestionID != "" {
			if err := r.manager.chat.CancelQuestion(r.ctx, token, session); err != nil {
				return retryResult("question_cancel_failed", err)
			}
		}
		session.ConversationID = uuid.NewString()
		clearPendingQuestion(&session)
		session.UpdatedAt = r.manager.now()
		if err := r.manager.store.UpsertSession(r.ctx, session); err != nil {
			return retryResult("session_unavailable", err)
		}
		return r.sendDone(item, "已创建新的 Cocola 对话。")
	case "/stop":
		r.stopActive()
		active, activeErr := r.manager.chat.ActiveRun(
			r.ctx,
			token,
			session.ConversationID,
		)
		if activeErr == nil {
			if err := r.manager.chat.CancelRun(r.ctx, token, active.ID); err != nil {
				return retryResult("run_cancel_failed", err)
			}
		} else if !chatNotFound(activeErr) {
			return retryResult("run_lookup_failed", activeErr)
		}
		if session.PendingQuestionID != "" {
			if err := r.manager.chat.CancelQuestion(r.ctx, token, session); err != nil {
				return retryResult("question_cancel_failed", err)
			}
			clearPendingQuestion(&session)
			session.UpdatedAt = r.manager.now()
			if err := r.manager.store.UpsertSession(r.ctx, session); err != nil {
				return retryResult("session_unavailable", err)
			}
		}
		return r.sendDone(item, "已停止当前任务。")
	}
	if index, matched, parseErr := parseSwitchCommand(command); matched {
		if parseErr != nil {
			return r.sendDone(item, "用法：`/switch 编号`，例如 `/switch 2`。")
		}
		return r.switchConversation(item, token, session, index)
	}

	if session.PendingQuestionID != "" {
		if len(item.Payload.Resources) > 0 {
			return r.sendRejected(
				item,
				"question_requires_text",
				"当前 Agent 正在等待回答，请先发送文字或选项编号。",
			)
		}
		if command == "" {
			return r.sendRejected(item, "empty_answer", "回答不能为空。")
		}
		return r.answerQuestion(item, token, session)
	}

	connector, err := r.manager.store.GetConnectorByID(r.ctx, r.connector.ID)
	if err != nil {
		return retryResult("connector_unavailable", err)
	}
	attachments, attachmentErr := r.downloadAttachments(item, downloader)
	switch {
	case errors.Is(attachmentErr, ErrUnsupportedMedia):
		return r.sendRejected(
			item,
			"unsupported_media",
			"暂不支持语音、视频或贴纸，请发送文字、图片或普通文件。",
		)
	case errors.Is(attachmentErr, ErrAttachmentTooLarge):
		return r.sendRejected(
			item,
			"attachment_too_large",
			"附件过大：单轮最多 8 个文件，合计不超过 32 MiB。",
		)
	case attachmentErr != nil:
		return retryResult("attachment_download_failed", attachmentErr)
	}
	if strings.TrimSpace(item.Payload.Text) == "" && len(attachments) == 0 {
		return r.sendRejected(item, "empty_message", "消息中没有可处理的文字或附件。")
	}
	return r.startChat(item, token, session, connector, attachments)
}

func (r *connectorRunner) beginProcessingReaction(item InboxItem) string {
	if strings.TrimSpace(item.ExternalMessageID) == "" {
		return ""
	}
	now := r.manager.now()
	r.reactionMu.Lock()
	backoffUntil := r.reactionBackoffUntil
	r.reactionMu.Unlock()
	if now.Before(backoffUntil) {
		return ""
	}

	ctx, cancel := context.WithTimeout(r.ctx, reactionRequestTimeout)
	reactionID, err := r.channel.AddReaction(
		ctx,
		item.ExternalMessageID,
		processingReactionEmoji,
	)
	cancel()
	if err == nil {
		r.reactionMu.Lock()
		r.reactionBackoffUntil = time.Time{}
		r.reactionMu.Unlock()
		return reactionID
	}
	var permissionErr *PermissionError
	if errors.As(err, &permissionErr) {
		r.onReactionPermissionMissing(item, permissionErr)
		return ""
	}
	r.logReactionFailure("Feishu processing reaction failed: ", err)
	return ""
}

func (r *connectorRunner) finishProcessingReaction(
	item InboxItem,
	reactionID string,
) {
	if reactionID == "" || strings.TrimSpace(item.ExternalMessageID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reactionRequestTimeout)
	err := r.channel.DeleteReaction(ctx, item.ExternalMessageID, reactionID)
	cancel()
	if err == nil {
		return
	}
	var permissionErr *PermissionError
	if errors.As(err, &permissionErr) {
		r.onReactionPermissionMissing(item, permissionErr)
		return
	}
	r.logReactionFailure("Feishu processing reaction cleanup failed: ", err)
}

func (r *connectorRunner) onReactionPermissionMissing(
	item InboxItem,
	permissionErr *PermissionError,
) {
	now := r.manager.now()
	r.reactionMu.Lock()
	r.reactionBackoffUntil = now.Add(reactionAttemptBackoff)
	shouldNotify := r.lastPermissionNoticeAt.IsZero() ||
		now.Sub(r.lastPermissionNoticeAt) >= permissionNoticeInterval
	if shouldNotify {
		r.lastPermissionNoticeAt = now
	}
	r.reactionMu.Unlock()
	if !shouldNotify {
		return
	}

	targetURL := permissionErr.ConsoleURL
	if !trustedPermissionURL(targetURL) {
		targetURL = ""
	}
	if targetURL == "" {
		targetURL = r.manager.settingsURL
	}
	if targetURL == "" {
		if r.connector.Domain == DomainLark {
			targetURL = "https://open.larksuite.com/app"
		} else {
			targetURL = "https://open.feishu.cn/app"
		}
	}
	r.sendAsync(
		item.ExternalChatID,
		item.ExternalMessageID,
		"当前机器人缺少“发送和删除消息表情”权限（`"+
			reactionPermissionScope+
			"`），不影响正常对话。\n"+
			"请使用应用开发者或管理员账号打开以下权限申请入口；"+
			"开通后可能还需要发布应用并等待管理员审批：\n"+
			targetURL,
	)
}

func (r *connectorRunner) logReactionFailure(prefix string, err error) {
	if r.manager == nil || r.manager.log == nil {
		return
	}
	r.manager.log.Warn(prefix + boundedError(err))
}

func (r *connectorRunner) session(chatID string) (Session, error) {
	session, err := r.manager.store.GetSession(r.ctx, r.connector.ID, chatID)
	if err == nil {
		return session, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Session{}, err
	}
	now := r.manager.now()
	session = Session{
		ConnectorID: r.connector.ID, ExternalChatID: chatID,
		ConversationID: uuid.NewString(), UpdatedAt: now,
	}
	if err := r.manager.store.UpsertSession(r.ctx, session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (r *connectorRunner) downloadAttachments(
	item InboxItem,
	downloader *BoundedDownloader,
) ([]ChatAttachment, error) {
	if len(item.Payload.Resources) > maxChatAttachments {
		return nil, ErrAttachmentTooLarge
	}
	attachments := make([]ChatAttachment, 0, len(item.Payload.Resources))
	var total int64
	for _, resource := range item.Payload.Resources {
		if resource.Type != "image" && resource.Type != "file" {
			return nil, ErrUnsupportedMedia
		}
		downloaded, err := downloader.Download(
			r.ctx,
			item.Payload.MessageID,
			resource,
			maxChatAttachmentBytes-total,
		)
		if err != nil {
			return nil, err
		}
		total += int64(len(downloaded.Content))
		attachments = append(attachments, ChatAttachment{
			Filename: downloaded.Filename,
			MIME:     downloaded.MIME,
			Content:  downloaded.Content,
		})
	}
	return attachments, nil
}

func (r *connectorRunner) startChat(
	item InboxItem,
	token string,
	session Session,
	connector Connector,
	attachments []ChatAttachment,
) inboxResult {
	turn := ChatTurn{
		Prompt: item.Payload.Text, ConversationID: session.ConversationID,
		ConversationTitle: conversationTitle(item.Payload.Text),
		AgentID:           connector.AgentID,
		ClientRequestID: DeterministicRequestID(
			r.connector.ID,
			item.EventID,
			"chat",
		),
		Attachments: attachments,
	}
	return r.consumeAgentStream(item, token, &session, func(
		ctx context.Context,
		onStarted func(string),
		onEvent func(ChatEvent) error,
	) error {
		return r.manager.chat.Chat(ctx, token, turn, onStarted, onEvent)
	})
}

func (r *connectorRunner) answerQuestion(
	item InboxItem,
	token string,
	session Session,
) inboxResult {
	answer := questionAnswer(item.Payload.Text, session.PendingOptions)
	requestID := DeterministicRequestID(r.connector.ID, item.EventID, "question")
	return r.consumeAgentStream(item, token, &session, func(
		ctx context.Context,
		onStarted func(string),
		onEvent func(ChatEvent) error,
	) error {
		return r.manager.chat.AnswerQuestion(
			ctx,
			token,
			session,
			answer,
			requestID,
			onStarted,
			onEvent,
		)
	})
}

func (r *connectorRunner) showHistory(
	item InboxItem,
	token string,
	session Session,
) inboxResult {
	conversations, defaultRuntimeID, err := r.historyConversations(token)
	if err != nil {
		return retryResult("history_unavailable", err)
	}
	available := switchableConversations(
		conversations,
		defaultRuntimeID,
		r.connector.AgentID,
	)
	r.saveHistorySelection(item.ExternalChatID, available)
	return r.sendDone(item, historyMessage(available, session.ConversationID))
}

func (r *connectorRunner) switchConversation(
	item InboxItem,
	token string,
	session Session,
	index int,
) inboxResult {
	if session.PendingQuestionID != "" {
		return r.sendDone(item, "当前 Agent 正在等待回答，请先发送 `/stop`。")
	}
	selection, ok := r.historySelection(item.ExternalChatID)
	if !ok {
		return r.sendDone(item, "请先发送 `/history` 查看可切换的对话。")
	}
	if index < 1 || index > len(selection) {
		return r.sendDone(item, "对话编号无效，请重新发送 `/history` 查看可切换的对话。")
	}
	conversations, defaultRuntimeID, err := r.historyConversations(token)
	if err != nil {
		return retryResult("history_unavailable", err)
	}
	targetID := selection[index-1]
	var target convo.Conversation
	for _, conversation := range switchableConversations(
		conversations,
		defaultRuntimeID,
		r.connector.AgentID,
	) {
		if conversation.ID == targetID {
			target = conversation
			break
		}
	}
	if target.ID == "" {
		return r.sendDone(item, "对话列表已变化，请重新发送 `/history` 后再切换。")
	}
	if target.ID == session.ConversationID {
		return r.sendDone(item, "当前已经是这个对话："+historyTitle(target.Title))
	}
	session.ConversationID = target.ID
	session.UpdatedAt = r.manager.now()
	if err := r.manager.store.UpsertSession(r.ctx, session); err != nil {
		return retryResult("session_unavailable", err)
	}
	return r.sendDone(item, "已切换到对话："+historyTitle(target.Title))
}

func (r *connectorRunner) historyConversations(
	token string,
) ([]convo.Conversation, string, error) {
	conversations, err := r.manager.chat.ListConversations(r.ctx, token)
	if err != nil {
		return nil, "", err
	}
	defaultRuntimeID, err := r.manager.chat.DefaultRuntimeID(r.ctx, token)
	if err != nil {
		return nil, "", err
	}
	return conversations, defaultRuntimeID, nil
}

func (r *connectorRunner) saveHistorySelection(
	chatID string,
	conversations []convo.Conversation,
) {
	selection := make([]string, 0, len(conversations))
	for _, conversation := range conversations {
		selection = append(selection, conversation.ID)
	}
	r.historyMu.Lock()
	defer r.historyMu.Unlock()
	if r.historySelections == nil {
		r.historySelections = make(map[string][]string)
	}
	r.historySelections[chatID] = selection
}

func (r *connectorRunner) historySelection(chatID string) ([]string, bool) {
	r.historyMu.Lock()
	defer r.historyMu.Unlock()
	selection, ok := r.historySelections[chatID]
	return append([]string(nil), selection...), ok
}

func switchableConversations(
	conversations []convo.Conversation,
	defaultRuntimeID string,
	agentID string,
) []convo.Conversation {
	available := make([]convo.Conversation, 0, min(len(conversations), maxHistoryConversations))
	for _, conversation := range conversations {
		if (conversation.ChatType != "" && conversation.ChatType != "chat") ||
			conversation.ProjectID != "" ||
			conversation.RuntimeID != defaultRuntimeID ||
			conversation.AgentID != agentID {
			continue
		}
		available = append(available, conversation)
		if len(available) == maxHistoryConversations {
			break
		}
	}
	return available
}

func historyMessage(conversations []convo.Conversation, currentID string) string {
	if len(conversations) == 0 {
		return "还没有可切换的历史对话。"
	}
	var builder strings.Builder
	builder.WriteString("最近对话：\n\n")
	for index, conversation := range conversations {
		builder.WriteString(strconv.Itoa(index + 1))
		builder.WriteString(". ")
		builder.WriteString(historyTitle(conversation.Title))
		if conversation.ID == currentID {
			builder.WriteString(" · 当前")
		}
		builder.WriteByte('\n')
	}
	builder.WriteString("\n发送 `/switch 编号` 切换，例如 `/switch 2`。")
	return builder.String()
}

func historyTitle(title string) string {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if title == "" {
		return "未命名对话"
	}
	runes := []rune(title)
	if len(runes) > 48 {
		return string(runes[:48]) + "…"
	}
	return title
}

func isSwitchCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	return len(fields) > 0 && fields[0] == "/switch"
}

func parseSwitchCommand(command string) (int, bool, error) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 || fields[0] != "/switch" {
		return 0, false, nil
	}
	if len(fields) != 2 {
		return 0, true, ErrInvalid
	}
	index, err := strconv.Atoi(fields[1])
	if err != nil || index <= 0 {
		return 0, true, ErrInvalid
	}
	return index, true, nil
}

func (r *connectorRunner) consumeAgentStream(
	item InboxItem,
	token string,
	session *Session,
	call func(context.Context, func(string), func(ChatEvent) error) error,
) inboxResult {
	runCtx, cancel := context.WithCancel(r.ctx)
	active := &activeRun{cancel: cancel}
	r.activeMu.Lock()
	r.active = active
	r.activeMu.Unlock()
	defer func() {
		cancel()
		r.activeMu.Lock()
		if r.active == active {
			r.active = nil
		}
		r.activeMu.Unlock()
	}()

	var stream MessageStream
	var pendingQuestion string
	var pendingOptions []QuestionOption
	sawError := false
	sendText := func(text string) error {
		if text == "" {
			return nil
		}
		if stream == nil {
			created, err := r.channel.StreamMarkdown(
				runCtx,
				item.ExternalChatID,
				item.ExternalMessageID,
				text,
			)
			if err != nil {
				return err
			}
			stream = created
			return nil
		}
		return stream.Append(runCtx, text)
	}
	callErr := call(runCtx, func(runID string) {
		r.activeMu.Lock()
		if r.active == active {
			active.runID = runID
		}
		r.activeMu.Unlock()
	}, func(event ChatEvent) error {
		switch event.Kind {
		case "snapshot":
			snapshot, err := parseChatSnapshot(event.Data["parts"])
			if err != nil {
				return err
			}
			if err := sendText(snapshot.Text); err != nil {
				return err
			}
			if snapshot.QuestionID != "" {
				session.PendingQuestionID = snapshot.QuestionID
				session.PendingQuestionVersion = snapshot.QuestionVersion
				session.PendingOptions = snapshot.Options
				session.UpdatedAt = r.manager.now()
				if err := r.manager.store.UpsertSession(runCtx, *session); err != nil {
					return err
				}
				pendingQuestion = snapshot.Question
				pendingOptions = snapshot.Options
			}
		case "text":
			return sendText(event.Data["text"])
		case "question_ready":
			options, err := parseQuestionOptions(event.Data["options"])
			if err != nil {
				return err
			}
			version, err := parsePositiveInt(event.Data["version"])
			if err != nil {
				return err
			}
			session.PendingQuestionID = event.Data["id"]
			session.PendingQuestionVersion = version
			session.PendingOptions = options
			session.UpdatedAt = r.manager.now()
			if err := r.manager.store.UpsertSession(runCtx, *session); err != nil {
				return err
			}
			pendingQuestion = event.Data["question"]
			pendingOptions = options
		case "error":
			sawError = true
		}
		return nil
	})
	if stream != nil {
		closeErr := stream.Close(runCtx)
		if callErr == nil {
			callErr = closeErr
		}
	}
	if callErr != nil {
		r.activeMu.Lock()
		stopped := active.stopped
		r.activeMu.Unlock()
		if stopped {
			return inboxResult{Status: InboxRejected, ErrorCode: "stopped"}
		}
		if chatAgentMismatch(callErr) {
			return r.sendRejected(
				item,
				"agent_mismatch",
				"这个对话属于其他 Agent，请发送 `/history` 选择当前 Agent 的对话。",
			)
		}
		return retryResult("agent_request_failed", callErr)
	}
	if pendingQuestion != "" {
		if err := r.channel.SendMarkdown(
			runCtx,
			item.ExternalChatID,
			item.ExternalMessageID,
			formatQuestion(pendingQuestion, pendingOptions),
		); err != nil {
			return retryResult("send_failed", err)
		}
		return inboxResult{Status: InboxDone}
	}
	if session.PendingQuestionID != "" {
		clearPendingQuestion(session)
		session.UpdatedAt = r.manager.now()
		if err := r.manager.store.UpsertSession(runCtx, *session); err != nil {
			return retryResult("session_unavailable", err)
		}
	}
	if sawError {
		if err := r.channel.SendMarkdown(
			runCtx,
			item.ExternalChatID,
			item.ExternalMessageID,
			"Agent 未能完成本次请求，请稍后重试。",
		); err != nil {
			return retryResult("send_failed", err)
		}
	}
	if stream == nil && !sawError {
		if err := r.channel.SendMarkdown(
			runCtx,
			item.ExternalChatID,
			item.ExternalMessageID,
			"任务已完成。",
		); err != nil {
			return retryResult("send_failed", err)
		}
	}
	return inboxResult{Status: InboxDone}
}

func (r *connectorRunner) sendDone(item InboxItem, text string) inboxResult {
	if err := r.channel.SendMarkdown(
		r.ctx,
		item.ExternalChatID,
		item.ExternalMessageID,
		text,
	); err != nil {
		return retryResult("send_failed", err)
	}
	return inboxResult{Status: InboxDone}
}

func (r *connectorRunner) sendRejected(
	item InboxItem,
	code string,
	text string,
) inboxResult {
	if err := r.channel.SendMarkdown(
		r.ctx,
		item.ExternalChatID,
		item.ExternalMessageID,
		text,
	); err != nil {
		return retryResult("send_failed", err)
	}
	return inboxResult{Status: InboxRejected, ErrorCode: code}
}

func (r *connectorRunner) stopActive() bool {
	r.activeMu.Lock()
	active := r.active
	r.activeMu.Unlock()
	if active == nil {
		return false
	}
	r.activeMu.Lock()
	if r.active == active {
		active.stopped = true
	}
	r.activeMu.Unlock()
	active.cancel()
	return true
}

func (r *connectorRunner) hasActiveRun() bool {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	return r.active != nil
}

func retryResult(code string, err error) inboxResult {
	return inboxResult{ErrorCode: code, RetryError: err}
}

type chatSnapshot struct {
	Text            string
	QuestionID      string
	QuestionVersion int
	Question        string
	Options         []QuestionOption
}

func parseChatSnapshot(raw string) (chatSnapshot, error) {
	if strings.TrimSpace(raw) == "" {
		return chatSnapshot{}, nil
	}
	var parts []convo.Part
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return chatSnapshot{}, fmt.Errorf("decode chat snapshot: %w", err)
	}
	var snapshot chatSnapshot
	var text strings.Builder
	for _, part := range parts {
		switch part.Type {
		case convo.PartText:
			text.WriteString(part.Text)
		case convo.PartQuestion:
			if part.Status != "pending" {
				continue
			}
			snapshot.QuestionID = part.QuestionID
			snapshot.QuestionVersion = part.Version
			snapshot.Question = part.Question
			snapshot.Options = make([]QuestionOption, 0, len(part.QuestionOptions))
			for _, option := range part.QuestionOptions {
				snapshot.Options = append(snapshot.Options, QuestionOption{
					ID: option.ID, Label: option.Label,
				})
			}
		}
	}
	snapshot.Text = text.String()
	return snapshot, nil
}

func chatNotFound(err error) bool {
	var httpErr *ChatHTTPError
	return errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound
}

func chatAgentMismatch(err error) bool {
	var httpErr *ChatHTTPError
	return errors.As(err, &httpErr) &&
		httpErr.Status == http.StatusConflict &&
		httpErr.Code == "AGENT_MISMATCH"
}

func clearPendingQuestion(session *Session) {
	session.PendingQuestionID = ""
	session.PendingQuestionVersion = 0
	session.PendingOptions = nil
}

func formatQuestion(question string, options []QuestionOption) string {
	var builder strings.Builder
	builder.WriteString("**需要你的回答**\n\n")
	builder.WriteString(strings.TrimSpace(question))
	for index, option := range options {
		builder.WriteString(fmt.Sprintf("\n\n%d. %s", index+1, option.Label))
	}
	if len(options) > 0 {
		builder.WriteString("\n\n请回复编号、完整选项文字，或直接输入回答。")
	}
	return builder.String()
}

func helpMessage() string {
	return strings.Join([]string{
		"直接发送文字、图片或文件即可与 Cocola Agent 对话。",
		"",
		"- `/new` 创建新对话",
		"- `/history` 查看最近对话",
		"- `/switch 编号` 切换对话",
		"- `/stop` 停止当前任务",
		"- `/status` 查看状态",
		"- `/help` 查看帮助",
	}, "\n")
}

func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}

func boundedError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 256 {
		message = message[:256]
	}
	return message
}
