import assert from "node:assert/strict";
import test from "node:test";

import {
  reasoningPresetForEffort,
  reasoningPresetOptions,
  resolveReasoningEffort,
} from "./reasoning-effort.mjs";

test("reasoning presets map to model-native effort values", () => {
  const claudeEfforts = ["low", "high", "max"];

  assert.equal(resolveReasoningEffort("auto", claudeEfforts), "");
  assert.equal(resolveReasoningEffort("fast", claudeEfforts), "low");
  assert.equal(resolveReasoningEffort("deep", claudeEfforts), "high");
  assert.equal(resolveReasoningEffort("max", claudeEfforts), "max");
});

test("unsupported presets stay unavailable instead of guessing", () => {
  assert.equal(resolveReasoningEffort("deep", ["low"]), "");
  assert.deepEqual(reasoningPresetOptions(["low"]), [
    { id: "auto", available: true },
    { id: "fast", available: true },
    { id: "deep", available: false },
    { id: "max", available: false },
  ]);
});

test("persisted native efforts restore the matching product preset", () => {
  assert.equal(reasoningPresetForEffort(""), "auto");
  assert.equal(reasoningPresetForEffort("low"), "fast");
  assert.equal(reasoningPresetForEffort("medium"), "deep");
  assert.equal(reasoningPresetForEffort("high"), "deep");
  assert.equal(reasoningPresetForEffort("xhigh"), "max");
  assert.equal(reasoningPresetForEffort("max"), "max");
});
