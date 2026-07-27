package feishu

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

const maxSSELineBytes = 1 << 20

type ChatAttachment struct {
	Filename string
	MIME     string
	Content  []byte
}

type ChatTurn struct {
	Prompt            string
	ConversationID    string
	ConversationTitle string
	ClientRequestID   string
	AgentID           string
	Attachments       []ChatAttachment
}

type ChatEvent struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
}

type ChatHTTPError struct {
	Status int
	Code   string
}

type ChatRun struct {
	ID     string `json:"run_id"`
	Status string `json:"status"`
}

type ChatProductConfig struct {
	AgentRuntime struct {
		DefaultID string `json:"default_id"`
	} `json:"agent_runtime"`
}

func (e *ChatHTTPError) Error() string {
	return fmt.Sprintf("chat request failed: status=%d code=%s", e.Status, e.Code)
}

type ChatClient struct {
	baseURL string
	http    *http.Client
}

func NewChatClient(baseURL string, httpClient *http.Client) (*ChatClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return nil, errors.New("Gateway loopback URL must be an absolute http URL")
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &ChatClient{baseURL: baseURL, http: httpClient}, nil
}

func GatewayLoopbackURL(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid COCOLA_GATEWAY_ADDR: %w", err)
	}
	if port == "" {
		return "", errors.New("COCOLA_GATEWAY_ADDR is missing a port")
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func DeterministicRequestID(connectorID, eventID, action string) string {
	name := "cocola:feishu:" + connectorID + ":" + eventID + ":" + action
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()
}

func (c *ChatClient) Chat(
	ctx context.Context,
	runtimeToken string,
	turn ChatTurn,
	onStarted func(string),
	onEvent func(ChatEvent) error,
) error {
	attachments := make([]map[string]string, 0, len(turn.Attachments))
	for _, attachment := range turn.Attachments {
		attachments = append(attachments, map[string]string{
			"filename":    attachment.Filename,
			"mime":        attachment.MIME,
			"content_b64": base64.StdEncoding.EncodeToString(attachment.Content),
		})
	}
	prompt := strings.TrimSpace(turn.Prompt)
	if prompt == "" && len(attachments) > 0 {
		prompt = "Please review the attached files."
	}
	body := map[string]any{
		"prompt":             prompt,
		"session_id":         turn.ConversationID,
		"conversation_title": turn.ConversationTitle,
		"conversation_type":  "interactive",
		"interaction_mode":   "execute",
		"client_request_id":  turn.ClientRequestID,
		"agent_id":           turn.AgentID,
		"attachments":        attachments,
	}
	return c.stream(
		ctx,
		http.MethodPost,
		"/v1/chat",
		runtimeToken,
		body,
		onStarted,
		onEvent,
	)
}

func (c *ChatClient) AnswerQuestion(
	ctx context.Context,
	runtimeToken string,
	session Session,
	answer QuestionAnswer,
	clientRequestID string,
	onStarted func(string),
	onEvent func(ChatEvent) error,
) error {
	body := map[string]any{
		"expected_version":  session.PendingQuestionVersion,
		"answer":            answer,
		"client_request_id": clientRequestID,
	}
	path := "/v1/conversations/" + url.PathEscape(session.ConversationID) +
		"/questions/" + url.PathEscape(session.PendingQuestionID) + "/answer"
	return c.stream(
		ctx,
		http.MethodPost,
		path,
		runtimeToken,
		body,
		onStarted,
		onEvent,
	)
}

type QuestionAnswer struct {
	OptionID string `json:"option_id,omitempty"`
	Text     string `json:"text,omitempty"`
}

func (c *ChatClient) CancelRun(
	ctx context.Context,
	runtimeToken string,
	runID string,
) error {
	if runID == "" {
		return nil
	}
	return c.requestJSON(
		ctx,
		http.MethodDelete,
		"/v1/chat/runs/"+url.PathEscape(runID),
		runtimeToken,
		nil,
	)
}

func (c *ChatClient) ActiveRun(
	ctx context.Context,
	runtimeToken string,
	conversationID string,
) (ChatRun, error) {
	if conversationID == "" {
		return ChatRun{}, ErrInvalid
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/chat/runs/active?conversation_id="+url.QueryEscape(conversationID),
		nil,
	)
	if err != nil {
		return ChatRun{}, err
	}
	req.Header.Set("authorization", "Bearer "+runtimeToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return ChatRun{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatRun{}, decodeChatHTTPError(resp)
	}
	var run ChatRun
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&run); err != nil {
		return ChatRun{}, fmt.Errorf("decode active chat run: %w", err)
	}
	if run.ID == "" {
		return ChatRun{}, errors.New("active chat run response is missing run_id")
	}
	return run, nil
}

func (c *ChatClient) CancelQuestion(
	ctx context.Context,
	runtimeToken string,
	session Session,
) error {
	if session.PendingQuestionID == "" {
		return nil
	}
	body := map[string]int{"expected_version": session.PendingQuestionVersion}
	return c.requestJSON(
		ctx,
		http.MethodPost,
		"/v1/conversations/"+url.PathEscape(session.ConversationID)+
			"/questions/"+url.PathEscape(session.PendingQuestionID)+"/cancel",
		runtimeToken,
		body,
	)
}

func (c *ChatClient) ListConversations(
	ctx context.Context,
	runtimeToken string,
) ([]convo.Conversation, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/conversations",
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+runtimeToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeChatHTTPError(resp)
	}
	var conversations []convo.Conversation
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&conversations); err != nil {
		return nil, fmt.Errorf("decode conversation list: %w", err)
	}
	return conversations, nil
}

