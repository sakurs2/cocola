package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	openviking "github.com/volcengine/OpenViking/sdk/go"
)

var ErrNotFound = errors.New("memory: not found")

type Identity struct {
	TenantID string
	UserID   string
}

func (i Identity) openVikingAccount() string {
	if account := strings.TrimSpace(i.TenantID); account != "" {
		return account
	}
	return "default"
}

// openVikingClient keeps the official v0.4.10 Go SDK behind the memory module.
// The sole raw endpoint is /used, which that SDK release does not expose yet.
type openVikingClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type openVikingTask struct {
	ID     string
	Status string
}

type commitArchiveState int

const (
	commitArchiveAbsent commitArchiveState = iota
	commitArchivePending
	commitArchiveCompleted
	commitArchiveFailed
)

func newOpenVikingClient(baseURL, apiKey string) *openVikingClient {
	return &openVikingClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *openVikingClient) sdk(identity Identity) (*openviking.Client, error) {
	return openviking.NewClient(openviking.Config{
		BaseURL: c.baseURL, APIKey: c.apiKey,
		Account: identity.openVikingAccount(), User: identity.UserID,
		HTTPClient: c.http,
	})
}

func (c *openVikingClient) close() { c.http.CloseIdleConnections() }

func (c *openVikingClient) ready(ctx context.Context) error {
	if c.baseURL == "" {
		return errors.New("OpenViking URL is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ready", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenViking readiness returned %d", response.StatusCode)
	}
	return nil
}

func normalizeOpenVikingError(err error) error {
	if err == nil {
		return nil
	}
	if openviking.IsCode(err, "NOT_FOUND") {
		return ErrNotFound
	}
	var apiErr *openviking.Error
	if errors.As(err, &apiErr) {
		return fmt.Errorf("OpenViking request failed: %s", apiErr.Code)
	}
	return errors.New("OpenViking request failed: UNAVAILABLE")
}

func (c *openVikingClient) find(
	ctx context.Context,
	identity Identity,
	query string,
	limit int,
) ([]memoryResult, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return nil, err
	}
	result, err := client.Find(ctx, query, &openviking.FindOptions{
		TargetURI: []string{
			"viking://user/memories/preferences/",
			"viking://user/memories/entities/",
			"viking://user/memories/events/",
		},
		Limit: limit,
	})
	if err != nil {
		return nil, normalizeOpenVikingError(err)
	}
	items := make([]memoryResult, 0, min(limit, len(result.Memories)))
	for _, item := range result.Memories {
		if len(items) >= limit {
			break
		}
		items = append(items, memoryResult{
			URI: item.URI, Abstract: item.Abstract, Content: item.Overview, Score: item.Score,
		})
	}
	return items, nil
}

func (c *openVikingClient) read(
	ctx context.Context,
	identity Identity,
	uri string,
) (string, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return "", err
	}
	content, err := client.Read(ctx, uri, 0, -1)
	return content, normalizeOpenVikingError(err)
}

func (c *openVikingClient) createSession(
	ctx context.Context,
	identity Identity,
	sessionID string,
) error {
	client, err := c.sdk(identity)
	if err != nil {
		return err
	}
	_, err = client.CreateSession(ctx, &openviking.CreateSessionOptions{
		SessionID: sessionID,
		MemoryPolicy: map[string]any{
			"self":           map[string]bool{"enabled": true},
			"peer":           map[string]bool{"enabled": false},
			"working_memory": map[string]bool{"enabled": false},
			"memory_types":   []string{"profile", "preferences", "entities", "events"},
		},
	})
	if openviking.IsCode(err, "ALREADY_EXISTS") {
		return nil
	}
	return normalizeOpenVikingError(err)
}

func (c *openVikingClient) addMessages(
	ctx context.Context,
	identity Identity,
	sessionID string,
	messages []map[string]string,
) error {
	client, err := c.sdk(identity)
	if err != nil {
		return err
	}
	batch := make([]openviking.Message, 0, len(messages))
	for _, message := range messages {
		content := message["content"]
		batch = append(batch, openviking.Message{Role: message["role"], Content: &content})
	}
	_, err = client.BatchAddMessages(ctx, sessionID, batch, nil)
	return normalizeOpenVikingError(err)
}

func (c *openVikingClient) used(
	ctx context.Context,
	identity Identity,
	sessionID string,
	contexts []string,
) error {
	if len(contexts) == 0 {
		return nil
	}
	return c.rawCall(
		ctx,
		identity,
		http.MethodPost,
		"/api/v1/sessions/"+url.PathEscape(sessionID)+"/used",
		map[string]any{"contexts": contexts},
		nil,
	)
}

