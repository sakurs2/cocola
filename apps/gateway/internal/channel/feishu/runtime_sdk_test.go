package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larktypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestSDKRuntimeUsesBoundedHTTPPrimitivesWithoutChannelLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(
				`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`,
			))
		case r.URL.Path == "/open-apis/bot/v3/info":
			_, _ = w.Write([]byte(
				`{"code":0,"bot":{"open_id":"bot-1","app_name":"Cocola","activate_status":2}}`,
			))
		case r.Method == http.MethodPost &&
			r.URL.Path == "/open-apis/im/v1/messages/incoming-message/reactions":
			var body struct {
				ReactionType struct {
					EmojiType string `json:"emoji_type"`
				} `json:"reaction_type"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode reaction body: %v", err)
			}
			if body.ReactionType.EmojiType != processingReactionEmoji {
				t.Errorf("reaction emoji = %q", body.ReactionType.EmojiType)
			}
			_, _ = w.Write([]byte(
				`{"code":0,"data":{"reaction_id":"reaction-1"}}`,
			))
		case r.Method == http.MethodDelete &&
			r.URL.Path == "/open-apis/im/v1/messages/incoming-message/reactions/reaction-1":
			_, _ = w.Write([]byte(
				`{"code":0,"data":{"reaction_id":"reaction-1"}}`,
			))
		case strings.HasSuffix(r.URL.Path, "/reply"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode reply body: %v", err)
			}
			if body["msg_type"] != "post" {
				t.Errorf("reply body = %#v", body)
			}
			_, _ = w.Write([]byte(
				`{"code":0,"data":{"message_id":"reply-1"}}`,
			))
		case r.URL.Path == "/open-apis/im/v1/messages":
			if r.URL.Query().Get("receive_id_type") != "chat_id" {
				t.Errorf("receive_id_type = %q", r.URL.Query().Get("receive_id_type"))
			}
			_, _ = w.Write([]byte(
				`{"code":0,"data":{"message_id":"message-1"}}`,
			))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := lark.NewClient(
		"app-id",
		"app-secret",
		lark.WithOpenBaseUrl(server.URL),
	)
	runtime := &sdkRuntimeChannel{
		client: client,
		config: larktypes.DefaultChannelConfig(),
	}
	identity, err := runtime.getBotIdentity(context.Background())
	if err != nil {
		t.Fatalf("getBotIdentity: %v", err)
	}
	if identity.OpenID != "bot-1" || identity.Name != "Cocola" ||
		identity.ActivateStatus != 2 {
		t.Fatalf("identity = %+v", identity)
	}
	replyID, err := runtime.sendMarkdown(
		context.Background(),
		"chat-1",
		"incoming-message",
		"**reply**",
	)
	if err != nil || replyID != "reply-1" {
		t.Fatalf("reply = %q, %v", replyID, err)
	}
	messageID, err := runtime.sendMarkdown(
		context.Background(),
		"chat-1",
		"",
		"new message",
	)
	if err != nil || messageID != "message-1" {
		t.Fatalf("create = %q, %v", messageID, err)
	}
	reactionID, err := runtime.AddReaction(
		context.Background(),
		"incoming-message",
		processingReactionEmoji,
	)
	if err != nil || reactionID != "reaction-1" {
		t.Fatalf("reaction = %q, %v", reactionID, err)
	}
	if err := runtime.DeleteReaction(
		context.Background(),
		"incoming-message",
		reactionID,
	); err != nil {
		t.Fatalf("DeleteReaction: %v", err)
	}
}
