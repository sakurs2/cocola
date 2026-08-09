// Package memory is the only Gateway module that knows OpenViking. Callers use
// its small service API; OpenViking request shapes and trusted identity headers
// never leak into chat or HTTP handlers.
package memory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cocola-project/cocola/packages/go-common/logger"
)

var ErrDisabled = errors.New("memory: disabled by administrator")
var ErrNotReady = errors.New("memory: configuration is not ready")

const (
	maxRecallItems = 6
	// Tokenization differs across routed models. Limiting UTF-8 bytes is a
	// conservative provider-independent upper bound for a 1600-token context
	// and avoids adding a tokenizer tied to one model family.
	maxRecallBytes = 1600
)

type Settings struct {
	GlobalEnabled bool  `json:"global_enabled"`
	UseEnabled    bool  `json:"use_enabled"`
	LearnEnabled  bool  `json:"learn_enabled"`
	Epoch         int64 `json:"-"`
}

type Item struct {
	ID       string  `json:"id"`
	URI      string  `json:"-"`
	Category string  `json:"category"`
	Title    string  `json:"title"`
	Abstract string  `json:"abstract,omitempty"`
	Content  string  `json:"content,omitempty"`
	Score    float64 `json:"score,omitempty"`
}

type ItemPage struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

const (
	RecallStatusRunning     = "running"
	RecallStatusSkipped     = "skipped"
	RecallStatusMiss        = "miss"
	RecallStatusHit         = "hit"
	RecallStatusDegraded    = "degraded"
	RecallStatusUnavailable = "unavailable"
)

// RecallResult contains the exact bounded context injected into the Agent.
// The chat orchestrator also persists that context for user-visible recall
// transparency; URIs are additionally retained as capture-job bookkeeping.
type RecallResult struct {
	Context   string
	URIs      []string
	Status    string
	Count     int
	ErrorCode string
}

type CaptureInput struct {
	RunID           string
	TenantID        string
	UserID          string
	ConversationID  string
	Source          string
	InteractionMode string
	ProjectID       string
	PlanID          string
	RecalledURIs    []string
}

type Config struct {
	OpenVikingURL          string
	OpenVikingRootAPIKey   string
	EmbeddingDimension     int
	RecallTimeout          time.Duration
	RecoveryScanInterval   time.Duration
	CaptureAttemptLimit    int
	CaptureMaxRetryHorizon time.Duration
	ClearTimeout           time.Duration
	Metrics                prometheus.Registerer
}

type Service struct {
	pool         *pgxpool.Pool
	client       *openVikingClient
	log          logger.Logger
	cfg          Config
	wake         chan struct{}
	workerCtx    context.Context
	cancelWorker context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
	metrics      serviceMetrics
}

func New(ctx context.Context, dsn string, cfg Config, log logger.Logger) (*Service, error) {
	if strings.TrimSpace(cfg.OpenVikingURL) == "" {
		return nil, fmt.Errorf("memory: OpenViking URL is required")
	}
	if strings.TrimSpace(cfg.OpenVikingRootAPIKey) == "" {
		return nil, fmt.Errorf("memory: OpenViking root API key is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if cfg.EmbeddingDimension <= 0 {
		cfg.EmbeddingDimension = 1024
	}
	if cfg.RecallTimeout <= 0 {
		cfg.RecallTimeout = 1500 * time.Millisecond
	}
	if cfg.RecoveryScanInterval <= 0 {
		cfg.RecoveryScanInterval = time.Minute
	}
	if cfg.CaptureAttemptLimit <= 0 {
		cfg.CaptureAttemptLimit = 5
	}
	if cfg.CaptureMaxRetryHorizon <= 0 {
		cfg.CaptureMaxRetryHorizon = 6 * time.Hour
	}
	if cfg.ClearTimeout <= 0 {
		cfg.ClearTimeout = 30 * time.Second
	}
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	service := &Service{
		pool: pool, client: newOpenVikingClient(cfg.OpenVikingURL, cfg.OpenVikingRootAPIKey),
		log: log, cfg: cfg, wake: make(chan struct{}, 1), workerCtx: workerCtx,
		cancelWorker: cancelWorker, done: make(chan struct{}),
		metrics: newServiceMetrics(cfg.Metrics),
	}
	go service.worker()
	return service, nil
}

func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.cancelWorker()
		<-s.done
		s.client.close()
		s.pool.Close()
	})
}

