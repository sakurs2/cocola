package traceevent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Run struct {
	TraceID           string
	RootSpanID        string
	ConversationID    string
	ConversationTitle string
	UserID            string
	UserEmail         string
	Source            string
	ModelAlias        string
	Status            string
	StartedAt         time.Time
	CompletedAt       time.Time
	LastActivityAt    time.Time
	DurationMS        int64
	TTFTMS            int64
	LLMCallCount      int64
	ToolCallCount     int64
	InputTokens       int64
	OutputTokens      int64
	CacheTokens       int64
	ErrorCode         string
	SafeErrorSummary  string
	DetailStatus      string
}

type Span struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	SchemaVersion int
	Service       string
	Name          string
	Category      string
	StartedAt     time.Time
	DurationUS    int64
	Status        string
	Attributes    map[string]any
}

type Store interface {
	UpsertConversationRun(ctx context.Context, run Run) error
	UpsertConversationTraceSpan(ctx context.Context, span Span) error
	MarkConversationRunPartial(ctx context.Context, traceID string) error
}

type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

func NewSpanID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return hex.EncodeToString([]byte(time.Now().UTC().Format("150405.0")))[:16]
}

func (p *Postgres) UpsertConversationRun(ctx context.Context, run Run) error {
	if run.TraceID == "" {
		return nil
	}
	now := time.Now().UTC()
	if run.LastActivityAt.IsZero() {
		run.LastActivityAt = now
	}
	if run.DetailStatus == "" {
		run.DetailStatus = "available"
	}
	var completedAt any
	if !run.CompletedAt.IsZero() {
		completedAt = run.CompletedAt
	}
	const q = `INSERT INTO conversation_runs (
		trace_id, root_span_id, conversation_id, conversation_title, user_id, user_email,
		source, model_alias, status, started_at, completed_at, last_activity_at,
		duration_ms, ttft_ms, llm_call_count, tool_call_count, input_tokens,
		output_tokens, cache_tokens, error_code, safe_error_summary, detail_status
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
	ON CONFLICT (trace_id) DO UPDATE SET
		conversation_id=EXCLUDED.conversation_id, conversation_title=EXCLUDED.conversation_title,
		user_id=EXCLUDED.user_id, user_email=EXCLUDED.user_email, source=EXCLUDED.source,
		model_alias=EXCLUDED.model_alias, status=EXCLUDED.status,
		completed_at=EXCLUDED.completed_at, last_activity_at=EXCLUDED.last_activity_at,
		duration_ms=EXCLUDED.duration_ms, ttft_ms=EXCLUDED.ttft_ms,
		llm_call_count=GREATEST(conversation_runs.llm_call_count, EXCLUDED.llm_call_count),
		tool_call_count=EXCLUDED.tool_call_count,
		input_tokens=GREATEST(conversation_runs.input_tokens, EXCLUDED.input_tokens),
		output_tokens=GREATEST(conversation_runs.output_tokens, EXCLUDED.output_tokens),
		cache_tokens=GREATEST(conversation_runs.cache_tokens, EXCLUDED.cache_tokens),
		error_code=EXCLUDED.error_code, safe_error_summary=EXCLUDED.safe_error_summary,
		detail_status=CASE
			WHEN conversation_runs.detail_status = 'partial' THEN 'partial'
			ELSE EXCLUDED.detail_status
		END, updated_at=now()`
	_, err := p.pool.Exec(ctx, q, run.TraceID, run.RootSpanID, run.ConversationID,
		run.ConversationTitle, run.UserID, run.UserEmail, run.Source, run.ModelAlias,
		run.Status, run.StartedAt, completedAt, run.LastActivityAt, run.DurationMS,
		run.TTFTMS, run.LLMCallCount, run.ToolCallCount, run.InputTokens,
		run.OutputTokens, run.CacheTokens, run.ErrorCode, run.SafeErrorSummary, run.DetailStatus)
	return err
}

func (p *Postgres) MarkConversationRunPartial(ctx context.Context, traceID string) error {
	if traceID == "" {
		return nil
	}
	_, err := p.pool.Exec(ctx, `UPDATE conversation_runs
		SET detail_status = 'partial', last_activity_at = now(), updated_at = now()
		WHERE trace_id = $1`, traceID)
	return err
}

func (p *Postgres) UpsertConversationTraceSpan(ctx context.Context, span Span) error {
	if span.TraceID == "" || span.Name == "" {
		return nil
	}
	if span.SpanID == "" {
		span.SpanID = NewSpanID()
	}
	if span.SchemaVersion <= 0 {
		span.SchemaVersion = 1
	}
	if span.StartedAt.IsZero() {
		span.StartedAt = time.Now().UTC()
	}
	if span.Status == "" {
		span.Status = "success"
	}
	if span.Attributes == nil {
		span.Attributes = map[string]any{}
	}
	attributes, err := json.Marshal(span.Attributes)
	if err != nil {
		return err
	}
	const q = `INSERT INTO conversation_trace_spans (
		trace_id, span_id, parent_span_id, schema_version, service, name, category,
		started_at, duration_us, status, attributes_json
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	ON CONFLICT (trace_id, span_id) DO UPDATE SET
		parent_span_id=EXCLUDED.parent_span_id, service=EXCLUDED.service,
		name=EXCLUDED.name, category=EXCLUDED.category, started_at=EXCLUDED.started_at,
		duration_us=EXCLUDED.duration_us, status=EXCLUDED.status,
		attributes_json=EXCLUDED.attributes_json, updated_at=now()`
	_, err = p.pool.Exec(ctx, q, span.TraceID, span.SpanID, span.ParentSpanID,
		span.SchemaVersion, span.Service, span.Name, span.Category, span.StartedAt,
		span.DurationUS, span.Status, attributes)
	return err
}
