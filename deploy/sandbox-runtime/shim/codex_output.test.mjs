import assert from "node:assert/strict";
import test from "node:test";

import { commandOutputDelta } from "./codex_output.mjs";

test("command output emits only the newly appended content", () => {
  assert.deepEqual(
    commandOutputDelta(
      { type: "command_execution", aggregated_output: "epoch 1\nepoch 2\n" },
      "epoch 1\n",
    ),
    { current: "epoch 1\nepoch 2\n", delta: "epoch 2\n" },
  );
});

test("command output replacement is emitted in full", () => {
  assert.deepEqual(
    commandOutputDelta({ type: "command_execution", aggregated_output: "new\n" }, "old\n"),
    { current: "new\n", delta: "new\n" },
  );
});
