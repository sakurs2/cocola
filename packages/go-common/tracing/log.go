package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// LogFields returns the trace correlation fields for the span in ctx, ready to
// pass to a zap logger: trace_id and span_id. When ctx carries no recording
// span (tracing disabled, or outside any span) it returns nil, so callers can
// unconditionally splat it:
//
//	log.Info("handled", tracing.LogFields(ctx)...)
//
// This is the seam that lets a single user request be grepped across the
// gateway/agent-runtime/sandbox-manager/llm-gateway logs by trace_id, even when
// span export to a backend is turned off.
func LogFields(ctx context.Context) []zap.Field {
	traceID, spanID := IDs(ctx)
	if traceID == "" {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", traceID),
		zap.String("span_id", spanID),
	}
}

// IDs returns the active trace/span ids in ctx. Empty strings mean no valid span
// context is present.
func IDs(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

// TraceID returns the active OTel trace id when one exists, otherwise a fresh
// internal 16-byte hex id. Use it for in-product correlation records that should
// exist even when external OpenTelemetry export is disabled.
func TraceID(ctx context.Context) string {
	traceID, _ := IDs(ctx)
	if traceID != "" {
		return traceID
	}
	return NewTraceID()
}

// NewTraceID returns a lowercase 32-character hex trace id.
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	// crypto/rand failures are vanishingly rare; keep correlation alive anyway.
	return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))[:32]
}
