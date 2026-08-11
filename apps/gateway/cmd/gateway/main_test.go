package main

import (
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
)

func TestBoundedEnvInt(t *testing.T) {
	t.Setenv("COCOLA_TEST_BOUND", "200")
	value, err := boundedEnvInt("COCOLA_TEST_BOUND", 100, 1, 1000)
	if err != nil || value != 200 {
		t.Fatalf("bounded value = %d, %v; want 200", value, err)
	}

	t.Setenv("COCOLA_TEST_BOUND", "1001")
	if _, err := boundedEnvInt("COCOLA_TEST_BOUND", 100, 1, 1000); err == nil {
		t.Fatal("out-of-range value should fail")
	}
}

func TestProductConfigFromEnv(t *testing.T) {
	runtimes := []agent.Runtime{{ID: "claude-code"}}

	config, err := productConfigFromEnv(runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentRuntime.DefaultID != "claude-code" {
		t.Fatalf("default product config = %+v", config)
	}

	// Removed runtime-selection variables must not reactivate a retired runtime.
	t.Setenv("COCOLA_AGENT_RUNTIME_DEFAULT_ID", "retired-runtime")
	t.Setenv("COCOLA_AGENT_RUNTIME_PICKER_ENABLED", "true")
	config, err = productConfigFromEnv(runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentRuntime.DefaultID != "claude-code" {
		t.Fatalf("legacy environment changed product config = %+v", config)
	}
}

func TestProductConfigFromEnvRejectsInvalidValues(t *testing.T) {
	if _, err := productConfigFromEnv(nil); err == nil {
		t.Fatal("unavailable default runtime should fail")
	}

	t.Setenv("COCOLA_WIKI_MAX_FILE_BYTES", "invalid")
	if _, err := productConfigFromEnv([]agent.Runtime{{ID: "claude-code"}}); err == nil {
		t.Fatal("invalid Wiki limit should fail")
	}
}
