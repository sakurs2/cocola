package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocola-project/cocola/packages/go-common/token"
)

type managerTestStore struct {
	Store

	session          Session
	upsertedSessions []Session
	connector        Connector
}

func (s *managerTestStore) GetSession(
	context.Context,
	string,
	string,
) (Session, error) {
	return s.session, nil
}

func (s *managerTestStore) UpsertSession(_ context.Context, session Session) error {
	s.upsertedSessions = append(s.upsertedSessions, session)
	return nil
}

func (s *managerTestStore) GetConnectorByID(context.Context, string) (Connector, error) {
	if s.connector.ID == "" {
		return Connector{}, ErrNotFound
	}
	return s.connector, nil
}

type managerTestChannel struct {
	mu                sync.Mutex
	messages          []string
	reactionID        string
	addReactionErr    error
	deleteReactionErr error
	addReactionCalls  int
	deleteCalls       int
}

type managerTestStream struct {
	channel *managerTestChannel
}

func (*managerTestChannel) OnMessage(func(context.Context, RuntimeMessage) error) {}
func (*managerTestChannel) OnReady(func(BotIdentity))                             {}
func (*managerTestChannel) OnError(func(error))                                   {}
func (*managerTestChannel) Start(context.Context) error                           { return nil }
func (*managerTestChannel) Stop(context.Context) error                            { return nil }
func (c *managerTestChannel) AddReaction(
	context.Context,
	string,
	string,
) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addReactionCalls++
	return c.reactionID, c.addReactionErr
}
func (c *managerTestChannel) DeleteReaction(context.Context, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteCalls++
	return c.deleteReactionErr
}

func (c *managerTestChannel) SendMarkdown(
	_ context.Context,
	_ string,
	_ string,
	text string,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, text)
	return nil
}

func (c *managerTestChannel) StreamMarkdown(
	_ context.Context,
	_ string,
	_ string,
	text string,
) (MessageStream, error) {
	c.mu.Lock()
	c.messages = append(c.messages, text)
	c.mu.Unlock()
	return &managerTestStream{channel: c}, nil
}

func (s *managerTestStream) Append(_ context.Context, text string) error {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	s.channel.messages = append(s.channel.messages, text)
	return nil
}

func (*managerTestStream) Close(context.Context) error { return nil }

func (c *managerTestChannel) sentMessages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.messages...)
}

func (c *managerTestChannel) reactionCounts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.addReactionCalls, c.deleteCalls
}

func TestProcessingReactionLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 26, 20, 0, 0, 0, time.UTC)
	channel := &managerTestChannel{reactionID: "reaction-1"}
	runner := &connectorRunner{
		manager: &Manager{now: func() time.Time { return now }},
		channel: channel,
		ctx:     context.Background(),
	}
	item := InboxItem{
		ExternalChatID:    "chat-1",
		ExternalMessageID: "message-1",
	}
	reactionID := runner.beginProcessingReaction(item)
	if reactionID != "reaction-1" {
		t.Fatalf("reactionID = %q", reactionID)
	}
	runner.finishProcessingReaction(item, reactionID)
	adds, deletes := channel.reactionCounts()
	if adds != 1 || deletes != 1 {
		t.Fatalf("reaction calls = add:%d delete:%d", adds, deletes)
	}
}

func TestReactionPermissionBackoffAndNotice(t *testing.T) {
	now := time.Date(2026, 7, 26, 20, 0, 0, 0, time.UTC)
	const consoleURL = "https://open.feishu.cn/app/cli_test/auth"
	channel := &managerTestChannel{
		addReactionErr: &PermissionError{
			Code:       99991672,
			ConsoleURL: consoleURL,
			MissingScopes: []string{
				reactionPermissionScope,
			},
		},
	}
	runner := &connectorRunner{
		manager: &Manager{
			now:         func() time.Time { return now },
			settingsURL: "https://cocola.example.com/connectors",
		},
		connector: Connector{Domain: DomainFeishu},
		channel:   channel,
		ctx:       context.Background(),
	}
	item := InboxItem{
		ExternalChatID:    "chat-1",
		ExternalMessageID: "message-1",
	}

	if got := runner.beginProcessingReaction(item); got != "" {
		t.Fatalf("reactionID = %q, want empty", got)
	}
	waitForMessages(t, channel, 1)
	messages := channel.sentMessages()
	if !strings.Contains(messages[0], consoleURL) ||
		!strings.Contains(messages[0], reactionPermissionScope) {
		t.Fatalf("permission notice = %q", messages[0])
	}

	runner.beginProcessingReaction(item)
	adds, _ := channel.reactionCounts()
	if adds != 1 {
		t.Fatalf("reaction calls during backoff = %d, want 1", adds)
	}

	now = now.Add(11 * time.Minute)
	runner.beginProcessingReaction(item)
	adds, _ = channel.reactionCounts()
	if adds != 2 {
		t.Fatalf("reaction calls after backoff = %d, want 2", adds)
	}
	time.Sleep(10 * time.Millisecond)
	if got := len(channel.sentMessages()); got != 1 {
		t.Fatalf("permission notices within 24h = %d, want 1", got)
	}

	now = now.Add(25 * time.Hour)
	runner.beginProcessingReaction(item)
	waitForMessages(t, channel, 2)
}

