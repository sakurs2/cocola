package memory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openviking "github.com/volcengine/OpenViking/sdk/go"
)

var ErrNotFound = errors.New("memory: not found")

const minRecallScore = 0.35

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

// openVikingClient contains the complete OpenViking protocol boundary. The
// rest of Gateway only deals in Cocola memory concepts and durable job state.
type openVikingClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type openVikingTask struct {
	ID     string
	Status string
}

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

func (c *openVikingClient) searchMemories(
	ctx context.Context,
	identity Identity,
	query string,
	limit int,
) ([]memoryResult, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return nil, err
	}
	scoreThreshold := minRecallScore
	result, err := client.Search(ctx, query, &openviking.SearchOptions{
		TargetURI:      "viking://user/memories/",
		ContextType:    "memory",
		Limit:          limit,
		ScoreThreshold: &scoreThreshold,
	})
	if err != nil {
		return nil, normalizeOpenVikingError(err)
	}
	items := make([]memoryResult, 0, min(limit, len(result.Memories)))
	for _, item := range result.Memories {
		if len(items) >= limit {
			break
		}
		uri := normalizeMemoryURI(item.URI, identity.UserID)
		if item.Score < minRecallScore || !validItemURI(uri) {
			continue
		}
		items = append(items, memoryResult{
			URI: uri, Abstract: item.Abstract, Content: item.Overview, Score: item.Score,
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

func (c *openVikingClient) ensureSession(
	ctx context.Context,
	identity Identity,
	sessionID string,
) (int, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return 0, err
	}
	info, err := client.GetSession(ctx, sessionID, nil)
	if err == nil {
		return intValue(info["message_count"]), nil
	}
	if !openviking.IsCode(err, "NOT_FOUND") {
		return 0, normalizeOpenVikingError(err)
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
		info, getErr := client.GetSession(ctx, sessionID, nil)
		if getErr != nil {
			return 0, normalizeOpenVikingError(getErr)
		}
		return intValue(info["message_count"]), nil
	}
	return 0, normalizeOpenVikingError(err)
}

func (c *openVikingClient) addTurn(
	ctx context.Context,
	identity Identity,
	sessionID string,
	userText string,
	assistantText string,
	contexts []string,
) error {
	client, err := c.sdk(identity)
	if err != nil {
		return err
	}
	userContent := userText
	parts := []map[string]any{{"type": "text", "text": assistantText}}
	for _, uri := range contexts {
		if validItemURI(uri) {
			parts = append(parts, map[string]any{
				"type": "context", "uri": uri, "context_type": "memory",
			})
		}
	}
	_, err = client.BatchAddMessages(ctx, sessionID, []openviking.Message{
		{Role: "user", Content: &userContent},
		{Role: "assistant", Parts: parts},
	}, nil)
	return normalizeOpenVikingError(err)
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

func (c *openVikingClient) cancelTask(
	ctx context.Context,
	identity Identity,
	taskID string,
) (string, error) {
	client, err := c.sdk(identity)
	if err != nil {
		return "", err
	}
	result, err := client.CancelTask(ctx, taskID)
	if err != nil {
		return "", normalizeOpenVikingError(err)
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
		if task.ID != "" {
			return task, true, nil
		}
	}
	return openVikingTask{}, false, nil
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

func intValue(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}
