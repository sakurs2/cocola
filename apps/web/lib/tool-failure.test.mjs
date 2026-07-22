import assert from "node:assert/strict";
import test from "node:test";

import { isCommandTool } from "./tool-failure.mjs";

test("recognizes only the command tools defined by the runtime protocol", () => {
  assert.equal(isCommandTool("Bash"), true);
  assert.equal(isCommandTool("command_execution"), true);
  assert.equal(isCommandTool("Edit"), false);
  assert.equal(isCommandTool("terminal"), false);
  assert.equal(isCommandTool("mcp__host__command_execution"), false);
});