func (s *Service) Ready(ctx context.Context) error {
	return s.client.ready(ctx)
}

func (s *Service) enabled(ctx context.Context) (bool, error) {
	var enabled bool
	var modelsReady bool
	var dimension int
	var embeddingRouteID string
	var embeddingProviderID string
	var embeddingModel string
	var embeddingBaseURL string
	err := s.pool.QueryRow(ctx, `SELECT c.enabled,
		COALESCE(extraction.enabled, FALSE)
			AND COALESCE(extraction_provider.enabled, FALSE)
			AND extraction.protocol IN ('anthropic-messages', 'openai-responses')
			AND extraction_provider.type IN ('anthropic', 'openai_responses')
			AND COALESCE(embedding.enabled, FALSE)
			AND COALESCE(embedding_provider.enabled, FALSE)
			AND embedding.protocol = 'openai-embeddings'
			AND embedding_provider.type = 'openai_embeddings',
		COALESCE(embedding.embedding_dimension, 0),
		COALESCE(c.embedding_model_route_id, ''),
		COALESCE(embedding.provider_id, ''),
		COALESCE(embedding.real_model, ''),
		COALESCE(embedding_provider.base_url, '')
		FROM memory_config c
		LEFT JOIN llm_model_routes extraction ON extraction.id=c.extraction_model_route_id
		LEFT JOIN llm_providers extraction_provider ON extraction_provider.id=extraction.provider_id
		LEFT JOIN llm_model_routes embedding ON embedding.id=c.embedding_model_route_id
		LEFT JOIN llm_providers embedding_provider ON embedding_provider.id=embedding.provider_id
		WHERE c.singleton=TRUE`).Scan(
		&enabled, &modelsReady, &dimension, &embeddingRouteID,
		&embeddingProviderID, &embeddingModel, &embeddingBaseURL,
	)
	if err != nil || !enabled {
		return enabled, err
	}
	if !modelsReady {
		return false, ErrNotReady
	}
	if dimension != s.cfg.EmbeddingDimension {
		return false, fmt.Errorf("%w: embedding dimension mismatch", ErrNotReady)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO memory_index_state
		(singleton, embedding_dimension, embedding_model_route_id,
		 embedding_provider_id, embedding_model, embedding_base_url)
		VALUES (TRUE, $1, $2, $3, $4, $5) ON CONFLICT (singleton) DO NOTHING`,
		dimension, embeddingRouteID, embeddingProviderID,
		strings.TrimSpace(embeddingModel), strings.TrimRight(strings.TrimSpace(embeddingBaseURL), "/"))
	if err != nil {
		return false, err
	}
	var locked int
	var lockedRouteID string
	var lockedProviderID string
	var lockedModel string
	var lockedBaseURL string
	if err := s.pool.QueryRow(ctx, `SELECT embedding_dimension, embedding_model_route_id,
		embedding_provider_id, embedding_model, embedding_base_url
		FROM memory_index_state WHERE singleton=TRUE`).Scan(
		&locked, &lockedRouteID, &lockedProviderID, &lockedModel, &lockedBaseURL,
	); err != nil {
		return false, err
	}
	if locked != dimension || lockedRouteID != embeddingRouteID ||
		lockedProviderID != embeddingProviderID || lockedModel != strings.TrimSpace(embeddingModel) ||
		lockedBaseURL != strings.TrimRight(strings.TrimSpace(embeddingBaseURL), "/") {
		return false, fmt.Errorf("%w: embedding route is locked", ErrNotReady)
	}
	return true, nil
}

func (s *Service) GetSettings(ctx context.Context, identity Identity) (Settings, error) {
	settings := Settings{UseEnabled: true, LearnEnabled: true}
	global, err := s.enabled(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.GlobalEnabled = global
	err = s.pool.QueryRow(ctx, `SELECT use_enabled, learn_enabled, epoch
		FROM memory_user_settings WHERE tenant_id=$1 AND user_id=$2`,
		identity.TenantID, identity.UserID).Scan(
		&settings.UseEnabled, &settings.LearnEnabled, &settings.Epoch,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings, nil
	}
	return settings, err
}

func (s *Service) UpdateSettings(
	ctx context.Context,
	identity Identity,
	useEnabled bool,
	learnEnabled bool,
) (Settings, error) {
	global, err := s.enabled(ctx)
	if err != nil {
		return Settings{}, err
	}
	if !global {
		return Settings{}, ErrDisabled
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO memory_user_settings
		(tenant_id, user_id, use_enabled, learn_enabled, updated_at)
		VALUES ($1,$2,$3,$4,now())
		ON CONFLICT (tenant_id,user_id) DO UPDATE SET
		use_enabled=EXCLUDED.use_enabled, learn_enabled=EXCLUDED.learn_enabled, updated_at=now()`,
		identity.TenantID, identity.UserID, useEnabled, learnEnabled)
	if err != nil {
		return Settings{}, err
	}
	return s.GetSettings(ctx, identity)
}