func (c *openVikingClient) commit(
	ctx context.Context,
	identity Identity,
	sessionID string,
) (string, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return "", err
	}
	result, err := client.CommitSession(ctx, sessionID, &openviking.CommitSessionOptions{
		KeepRecentCount: 0,
	})
	if err != nil {
		return "", normalizeOpenVikingError(err)
	}
	return stringValue(result["task_id"]), nil
}

func (c *openVikingClient) taskStatus(
	ctx context.Context,
	identity Identity,
	taskID string,
) (string, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return "", err
	}
	result, err := client.GetTask(ctx, taskID)
	if err != nil {
		return "", normalizeOpenVikingError(err)
	}
	if result == nil {
		return "", ErrNotFound
	}
	return strings.ToLower(stringValue(result["status"])), nil
}

func (c *openVikingClient) latestCommitTask(
	ctx context.Context,
	identity Identity,
	sessionID string,
) (openVikingTask, bool, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return openVikingTask{}, false, err
	}
	items, err := client.ListTasks(ctx, &openviking.ListTasksOptions{
		TaskType: "session_commit", ResourceID: sessionID, Limit: 20,
	})
	if err != nil {
		return openVikingTask{}, false, normalizeOpenVikingError(err)
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		task := openVikingTask{
			ID: stringValue(item["task_id"]), Status: strings.ToLower(stringValue(item["status"])),
		}
		if task.ID == "" {
			continue
		}
		// OpenViking returns newest tasks first. Only the newest task belongs to
		// the current deterministic-session attempt; older terminal records must
		// not override it after the session has been reset and retried.
		return task, true, nil
	}
	return openVikingTask{}, false, nil
}

func (c *openVikingClient) commitArchiveState(
	ctx context.Context,
	identity Identity,
	sessionID string,
) (commitArchiveState, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return commitArchiveAbsent, err
	}
	base := "viking://session/" + sessionID + "/history/archive_001/"
	exists := func(uri string) (bool, error) {
		_, statErr := client.Stat(ctx, uri)
		if statErr == nil {
			return true, nil
		}
		if openviking.IsCode(statErr, "NOT_FOUND") {
			return false, nil
		}
		return false, normalizeOpenVikingError(statErr)
	}
	if found, statErr := exists(base + ".done"); statErr != nil {
		return commitArchiveAbsent, statErr
	} else if found {
		return commitArchiveCompleted, nil
	}
	if found, statErr := exists(base + ".failed.json"); statErr != nil {
		return commitArchiveAbsent, statErr
	} else if found {
		return commitArchiveFailed, nil
	}
	if found, statErr := exists(base + "messages.jsonl"); statErr != nil {
		return commitArchiveAbsent, statErr
	} else if found {
		return commitArchivePending, nil
	}
	return commitArchiveAbsent, nil
}

func (c *openVikingClient) list(
	ctx context.Context,
	identity Identity,
	uri string,
) (any, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return nil, err
	}
	result, err := client.List(ctx, uri, &openviking.ListOptions{
		Recursive: true, Output: "original", NodeLimit: 1000,
	})
	return result, normalizeOpenVikingError(err)
}

func (c *openVikingClient) remove(
	ctx context.Context,
	identity Identity,
	uri string,
	recursive bool,
) error {
	client, err := c.sdk(identity)
	if err != nil {
		return err
	}
	return normalizeOpenVikingError(client.Remove(ctx, uri, &openviking.RemoveOptions{
		Recursive: recursive,
	}))
}

func (c *openVikingClient) deleteSession(
	ctx context.Context,
	identity Identity,
	sessionID string,
) error {
	client, err := c.sdk(identity)
	if err != nil {
		return err
	}
	return normalizeOpenVikingError(client.DeleteSession(ctx, sessionID))
}

type openVikingEnvelope struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func (c *openVikingClient) rawCall(
	ctx context.Context,
	identity Identity,
	method string,
	path string,
	body any,
	out any,
) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-OpenViking-Account", identity.openVikingAccount())
	req.Header.Set("X-OpenViking-User", identity.UserID)
	response, err := c.http.Do(req)
	if err != nil {
		return errors.New("OpenViking request failed: UNAVAILABLE")
	}
	defer response.Body.Close()
	var envelope openVikingEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return errors.New("OpenViking request failed: INVALID_RESPONSE")
	}
	if response.StatusCode == http.StatusNotFound ||
		(envelope.Error != nil && envelope.Error.Code == "NOT_FOUND") {
		return ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Status == "error" {
		code := "UNAVAILABLE"
		if envelope.Error != nil && envelope.Error.Code != "" {
			code = envelope.Error.Code
		}
		return fmt.Errorf("OpenViking request failed: %s", code)
	}
	if out == nil || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

type memoryResult struct {
	URI      string
	Abstract string
	Content  string
	Score    float64
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
