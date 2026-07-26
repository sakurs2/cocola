package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	processingReactionEmoji = "Typing"
	reactionPermissionScope = "im:message.reactions:write_only"
)

type PermissionError struct {
	Code          int
	ConsoleURL    string
	MissingScopes []string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("Feishu reaction permission denied (code=%d)", e.Code)
}

func (c *sdkRuntimeChannel) AddReaction(
	ctx context.Context,
	messageID string,
	emojiType string,
) (string, error) {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(emojiType) == "" {
		return "", errors.New("Feishu reaction is empty")
	}
	request := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()
	response, err := c.client.Im.V1.MessageReaction.Create(ctx, request)
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", reactionResponseError(
			response.Code,
			response.Msg,
			responseBody(response.ApiResp),
		)
	}
	if response.Data == nil || response.Data.ReactionId == nil ||
		strings.TrimSpace(*response.Data.ReactionId) == "" {
		return "", errors.New("Feishu reaction response is missing reaction_id")
	}
	return *response.Data.ReactionId, nil
}

func (c *sdkRuntimeChannel) DeleteReaction(
	ctx context.Context,
	messageID string,
	reactionID string,
) error {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(reactionID) == "" {
		return errors.New("Feishu reaction deletion is empty")
	}
	request := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build()
	response, err := c.client.Im.V1.MessageReaction.Delete(ctx, request)
	if err != nil {
		return err
	}
	if !response.Success() {
		return reactionResponseError(
			response.Code,
			response.Msg,
			responseBody(response.ApiResp),
		)
	}
	return nil
}

func responseBody(response *larkcore.ApiResp) []byte {
	if response == nil {
		return nil
	}
	return response.RawBody
}

func reactionResponseError(code int, message string, raw []byte) error {
	if code != 99991672 && code != 99991676 {
		return &larkcore.CodeError{Code: code, Msg: message}
	}
	var payload struct {
		ConsoleURL string `json:"console_url"`
		Error      struct {
			ConsoleURL     string `json:"console_url"`
			Troubleshooter string `json:"troubleshooter"`
			Permission     []struct {
				Scope   string `json:"scope"`
				Subject string `json:"subject"`
			} `json:"permission_violations"`
			Helps []struct {
				URL string `json:"url"`
			} `json:"helps"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &payload)

	candidates := []string{
		payload.ConsoleURL,
		payload.Error.ConsoleURL,
		payload.Error.Troubleshooter,
	}
	for _, help := range payload.Error.Helps {
		candidates = append(candidates, help.URL)
	}
	consoleURL := ""
	for _, candidate := range candidates {
		if trustedPermissionURL(candidate) {
			consoleURL = candidate
			break
		}
	}

	scopes := make([]string, 0, len(payload.Error.Permission))
	seen := make(map[string]struct{}, len(payload.Error.Permission))
	for _, violation := range payload.Error.Permission {
		for _, value := range []string{violation.Scope, violation.Subject} {
			scope := strings.TrimSpace(value)
			if scope == "" || !strings.Contains(scope, ":") {
				continue
			}
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			scopes = append(scopes, scope)
		}
	}
	return &PermissionError{
		Code: code, ConsoleURL: consoleURL, MissingScopes: scopes,
	}
}

func trustedPermissionURL(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil ||
		parsed.Host == "" || parsed.Port() != "" {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "open.feishu.cn", "open.larksuite.com":
		return true
	default:
		return false
	}
}
