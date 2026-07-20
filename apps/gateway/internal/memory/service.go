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
	maxRecallChars = 6000
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

// RecallResult is the secret-free outcome exposed to the chat orchestrator.
// Context is sent only to the Agent Runtime; the UI event uses Status, Count,
// and ErrorCode and never receives memory text or OpenViking identifiers.
type RecallResult struct {
	Context   string
	URIs      []string
	Status    string
	Count     int
	ErrorCode string
}

type CaptureInput struct {
	RunID          string
	TenantID       string
	UserID         string
	ConversationID string
	Source         string
	RecalledURIs   []string
}

type Config struct {
	OpenVikingURL          string
	OpenVikingRootAPIKey   string
	EmbeddingDimension     int
	RecallTimeout          time.Duration
	RecoveryScanInterval   time.Duration
	CaptureAttemptLimit    int
	CaptureMaxRetryHorizon time.Duration
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
		cfg.CaptureAttemptLimit = 8
	}
	if cfg.CaptureMaxRetryHorizon <= 0 {
		cfg.CaptureMaxRetryHorizon = 24 * time.Hour
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
	err := s.pool.QueryRow(ctx, `SELECT c.enabled,
		COALESCE(extraction.enabled, FALSE)
			AND COALESCE(extraction_provider.enabled, FALSE)
			AND extraction.protocol IN ('anthropic-messages', 'openai-responses')
			AND extraction_provider.type IN ('anthropic', 'openai_responses')
			AND COALESCE(embedding.enabled, FALSE)
			AND COALESCE(embedding_provider.enabled, FALSE)
			AND embedding.protocol = 'openai-embeddings'
			AND embedding_provider.type = 'openai_embeddings',
		COALESCE(embedding.embedding_dimension, 0)
		FROM memory_config c
		LEFT JOIN llm_model_routes extraction ON extraction.id=c.extraction_model_route_id
		LEFT JOIN llm_providers extraction_provider ON extraction_provider.id=extraction.provider_id
		LEFT JOIN llm_model_routes embedding ON embedding.id=c.embedding_model_route_id
		LEFT JOIN llm_providers embedding_provider ON embedding_provider.id=embedding.provider_id
		WHERE c.singleton=TRUE`).Scan(&enabled, &modelsReady, &dimension)
	if err != nil || !enabled {
		return enabled, err
	}
	if !modelsReady {
		return false, ErrNotReady
	}
	if dimension != s.cfg.EmbeddingDimension {
		return false, fmt.Errorf("%w: embedding dimension mismatch", ErrNotReady)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO memory_index_state (singleton, embedding_dimension)
		VALUES (TRUE, $1) ON CONFLICT (singleton) DO NOTHING`, dimension)
	if err != nil {
		return false, err
	}
	var locked int
	if err := s.pool.QueryRow(ctx, `SELECT embedding_dimension FROM memory_index_state
		WHERE singleton=TRUE`).Scan(&locked); err != nil {
		return false, err
	}
	if locked != dimension {
		return false, fmt.Errorf("%w: index dimension is locked", ErrNotReady)
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
) RecallResult {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RecallTimeout)
	defer cancel()
	settings, err := s.GetSettings(ctx, identity)
	if err != nil {
		s.metrics.recall("error")
		return RecallResult{
			Status: RecallStatusUnavailable, ErrorCode: recallErrorCode(err),
		}
	}
	if !settings.GlobalEnabled || !settings.UseEnabled {
		s.metrics.recall("skipped")
		return RecallResult{Status: RecallStatusSkipped}
	}
	type profileResult struct {
		text string
		err  error
	}
	type findResult struct {
		items []memoryResult
		err   error
	}
	profileCh := make(chan profileResult, 1)
	findCh := make(chan findResult, 1)
	go func() {
		profile, profileErr := s.client.read(
			ctx, identity, "viking://user/memories/profile.md",
		)
		profileCh <- profileResult{text: profile, err: profileErr}
	}()
	go func() {
		items, findErr := s.client.find(ctx, identity, query, maxRecallItems)
		findCh <- findResult{items: items, err: findErr}
	}()

	profile := <-profileCh
	found := <-findCh
	result := buildRecallResult(profile.text, found.items, profile.err, found.err)
	s.metrics.recall(result.Status)
	return result
}

func buildRecallResult(
	profile string,
	items []memoryResult,
	profileErr error,
	findErr error,
) RecallResult {
	if errors.Is(profileErr, ErrNotFound) {
		profileErr = nil
	}
	if profileErr != nil {
		profile = ""
	}
	if findErr != nil {
		items = nil
	}
	formatted, uris := formatRecall(profile, items)
	result := RecallResult{Context: formatted, URIs: uris, Count: len(uris)}
	if profileErr != nil || findErr != nil {
		result.ErrorCode = recallErrorCode(errors.Join(profileErr, findErr))
		if formatted == "" {
			result.Status = RecallStatusUnavailable
		} else {
			result.Status = RecallStatusDegraded
		}
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

func formatRecall(profile string, items []memoryResult) (string, []string) {
	var builder strings.Builder
	remaining := maxRecallChars
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
		if utf8.RuneCountInString(separator) >= remaining {
			return false
		}
		block := label + ":\n" + content
		if uri != "" {
			block = label + " (" + uri + "):\n" + content
		}
		blockBudget := remaining - utf8.RuneCountInString(separator)
		if utf8.RuneCountInString(block) > blockBudget {
			block = truncateRunes(block, blockBudget)
		}
		builder.WriteString(separator)
		builder.WriteString(block)
		remaining -= utf8.RuneCountInString(separator) + utf8.RuneCountInString(block)
		count++
		if uri != "" {
			uris = append(uris, uri)
		}
		return true
	}
	appendBlock("User profile", "viking://user/memories/profile.md", profile)
	for _, item := range items {
		content := item.Content
		if content == "" {
			content = item.Abstract
		}
		appendBlock("Relevant memory", item.URI, content)
		if remaining <= 0 {
			break
		}
	}
	return builder.String(), uris
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (s *Service) ScheduleCapture(ctx context.Context, input CaptureInput) error {
	if input.Source == "scheduled_task" {
		s.metrics.capture("skipped_scheduled")
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
		 openviking_session_id)
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
	raw, err := s.client.list(ctx, identity, root)
	if errors.Is(err, ErrNotFound) {
		return ItemPage{Items: []Item{}}, nil
	}
	if err != nil {
		return ItemPage{}, err
	}
	items := collectItems(raw)
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO memory_user_settings (tenant_id,user_id,epoch)
		VALUES ($1,$2,1) ON CONFLICT (tenant_id,user_id) DO UPDATE SET
		epoch=memory_user_settings.epoch+1, updated_at=now()`, identity.TenantID, identity.UserID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT openviking_session_id FROM memory_capture_jobs
		WHERE tenant_id=$1 AND user_id=$2 AND openviking_session_id<>''`,
		identity.TenantID, identity.UserID)
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
	_, err = tx.Exec(ctx, `UPDATE memory_capture_jobs SET status='cancelled', updated_at=now()
		WHERE tenant_id=$1 AND user_id=$2 AND status IN ('pending','submitted','retry')`,
		identity.TenantID, identity.UserID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.client.remove(ctx, identity, "viking://user/memories/", true); err != nil &&
		!errors.Is(err, ErrNotFound) {
		return err
	}
	for _, sessionID := range sessions {
		if err := s.client.deleteSession(ctx, identity, sessionID); err != nil &&
			!errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
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

func collectItems(raw any) []Item {
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
