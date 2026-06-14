package tracing

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
)

// HTTPHandler wraps an http.Handler with the upstream otelhttp server
// instrumentation: it extracts an inbound W3C traceparent, starts a server span
// named `operation`, and puts the span context into the request context so
// downstream handlers (and tracing.LogFields) see the trace_id. When tracing is
// disabled the global TracerProvider is the no-op provider, so this adds only a
// cheap context lookup and no spans are exported.
//
// Reuses go.opentelemetry.io/contrib rather than hand-rolling span lifecycle —
// the wrapper exists only to give services a one-liner and keep the otel import
// surface in one place.
func HTTPHandler(operation string, next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, operation)
}

// GRPCServerStatsHandler returns a grpc.ServerOption that installs otelgrpc's
// stats handler, so the server extracts inbound traceparent and emits a span per
// RPC. Compose it alongside the metrics interceptors in grpc.NewServer.
func GRPCServerStatsHandler() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}

// GRPCClientDialOption returns a grpc.DialOption that installs otelgrpc's client
// stats handler, so an outbound RPC injects the current span's traceparent into
// gRPC metadata — this is what carries the trace from the gateway into
// agent-runtime, and from agent-runtime into sandbox-manager.
func GRPCClientDialOption() grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler())
}
