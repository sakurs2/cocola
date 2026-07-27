package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatClientStreamsOnlyStructuredEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("authorization") != "Bearer runtime-token" {
			t.Errorf("authorization = %q", r.Header.Get("authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["conversation_type"] != "interactive" ||
			body["interaction_mode"] != "execute" ||
			body["client_request_id"] != "request-1" ||
			body["agent_id"] != "agent-1" {
			t.Errorf("request body = %#v", body)
		}
		w.Header().Set("content-type", "text/event-stream")
		w.Header().Set("x-cocola-run-id", "run-1")
		_, _ = w.Write([]byte(
			"event: text\n" +
				"data: {\"kind\":\"text\",\"data\":{\"text\":\"hello\"}}\n\n" +
				"event: done\n" +
				"data: {\"kind\":\"done\",\"data\":{\"status\":\"success\"}}\n\n",
		))
	}))
	defer server.Close()

	client, err := NewChatClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	var runID string
	var events []ChatEvent
	err = client.Chat(
		context.Background(),
		"runtime-token",
		ChatTurn{
			Prompt: "hi", ConversationID: "conversation-1",
			ConversationTitle: "Feishu · hi", ClientRequestID: "request-1",
			AgentID: "agent-1",
		},
		func(value string) { runID = value },
		func(event ChatEvent) error {
			events = append(events, event)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if runID != "run-1" || len(events) != 2 ||
		events[0].Kind != "text" || events[0].Data["text"] != "hello" ||
		events[1].Kind != "done" {
		t.Fatalf("runID=%q events=%+v", runID, events)
	}
}

func TestDeterministicRequestIDAndQuestionAnswer(t *testing.T) {
	first := DeterministicRequestID("connector-1", "event-1", "chat")
	if first != DeterministicRequestID("connector-1", "event-1", "chat") {
		t.Fatal("same event generated different request IDs")
	}
	if first == DeterministicRequestID("connector-1", "event-2", "chat") {
		t.Fatal("different events generated the same request ID")
	}
	options := []QuestionOption{
		{ID: "option-a", Label: "Alpha"},
		{ID: "option-b", Label: "Beta"},
	}
	if answer := questionAnswer("2", options); answer.OptionID != "option-b" {
		t.Fatalf("number answer = %+v", answer)
	}
	if answer := questionAnswer(" alpha ", options); answer.OptionID != "option-a" {
		t.Fatalf("label answer = %+v", answer)
	}
	if answer := questionAnswer("free text", options); answer.Text != "free text" {
		t.Fatalf("free answer = %+v", answer)
	}
}

func TestChatClientListsOwnedConversations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("authorization") != "Bearer runtime-token" {
			t.Errorf("authorization = %q", r.Header.Get("authorization"))
		}
		switch r.URL.Path {
		case "/v1/conversations":
			_, _ = w.Write([]byte(`[
				{"id":"conversation-1","title":"First","chat_type":"chat"},
				{"id":"conversation-2","title":"Second","chat_type":"chat"}
			]`))
		case "/v1/product-config":
			_, _ = w.Write([]byte(`{"agent_runtime":{"default_id":"claude-code"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewChatClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	conversations, err := client.ListConversations(context.Background(), "runtime-token")
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(conversations) != 2 ||
		conversations[0].ID != "conversation-1" ||
		conversations[1].Title != "Second" {
		t.Fatalf("conversations = %+v", conversations)
	}
	defaultRuntimeID, err := client.DefaultRuntimeID(context.Background(), "runtime-token")
	if err != nil || defaultRuntimeID != "claude-code" {
		t.Fatalf("DefaultRuntimeID = %q, %v", defaultRuntimeID, err)
	}
}

func TestChatClientConversationMetadataErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/conversations":
			http.Error(w, `{"error":{"code":"INTERNAL"}}`, http.StatusInternalServerError)
		case "/v1/product-config":
			_, _ = w.Write([]byte(`{"agent_runtime":`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewChatClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.ListConversations(context.Background(), "runtime-token"); err == nil {
		t.Fatal("ListConversations accepted a non-2xx response")
	}
	if _, err := client.DefaultRuntimeID(context.Background(), "runtime-token"); err == nil {
		t.Fatal("DefaultRuntimeID accepted malformed JSON")
	}
}

func TestGatewayLoopbackURL(t *testing.T) {
	tests := map[string]string{
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:9090":   "http://127.0.0.1:9090",
		"[::]:7070":      "http://127.0.0.1:7070",
		"localhost:6060": "http://localhost:6060",
	}
	for input, want := range tests {
		got, err := GatewayLoopbackURL(input)
		if err != nil || got != want {
			t.Fatalf("GatewayLoopbackURL(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestRegistrationAvatarURL(t *testing.T) {
	got := RegistrationAvatarURL(
		"not-an-origin, https://cocola.example.com/, https://ignored.example.com",
	)
	if got != "https://cocola.example.com/icon.svg" {
		t.Fatalf("RegistrationAvatarURL = %q", got)
	}
	if got := RegistrationAvatarURL("file:///tmp/icon.svg"); got != "" {
		t.Fatalf("invalid avatar origin = %q", got)
	}
}