func waitForMessages(t *testing.T, channel *managerTestChannel, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(channel.sentMessages()) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("sent messages = %d, want at least %d", len(channel.sentMessages()), count)
}

func TestConsumeAgentStreamRestoresSnapshotText(t *testing.T) {
	channel := &managerTestChannel{}
	store := &managerTestStore{}
	runner := &connectorRunner{
		manager: &Manager{store: store},
		connector: Connector{
			ID: "connector-1",
		},
		channel: channel,
		ctx:     context.Background(),
	}
	parts, err := json.Marshal([]map[string]string{{
		"type": "text",
		"text": "already generated",
	}})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	session := Session{
		ConnectorID:    "connector-1",
		ExternalChatID: "chat-1",
		ConversationID: "conversation-1",
	}
	result := runner.consumeAgentStream(
		InboxItem{
			ExternalChatID:    "chat-1",
			ExternalMessageID: "message-1",
		},
		"runtime-token",
		&session,
		func(_ context.Context, _ func(string), onEvent func(ChatEvent) error) error {
			return onEvent(ChatEvent{
				Kind: "snapshot",
				Data: map[string]string{"parts": string(parts), "status": "completed"},
			})
		},
	)
	if result.Status != InboxDone {
		t.Fatalf("result = %+v, want done", result)
	}
	messages := strings.Join(channel.sentMessages(), "\n")
	if !strings.Contains(messages, "already generated") {
		t.Fatalf("snapshot text was not sent: %q", messages)
	}
	if strings.Contains(messages, "任务已完成") {
		t.Fatalf("snapshot replay fell back to generic completion: %q", messages)
	}
}

func TestParseChatSnapshotRestoresPendingQuestion(t *testing.T) {
	snapshot, err := parseChatSnapshot(`[
		{"type":"text","text":"before question"},
		{
			"type":"question",
			"questionId":"question-1",
			"version":2,
			"status":"pending",
			"question":"Choose one",
			"options":[{"id":"option-1","label":"First"}]
		}
	]`)
	if err != nil {
		t.Fatalf("parseChatSnapshot: %v", err)
	}
	if snapshot.Text != "before question" ||
		snapshot.QuestionID != "question-1" ||
		snapshot.QuestionVersion != 2 ||
		snapshot.Question != "Choose one" ||
		len(snapshot.Options) != 1 ||
		snapshot.Options[0].ID != "option-1" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestProcessInboxRejectsUnconfiguredModel(t *testing.T) {
	const signingSecret = "test-secret"
	issuer := token.NewIssuer(signingSecret, "cocola", time.Hour)
	accountServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Account{
			ID: "user-1", TenantID: "tenant-1", Enabled: true,
			Email: "trusted@example.com", Name: "Trusted", Username: "trusted",
		})
	}))
	defer accountServer.Close()
	accounts, err := NewAccountAuthorizer(accountServer.URL, issuer, accountServer.Client())
	if err != nil {
		t.Fatalf("NewAccountAuthorizer: %v", err)
	}
	connector := Connector{
		ID: "connector-1", UserID: "user-1", TenantID: "tenant-1",
	}
	channel := &managerTestChannel{
		addReactionErr: &PermissionError{Code: 99991672},
	}
	runner := &connectorRunner{
		manager: &Manager{
			store: &managerTestStore{
				session: Session{
					ConnectorID: "connector-1", ExternalChatID: "chat-1",
					ConversationID: "conversation-1",
				},
				connector: connector,
			},
			accounts:    accounts,
			settingsURL: "https://cocola.example.com/connectors",
			now:         func() time.Time { return time.Now().UTC() },
		},
		connector: connector,
		channel:   channel,
		ctx:       context.Background(),
	}
	result := runner.processInbox(InboxItem{
		ExternalChatID:    "chat-1",
		ExternalMessageID: "message-1",
		Payload:           InboxPayload{Text: "hello"},
	}, nil)
	if result.Status != InboxRejected || result.ErrorCode != "model_not_configured" {
		t.Fatalf("result = %+v, want model_not_configured rejection", result)
	}
	waitForMessages(t, channel, 2)
	messages := strings.Join(channel.sentMessages(), "\n")
	if !strings.Contains(messages, "选择模型") {
		t.Fatalf("configuration guidance was not sent: %q", messages)
	}
	if !strings.Contains(messages, "https://cocola.example.com/connectors") {
		t.Fatalf("reaction permission guidance was not sent: %q", messages)
	}
}

