import assert from "node:assert/strict";
import test from "node:test";

import { selectAgentRuntime } from "./agent-runtime-policy.mjs";

const runtimes = [{ id: "claude-code", model_protocol: "anthropic-messages" }];

test("selects the configured built-in runtime", () => {
  const selected = selectAgentRuntime({
    runtimes,
    defaultRuntimeId: "claude-code",
  });
  assert.equal(selected?.id, "claude-code");
});

test("missing configured default fails closed", () => {
  const selected = selectAgentRuntime({
    runtimes,
    defaultRuntimeId: "missing",
  });
  assert.equal(selected, null);
});
