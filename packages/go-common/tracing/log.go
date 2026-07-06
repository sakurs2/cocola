package tracing

import (
	"context"

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