func (c *ChatClient) DefaultRuntimeID(
	ctx context.Context,
	runtimeToken string,
) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/product-config",
		nil,
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("authorization", "Bearer "+runtimeToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", decodeChatHTTPError(resp)
	}
	var config ChatProductConfig
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&config); err != nil {
		return "", fmt.Errorf("decode product config: %w", err)
	}
	defaultID := strings.TrimSpace(config.AgentRuntime.DefaultID)
	if defaultID == "" {
		return "", errors.New("product config is missing the default runtime")
	}
	return defaultID, nil
}

func (c *ChatClient) stream(
	ctx context.Context,
	method string,
	path string,
	runtimeToken string,
	body any,
	onStarted func(string),
	onEvent func(ChatEvent) error,
) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+runtimeToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeChatHTTPError(resp)
	}
	if onStarted != nil {
		onStarted(strings.TrimSpace(resp.Header.Get("x-cocola-run-id")))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), maxSSELineBytes)
	eventName := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			var event ChatEvent
			if err := json.Unmarshal(
				[]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))),
				&event,
			); err != nil {
				return fmt.Errorf("decode chat SSE: %w", err)
			}
			if event.Kind == "" {
				event.Kind = eventName
			}
			if onEvent != nil {
				if err := onEvent(event); err != nil {
					return err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read chat SSE: %w", err)
	}
	return nil
}

func (c *ChatClient) requestJSON(
	ctx context.Context,
	method string,
	path string,
	runtimeToken string,
	body any,
) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "Bearer "+runtimeToken)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeChatHTTPError(resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

func decodeChatHTTPError(resp *http.Response) error {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope)
	return &ChatHTTPError{Status: resp.StatusCode, Code: envelope.Error.Code}
}

func parseQuestionOptions(raw string) ([]QuestionOption, error) {
	var options []QuestionOption
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return nil, err
	}
	return options, nil
}

func questionAnswer(text string, options []QuestionOption) QuestionAnswer {
	trimmed := strings.TrimSpace(text)
	if index, err := strconv.Atoi(trimmed); err == nil &&
		index > 0 && index <= len(options) {
		return QuestionAnswer{OptionID: options[index-1].ID}
	}
	for _, option := range options {
		if strings.EqualFold(trimmed, strings.TrimSpace(option.Label)) {
			return QuestionAnswer{OptionID: option.ID}
		}
	}
	return QuestionAnswer{Text: trimmed}
}

func conversationTitle(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return "Feishu conversation"
	}
	runes := []rune(text)
	if len(runes) > 64 {
		text = string(runes[:64]) + "…"
	}
	return "Feishu · " + text
}

func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{
		time.Second,
		5 * time.Second,
		30 * time.Second,
		2 * time.Minute,
		5 * time.Minute,
	}
	if attempt <= 0 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}
