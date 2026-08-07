import assert from "node:assert/strict";
import test from "node:test";

import { withCodexReasoningEffort } from "./codex_reasoning.mjs";

test("Codex thread options receive the selected reasoning effort", () => {
  assert.deepEqual(withCodexReasoningEffort({ model: "gpt-5" }, "xhigh"), {
    model: "gpt-5",
    modelReasoningEffort: "xhigh",
  });
});

test("empty effort keeps model defaults and unsupported values fail closed", () => {
  const options = { model: "gpt-5" };
  assert.equal(withCodexReasoningEffort(options, ""), options);
  assert.throws(() => withCodexReasoningEffort(options, "max"), /unsupported Codex/);
});
