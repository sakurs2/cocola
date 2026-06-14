// Package tracing is cocola's shared OpenTelemetry bootstrap. Every Go service
// (gateway, sandbox-manager, admin-api) initialises distributed tracing from
// here so span export, sampling, and W3C context propagation are identical
// across the fleet — and identical in spirit to the Python side (py-common's
// tracing helper), so one trace stitches gateway -> agent-runtime ->
// sandbox-manager -> llm-gateway across three languages.
//
// Design (ADR-0011, M8):
//   - Reuse, don't reinvent: this is a thin wrapper over the upstream
//     go.opentelemetry.io/otel SDK + the OTLP/HTTP exporter and the standard
//     contrib instrumentation (otelhttp / otelgrpc). We add only env wiring and
//     sane defaults.
//   - OFF by default. Init is a no-op unless COCOLA_OTEL_ENABLED is truthy, so
//     zero-config deployments pay nothing (no exporter goroutine, no sampling
//     overhead) and behaviour matches pre-M8 exactly.
//   - Low default sampling (COCOLA_OTEL_SAMPLER_RATIO, default 0.05) with a
//     ParentBased sampler, so a sampled trace stays sampled end to end.
//   - OTLP over HTTP (not gRPC) on purpose: it keeps the google.golang.org/grpc
//     version off the exporter's critical path and is the simplest Collector
//     ingress to stand up locally (Tempo/OTel Collector both accept :4318).
//   - W3C TraceContext + Baggage propagators are set globally so the contrib
//     instrumentation injects/extracts traceparent without any per-call code.
package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config controls tracing init. Zero value (Enabled=false) is a safe no-op.
type Config struct {
	Enabled      bool
	ServiceName  string
	Endpoint     string  // OTLP/HTTP endpoint host:port, e.g. "localhost:4318"
	Insecure     bool    // use http:// (true) vs https:// (false) to the collector
	SamplerRatio float64 // 0..1; ParentBased(TraceIDRatioBased)
}

// ConfigFromEnv reads the COCOLA_OTEL_* knobs. service is the logical service
// name stamped onto every span's resource.
//
//	COCOLA_OTEL_ENABLED                "1"/"true" to enable (default off)
//	COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT host:port of the OTLP/HTTP collector
//	                                   (default "localhost:4318")
//	COCOLA_OTEL_EXPORTER_INSECURE      "1"/"true" => plaintext http (default true)
//	COCOLA_OTEL_SAMPLER_RATIO          float 0..1 (default 0.05)
func ConfigFromEnv(service string) Config {
	return Config{
		Enabled:      truthy(os.Getenv("COCOLA_OTEL_ENABLED")),
		ServiceName:  service,
		Endpoint:     envOr("COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318"),
		Insecure:     envOrBool("COCOLA_OTEL_EXPORTER_INSECURE", true),
		SamplerRatio: envOrFloat("COCOLA_OTEL_SAMPLER_RATIO", 0.05),
	}
}

// Init installs a global TracerProvider and the W3C propagator according to cfg
// and returns a stop function the caller defers. When cfg.Enabled is false it
// sets only the propagator (so an inbound traceparent is still honoured for log
// correlation) and returns a no-op stop — no exporter, no batcher.
func Init(ctx context.Context, cfg Config) (stop func(context.Context) error, err error) {
	// Always set propagation so trace_id flows through even when we are not the
	// service that exports spans; this is what lets logs correlate cheaply.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		attribute.String("service.namespace", "cocola"),
	))
	if err != nil {
		return nil, fmt.Errorf("tracing: resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envOrBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return truthy(v)
}

func envOrFloat(k string, def float64) float64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
