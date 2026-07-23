import assert from "node:assert/strict";
import test from "node:test";

import { selectAgentRuntime } from "./agent-runtime-policy.mjs";

const runtimes = [
  { id: "claude-code", model_protocol: "anthropic-messages" },
  { id: "codex", model_protocol: "openai-responses" },
];

test("picker disabled always selects the configured default", () => {
  const selected = selectAgentRuntime({
    runtimes,
    defaultRuntimeId: "claude-code",
    pickerEnabled: false,
    preferredRuntimeId: "codex",
  });
  assert.equal(selected?.id, "claude-code");
});

test("picker enabled honors a valid preference", () => {
  const selected = selectAgentRuntime({
    runtimes,
    defaultRuntimeId: "claude-code",
    pickerEnabled: true,
    preferredRuntimeId: "codex",
  });
  assert.equal(selected?.id, "codex");
});

test("picker enabled falls back to the configured default", () => {
  const selected = selectAgentRuntime({
    runtimes,
    defaultRuntimeId: "claude-code",
    pickerEnabled: true,
    preferredRuntimeId: "missing",
  });
  assert.equal(selected?.id, "claude-code");
});

test("missing configured default fails closed", () => {
  const selected = selectAgentRuntime({
    runtimes,
    defaultRuntimeId: "missing",
    pickerEnabled: false,
    preferredRuntimeId: "",
  });
  assert.equal(selected, null);
});