func (s *Service) Recall(
	ctx context.Context,
	identity Identity,
	query string,
	onStart func(),
) RecallResult {
	startedAt := time.Now()
	defer func() { s.metrics.observeRecall(time.Since(startedAt)) }()
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RecallTimeout)
	defer cancel()
	settings, err := s.GetSettings(ctx, identity)
	if err != nil {
		s.metrics.recall("error")
		return RecallResult{
			Status: RecallStatusUnavailable, ErrorCode: recallErrorCode(err),
		}
	}
	if !beginRecall(settings, onStart) {
		s.metrics.recall("skipped")
		return RecallResult{Status: RecallStatusSkipped}
	}
	items, searchErr := s.client.searchMemories(ctx, identity, query, maxRecallItems)
	result := buildRecallResult(items, searchErr)
	s.metrics.recall(result.Status)
	return result
}

// beginRecall notifies the caller only after both the administrator and user
// have allowed recall. This keeps disabled conversations free of transient UI
// progress while avoiding a duplicate settings query in the chat orchestrator.
func beginRecall(settings Settings, onStart func()) bool {
	if !settings.GlobalEnabled || !settings.UseEnabled {
		return false
	}
	if onStart != nil {
		onStart()
	}
	return true
}

func buildRecallResult(items []memoryResult, searchErr error) RecallResult {
	formatted, uris := formatRecall(items)
	result := RecallResult{Context: formatted, URIs: uris, Count: len(uris)}
	if searchErr != nil {
		result.Status = RecallStatusUnavailable
		result.ErrorCode = recallErrorCode(searchErr)
		return result
	}
	if formatted == "" {
		result.Status = RecallStatusMiss
	} else {
		result.Status = RecallStatusHit
	}
	return result
}

func recallErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "MEMORY_RECALL_TIMEOUT"
	case errors.Is(err, ErrNotReady):
		return "MEMORY_NOT_READY"
	default:
		return "MEMORY_UNAVAILABLE"
	}
}

func formatRecall(items []memoryResult) (string, []string) {
	var builder strings.Builder
	remaining := maxRecallBytes
	count := 0
	uris := make([]string, 0, maxRecallItems)
	appendBlock := func(label, uri, content string) bool {
		content = strings.TrimSpace(content)
		if content == "" || remaining <= 0 || count >= maxRecallItems {
			return false
		}
		separator := ""
		if builder.Len() > 0 {
			separator = "\n\n"
		}
		if len(separator) >= remaining {
			return false
		}
		// URI is bookkeeping for OpenViking ContextParts, never user-facing
		// recall content. Keep the injected summary readable and avoid leaking
		// provider-internal identifiers into the conversation card.
		block := label + ":\n" + content
		blockBudget := remaining - len(separator)
		if len(block) > blockBudget {
			block = truncateUTF8Bytes(block, blockBudget)
		}
		builder.WriteString(separator)
		builder.WriteString(block)
		remaining -= len(separator) + len(block)
		count++
		if uri != "" {
			uris = append(uris, uri)
		}
		return true
	}
	for _, item := range items {
		content := item.Content
		if content == "" {
			content = item.Abstract
		}
		appendBlock(memoryLabel(item.URI), item.URI, content)
		if remaining <= 0 {
			break
		}
	}
	return builder.String(), uris
}

