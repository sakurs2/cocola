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
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}
