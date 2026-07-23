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
	runtimes := []agent.Runtime{
		{ID: "claude-code"},
		{ID: "codex"},
	}

	config, err := productConfigFromEnv(runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentRuntime.DefaultID != "claude-code" || config.AgentRuntime.PickerEnabled {
		t.Fatalf("default product config = %+v", config)
	}

	t.Setenv("COCOLA_AGENT_RUNTIME_DEFAULT_ID", "codex")
	t.Setenv("COCOLA_AGENT_RUNTIME_PICKER_ENABLED", "true")
	config, err = productConfigFromEnv(runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentRuntime.DefaultID != "codex" || !config.AgentRuntime.PickerEnabled {
		t.Fatalf("configured product config = %+v", config)
	}
}

func TestProductConfigFromEnvRejectsInvalidValues(t *testing.T) {
	runtimes := []agent.Runtime{{ID: "claude-code"}}

	t.Setenv("COCOLA_AGENT_RUNTIME_PICKER_ENABLED", "sometimes")
	if _, err := productConfigFromEnv(runtimes); err == nil {
		t.Fatal("invalid picker boolean should fail")
	}

	t.Setenv("COCOLA_AGENT_RUNTIME_PICKER_ENABLED", "false")
	t.Setenv("COCOLA_AGENT_RUNTIME_DEFAULT_ID", "codex")
	if _, err := productConfigFromEnv(runtimes); err == nil {
		t.Fatal("unavailable default runtime should fail")
	}

	t.Setenv("COCOLA_AGENT_RUNTIME_DEFAULT_ID", " ")
	if _, err := productConfigFromEnv(runtimes); err == nil {
		t.Fatal("empty default runtime should fail")
	}
}