func memoryLabel(uri string) string {
	switch {
	case strings.HasSuffix(uri, "/profile.md"):
		return "User profile"
	case strings.Contains(uri, "/preferences/"):
		return "User preference"
	case strings.Contains(uri, "/entities/"):
		return "Relevant entity"
	case strings.Contains(uri, "/events/"):
		return "Relevant event"
	default:
		return "Relevant memory"
	}
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s *Service) ScheduleCapture(ctx context.Context, input CaptureInput) error {
	if reason := captureSkipReason(input); reason != "" {
		s.metrics.capture(reason)
		return nil
	}
	recalled, err := json.Marshal(input.RecalledURIs)
	if err != nil {
		s.metrics.capture("error")
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.metrics.capture("error")
		return err
	}
	defer tx.Rollback(ctx)

	// Serialize enqueue with Admin disable and user Clear. This guarantees that
	// a job belongs to exactly one memory epoch and cannot slip in after either
	// operation has completed.
	var globalEnabled bool
	var modelsReady bool
	if err := tx.QueryRow(ctx, `SELECT c.enabled,
		COALESCE(extraction.enabled, FALSE)
			AND COALESCE(extraction_provider.enabled, FALSE)
			AND extraction.protocol IN ('anthropic-messages', 'openai-responses')
			AND extraction_provider.type IN ('anthropic', 'openai_responses')
			AND COALESCE(embedding.enabled, FALSE)
			AND COALESCE(embedding_provider.enabled, FALSE)
			AND embedding.protocol = 'openai-embeddings'
			AND embedding_provider.type = 'openai_embeddings'
			AND embedding.embedding_dimension=$1
		FROM memory_config c
		LEFT JOIN llm_model_routes extraction ON extraction.id=c.extraction_model_route_id
		LEFT JOIN llm_providers extraction_provider ON extraction_provider.id=extraction.provider_id
		LEFT JOIN llm_model_routes embedding ON embedding.id=c.embedding_model_route_id
		LEFT JOIN llm_providers embedding_provider ON embedding_provider.id=embedding.provider_id
		WHERE c.singleton=TRUE FOR SHARE OF c`, s.cfg.EmbeddingDimension).
		Scan(&globalEnabled, &modelsReady); err != nil {
		s.metrics.capture("error")
		return err
	}
	if !globalEnabled {
		s.metrics.capture("skipped_disabled")
		return nil
	}
	if !modelsReady {
		s.metrics.capture("skipped_not_ready")
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memory_user_settings (tenant_id,user_id)
		VALUES ($1,$2) ON CONFLICT (tenant_id,user_id) DO NOTHING`,
		input.TenantID, input.UserID); err != nil {
		s.metrics.capture("error")
		return err
	}
	var learnEnabled bool
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT learn_enabled, epoch FROM memory_user_settings
		WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE`, input.TenantID, input.UserID).
		Scan(&learnEnabled, &epoch); err != nil {
		s.metrics.capture("error")
		return err
	}
	if !learnEnabled {
		s.metrics.capture("skipped_user")
		return nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO memory_capture_jobs
		(run_id, tenant_id, user_id, conversation_id, epoch, recalled_uris,
		 provider_session_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (run_id) DO NOTHING`, input.RunID, input.TenantID, input.UserID,
		input.ConversationID, epoch, recalled, "cocola-"+input.RunID)
	if err != nil {
		s.metrics.capture("error")
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		s.metrics.capture("error")
		return err
	}
	if tag.RowsAffected() > 0 {
		s.metrics.capture("scheduled")
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// captureSkipReason keeps product policy at the Memory boundary. Callers also
// avoid scheduling these runs, but correctness does not depend on that call
// order: scheduled work, planning flows, and project workspaces are never
// persisted as personal long-term memory.
func captureSkipReason(input CaptureInput) string {
	switch {
	case input.Source == "scheduled_task":
		return "skipped_scheduled"
	case input.InteractionMode == "plan" || strings.TrimSpace(input.PlanID) != "":
		return "skipped_plan"
	case strings.TrimSpace(input.ProjectID) != "":
		return "skipped_project"
	default:
		return ""
	}
}

func (s *Service) ListItems(
	ctx context.Context,
	identity Identity,
	category string,
	cursor string,
	limit int,
) (ItemPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	root, err := memoryRoot(category)
	if err != nil {
		return ItemPage{}, err
	}
	if strings.TrimSpace(category) == "profile" {
		content, readErr := s.client.read(ctx, identity, root)
		if errors.Is(readErr, ErrNotFound) {
			return ItemPage{Items: []Item{}}, nil
		}
		if readErr != nil {
			return ItemPage{}, readErr
		}
		item := itemFromURI(root)
		item.Content = content
		return ItemPage{Items: []Item{item}}, nil
	}
	// Query category directories directly. Filtering a globally truncated list
	// could otherwise hide an entire category once another category grows.
	raw, err := s.client.list(ctx, identity, root)
	if errors.Is(err, ErrNotFound) {
		return ItemPage{Items: []Item{}}, nil
	}
	if err != nil {
		return ItemPage{}, err
	}
	items := collectItems(raw, identity.UserID)
	category = strings.TrimSpace(category)
	if category != "" && category != "all" {
		filtered := items[:0]
		for _, item := range items {
			if item.Category == category {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	sort.Slice(items, func(i, j int) bool { return items[i].URI < items[j].URI })
	start := 0
	if cursor != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil {
			return ItemPage{}, fmt.Errorf("invalid cursor")
		}
		for start < len(items) && items[start].URI <= string(decoded) {
			start++
		}
	}
	end := min(start+limit, len(items))
	page := ItemPage{Items: items[start:end]}
	if end < len(items) && end > start {
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(items[end-1].URI))
	}
	return page, nil
}

func (s *Service) GetItem(ctx context.Context, identity Identity, opaqueID string) (Item, error) {
	uri, err := decodeItemID(opaqueID)
	if err != nil {
		return Item{}, err
	}
	content, err := s.client.read(ctx, identity, uri)
	if err != nil {
		return Item{}, err
	}
	item := itemFromURI(uri)
	item.Content = content
	return item, nil
}

func (s *Service) DeleteItem(ctx context.Context, identity Identity, opaqueID string) error {
	uri, err := decodeItemID(opaqueID)
	if err != nil {
		return err
	}
	return s.client.remove(ctx, identity, uri, false)
}

func (s *Service) Clear(ctx context.Context, identity Identity) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ClearTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var newEpoch int64
	err = tx.QueryRow(ctx, `INSERT INTO memory_user_settings (tenant_id,user_id,epoch)
		VALUES ($1,$2,1) ON CONFLICT (tenant_id,user_id) DO UPDATE SET
		epoch=memory_user_settings.epoch+1, updated_at=now()
		RETURNING epoch`, identity.TenantID, identity.UserID).Scan(&newEpoch)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE memory_capture_jobs
		SET cancellation_requested=TRUE,
			status=CASE WHEN status='submitting' THEN status ELSE 'cancel_requested' END,
			next_attempt_at=CASE WHEN status='submitting' THEN next_attempt_at ELSE now() END,
			updated_at=now()
		WHERE tenant_id=$1 AND user_id=$2 AND epoch<$3
			AND status IN ('pending','processing','submitting')`,
		identity.TenantID, identity.UserID, newEpoch)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if err := s.waitForCaptureCancellation(ctx, identity, newEpoch); err != nil {
		return err
	}
	if err := s.client.remove(ctx, identity, "viking://user/memories/", true); err != nil &&
		!errors.Is(err, ErrNotFound) {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT provider_session_id
		FROM memory_capture_jobs
		WHERE tenant_id=$1 AND user_id=$2 AND epoch<$3 AND provider_session_id<>''`,
		identity.TenantID, identity.UserID, newEpoch)
	if err != nil {
		return err
	}
	sessions := make([]string, 0)
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return err
		}
		sessions = append(sessions, sessionID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, sessionID := range sessions {
		if err := s.client.deleteSession(ctx, identity, sessionID); err != nil &&
			!errors.Is(err, ErrNotFound) {
			return err
		}
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM memory_capture_jobs
		WHERE tenant_id=$1 AND user_id=$2 AND epoch<$3`,
		identity.TenantID, identity.UserID, newEpoch)
	return err
}

func (s *Service) waitForCaptureCancellation(
	ctx context.Context,
	identity Identity,
	newEpoch int64,
) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var remaining int
		err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_capture_jobs
			WHERE tenant_id=$1 AND user_id=$2 AND epoch<$3
				AND cancellation_requested
				AND status IN ('pending','processing','submitting','cancel_requested')`,
			identity.TenantID, identity.UserID, newEpoch).Scan(&remaining)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return nil
		}
		select {
		case s.wake <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("memory clear timed out while cancelling capture jobs: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *Service) cancelAndWait(ctx context.Context, identity Identity, taskID string) error {
	status, err := s.client.cancelTask(ctx, identity, taskID)
	if err != nil {
		status, err = s.client.taskStatus(ctx, identity, taskID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	for !terminalTaskStatus(status) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		status, err = s.client.taskStatus(ctx, identity, taskID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func terminalTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "completed", "success", "succeeded", "failed", "error":
		return true
	default:
		return false
	}
}

func memoryRoot(category string) (string, error) {
	switch strings.TrimSpace(category) {
	case "", "all":
		return "viking://user/memories/", nil
	case "profile":
		return "viking://user/memories/profile.md", nil
	case "preferences", "entities", "events":
		return "viking://user/memories/" + category + "/", nil
	default:
		return "", fmt.Errorf("invalid memory category")
	}
}

func collectItems(raw any, userID string) []Item {
	items := make([]Item, 0)
	seen := make(map[string]struct{})
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				walk(child)
			}
		case map[string]any:
			uri := stringValue(node["uri"])
			if uri == "" {
				uri = stringValue(node["path"])
			}
			uri = normalizeMemoryURI(uri, userID)
			if validItemURI(uri) && !strings.HasSuffix(uri, "/") &&
				!strings.Contains(uri, "/.") {
				if _, exists := seen[uri]; !exists {
					seen[uri] = struct{}{}
					item := itemFromURI(uri)
					item.Abstract = stringValue(node["abstract"])
					items = append(items, item)
				}
			}
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(raw)
	return items
}

func normalizeMemoryURI(uri, userID string) string {
	if strings.HasPrefix(uri, "viking://user/memories/") {
		return uri
	}
	prefix := "viking://user/" + url.PathEscape(userID) + "/memories/"
	if strings.HasPrefix(uri, prefix) {
		return "viking://user/memories/" + strings.TrimPrefix(uri, prefix)
	}
	return ""
}

func itemFromURI(uri string) Item {
	trimmed := strings.TrimSuffix(uri, "/")
	parts := strings.Split(trimmed, "/")
	title := parts[len(parts)-1]
	category := "profile"
	if len(parts) >= 2 && parts[len(parts)-2] != "memories" {
		category = parts[len(parts)-2]
	}
	return Item{
		ID: base64.RawURLEncoding.EncodeToString([]byte(uri)), URI: uri,
		Category: category, Title: title,
	}
}

func decodeItemID(opaqueID string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(opaqueID)
	if err != nil || !validItemURI(string(raw)) {
		return "", fmt.Errorf("invalid memory item id")
	}
	return string(raw), nil
}

func validItemURI(uri string) bool {
	if uri == "viking://user/memories/profile.md" {
		return true
	}
	for _, category := range []string{"preferences", "entities", "events"} {
		prefix := "viking://user/memories/" + category + "/"
		if strings.HasPrefix(uri, prefix) {
			relative := strings.TrimPrefix(uri, prefix)
			decoded, err := url.PathUnescape(relative)
			if err != nil || decoded == "" || strings.HasSuffix(decoded, "/") ||
				strings.Contains(decoded, "..") || strings.ContainsAny(decoded, "\\?#") {
				return false
			}
			for _, segment := range strings.Split(decoded, "/") {
				if segment == "" || strings.HasPrefix(segment, ".") {
					return false
				}
			}
			return true
		}
	}
	return false
}