func TestProcessInboxStopUsesFreshRuntimeToken(t *testing.T) {
	const signingSecret = "test-secret"
	issuer := token.NewIssuer(signingSecret, "cocola", time.Hour)
	accountServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/account" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Account{
			ID: "user-1", TenantID: "tenant-1", Enabled: true,
			Email: "trusted@example.com", Name: "Trusted", Username: "trusted",
		})
	}))
	defer accountServer.Close()
	accounts, err := NewAccountAuthorizer(accountServer.URL, issuer, accountServer.Client())
	if err != nil {
		t.Fatalf("NewAccountAuthorizer: %v", err)
	}

	cancelled := make(chan string, 1)
	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.Path == "/v1/chat/runs/active" &&
			r.URL.Query().Get("conversation_id") == "conversation-1":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"run_id":"run-1","status":"running"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/chat/runs/run-1":
			cancelled <- strings.TrimPrefix(r.Header.Get("authorization"), "Bearer ")
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer chatServer.Close()
	chat, err := NewChatClient(chatServer.URL, chatServer.Client())
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}

	channel := &managerTestChannel{}
	runner := &connectorRunner{
		manager: &Manager{
			store: &managerTestStore{session: Session{
				ConnectorID: "connector-1", ExternalChatID: "chat-1",
				ConversationID: "conversation-1",
			}},
			accounts: accounts,
			chat:     chat,
			now:      func() time.Time { return time.Now().UTC() },
		},
		connector: Connector{
			ID: "connector-1", UserID: "user-1", TenantID: "tenant-1",
		},
		channel: channel,
		ctx:     context.Background(),
		active: &activeRun{
			cancel: func() {}, runID: "run-1",
		},
	}
	result := runner.processInbox(InboxItem{
		ExternalChatID:    "chat-1",
		ExternalMessageID: "message-stop",
		Payload: InboxPayload{
			Text: "/stop",
		},
	}, nil)
	if result.Status != InboxDone {
		t.Fatalf("result = %+v, want done", result)
	}
	select {
	case runtimeToken := <-cancelled:
		if runtimeToken == "" {
			t.Fatal("cancel did not use an authenticated runtime token")
		}
		if _, err := token.Decode(runtimeToken, signingSecret, time.Now().Unix()); err != nil {
			t.Fatalf("cancel token is not freshly signed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run cancellation was not requested")
	}
}

