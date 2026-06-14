package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Init with Enabled=false must NOT stand up an exporter (no port bound, per
// <network_security>) yet must still install the W3C propagator so an inbound
// traceparent is honoured for log correlation. The returned stop is a no-op.
func TestInitDisabledSetsPropagatorOnly(t *testing.T) {
	otel.SetTextMapPropagator(nil)

	stop, err := Init(context.Background(), Config{Enabled: false, ServiceName: "t"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if stop == nil {
		t.Fatal("stop func is nil")
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	prop := otel.GetTextMapPropagator()
	if prop == nil {
		t.Fatal("propagator not installed")
	}
	if !contains(prop.Fields(), "traceparent") {
		t.Fatalf("propagator missing traceparent; got %v", prop.Fields())
	}
}

// A traceparent injected by an upstream service must round-trip through the
// global propagator, proving cross-service trace continuity even with export off.
func TestPropagatorRoundTripsTraceparent(t *testing.T) {
	if _, err := Init(context.Background(), Config{Enabled: false}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	prop := otel.GetTextMapPropagator()

	carrier := propagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	ctx := prop.Extract(context.Background(), carrier)
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		t.Fatal("extracted span context invalid")
	}
	if got := sc.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %s", got)
	}
}

// LogFields returns nil outside any span and the trace/span ids inside one.
func TestLogFields(t *testing.T) {
	if f := LogFields(context.Background()); f != nil {
		t.Fatalf("expected nil fields outside a span, got %v", f)
	}

	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	fields := LogFields(ctx)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].Key != "trace_id" || fields[0].String != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id field wrong: %+v", fields[0])
	}
	if fields[1].Key != "span_id" || fields[1].String != "00f067aa0ba902b7" {
		t.Fatalf("span_id field wrong: %+v", fields[1])
	}
}

// ConfigFromEnv defaults: off, localhost:4318, insecure, 5% sampling.
func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("COCOLA_OTEL_ENABLED", "")
	t.Setenv("COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("COCOLA_OTEL_EXPORTER_INSECURE", "")
	t.Setenv("COCOLA_OTEL_SAMPLER_RATIO", "")

	c := ConfigFromEnv("svc")
	if c.Enabled {
		t.Fatal("should default disabled")
	}
	if c.ServiceName != "svc" {
		t.Fatalf("service = %s", c.ServiceName)
	}
	if c.Endpoint != "localhost:4318" {
		t.Fatalf("endpoint = %s", c.Endpoint)
	}
	if !c.Insecure {
		t.Fatal("insecure should default true")
	}
	if c.SamplerRatio != 0.05 {
		t.Fatalf("ratio = %v", c.SamplerRatio)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