func TestProcessInboxHistoryAndSwitch(t *testing.T) {
	const signingSecret = "test-secret"
	issuer := token.NewIssuer(signingSecret, "cocola", time.Hour)
	accountServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/account" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Account{
			ID: "user-1", TenantID: "tenant-1", Enabled: true,
			Email: "trusted@example.com", Name: "Trusted", Username: "trusted",
		})
	}))
	defer accountServer.Close()
	accounts, err := NewAccountAuthorizer(accountServer.URL, issuer, accountServer.Client())
	if err != nil {
		t.Fatalf("NewAccountAuthorizer: %v", err)
	}

	listCalls := 0
	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Error("history request is missing authorization")
		}
		if r.URL.Path == "/v1/product-config" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent_runtime": map[string]string{"default_id": "claude-code"},
			})
			return
		}
		if r.URL.Path != "/v1/conversations" {
			http.NotFound(w, r)
			return
		}
		listCalls++
		rows := []map[string]any{
			{
				"id": "conversation-current", "title": "Current chat",
				"chat_type": "chat", "runtime_id": "claude-code",
			},
			{
				"id": "conversation-old", "title": "Older chat",
				"chat_type": "chat", "runtime_id": "claude-code",
			},
			{
				"id": "codex", "title": "Codex chat",
				"chat_type": "chat", "runtime_id": "codex",
			},
			{
				"id": "scheduled", "title": "Scheduled task",
				"chat_type": "scheduled_task", "runtime_id": "claude-code",
			},
			{
				"id": "project", "title": "Project chat", "chat_type": "chat",
				"project_id": "project-1", "runtime_id": "claude-code",
			},
		}
		if listCalls > 1 {
			rows[0], rows[1] = rows[1], rows[0]
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer chatServer.Close()
	chat, err := NewChatClient(chatServer.URL, chatServer.Client())
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}

	store := &managerTestStore{session: Session{
		ConnectorID: "connector-1", ExternalChatID: "chat-1",
		ConversationID: "conversation-current",
	}}
	channel := &managerTestChannel{}
	runner := &connectorRunner{
		manager: &Manager{
			store: store, accounts: accounts, chat: chat,
			now: func() time.Time { return time.Date(2026, 7, 26, 20, 0, 0, 0, time.UTC) },
		},
		connector: Connector{
			ID: "connector-1", UserID: "user-1", TenantID: "tenant-1",
		},
		channel: channel,
		ctx:     context.Background(),
	}

	historyResult := runner.processInbox(InboxItem{
		ExternalChatID: "chat-1", ExternalMessageID: "message-history",
		Payload: InboxPayload{Text: "/history"},
	}, nil)
	if historyResult.Status != InboxDone {
		t.Fatalf("history result = %+v, want done", historyResult)
	}
	history := strings.Join(channel.sentMessages(), "\n")
	if !strings.Contains(history, "1. Current chat · 当前") ||
		!strings.Contains(history, "2. Older chat") ||
		strings.Contains(history, "Codex chat") ||
		strings.Contains(history, "Scheduled task") ||
		strings.Contains(history, "Project chat") {
		t.Fatalf("history response = %q", history)
	}

	invalidResult := runner.processInbox(InboxItem{
		ExternalChatID: "chat-1", ExternalMessageID: "message-switch-invalid",
		Payload: InboxPayload{Text: "/switch 9"},
	}, nil)
	if invalidResult.Status != InboxDone || len(store.upsertedSessions) != 0 {
		t.Fatalf("invalid switch result = %+v, sessions = %+v", invalidResult, store.upsertedSessions)
	}

	store.session.PendingQuestionID = "question-1"
	pendingResult := runner.processInbox(InboxItem{
		ExternalChatID: "chat-1", ExternalMessageID: "message-switch-pending",
		Payload: InboxPayload{Text: "/switch 2"},
	}, nil)
	if pendingResult.Status != InboxDone || len(store.upsertedSessions) != 0 {
		t.Fatalf("pending switch result = %+v, sessions = %+v", pendingResult, store.upsertedSessions)
	}
	store.session.PendingQuestionID = ""

	switchResult := runner.processInbox(InboxItem{
		ExternalChatID: "chat-1", ExternalMessageID: "message-switch",
		Payload: InboxPayload{Text: "/switch 2"},
	}, nil)
	if switchResult.Status != InboxDone {
		t.Fatalf("switch result = %+v, want done", switchResult)
	}
	if len(store.upsertedSessions) != 1 ||
		store.upsertedSessions[0].ConversationID != "conversation-old" {
		t.Fatalf("upserted sessions = %+v", store.upsertedSessions)
	}
	messages := strings.Join(channel.sentMessages(), "\n")
	if !strings.Contains(messages, "已切换到对话：Older chat") {
		t.Fatalf("switch response = %q", messages)
	}
}

func TestParseSwitchCommand(t *testing.T) {
	tests := []struct {
		command string
		index   int
		matched bool
		valid   bool
	}{
		{command: "/switch 2", index: 2, matched: true, valid: true},
		{command: " /switch\t3 ", index: 3, matched: true, valid: true},
		{command: "/switch", matched: true},
		{command: "/switch zero", matched: true},
		{command: "/switch 0", matched: true},
		{command: "/switcher 1"},
	}
	for _, test := range tests {
		index, matched, err := parseSwitchCommand(test.command)
		wantError := test.matched && !test.valid
		if index != test.index || matched != test.matched || (err != nil) != wantError {
			t.Fatalf(
				"parseSwitchCommand(%q) = %d, %t, %v",
				test.command,
				index,
				matched,
				err,
			)
		}
	}
}
