import assert from "node:assert/strict";
import test from "node:test";

import { isCommandTool, toolOutcomeFromArtifact, toolOutcomeLabel } from "./tool-failure.mjs";

test("recognizes only the command tools defined by the runtime protocol", () => {
  assert.equal(isCommandTool("Bash"), true);
  assert.equal(isCommandTool("command_execution"), true);
  assert.equal(isCommandTool("Edit"), false);
  assert.equal(isCommandTool("terminal"), false);
  assert.equal(isCommandTool("mcp__host__command_execution"), false);
});

test("uses structured outcomes instead of parsing tool error text", () => {
  assert.equal(toolOutcomeLabel("Bash", "permission_denied", true), "Command blocked in Plan mode");
  assert.equal(toolOutcomeLabel("Bash", "unavailable", true), "Command is unavailable");
  assert.equal(toolOutcomeLabel("Bash", "failed", true), "Command failed");
  assert.equal(toolOutcomeLabel("Bash", "timeout", true), "Command timed out");
  assert.equal(
    toolOutcomeLabel("Read", "permission_denied", true),
    "Tool call blocked in Plan mode",
  );
  assert.equal(toolOutcomeLabel("Read", "unavailable", true), "Tool is unavailable");
  assert.equal(toolOutcomeLabel("Read", "failed", true), "Tool call failed");
  assert.equal(toolOutcomeLabel("Read", "timeout", true), "Tool call timed out");
  assert.equal(toolOutcomeLabel("Bash", "", false), "Command completed");
  assert.equal(toolOutcomeLabel("Read", "", false), "Tool call completed");
});

test("reads only the formal outcome artifact field", () => {
  assert.equal(
    toolOutcomeFromArtifact({ cocolaToolOutcome: "permission_denied" }, true),
    "permission_denied",
  );
  assert.equal(toolOutcomeFromArtifact({ message: "permission denied" }, true), "failed");
  assert.equal(toolOutcomeFromArtifact("permission_denied", true), "failed");
  assert.equal(toolOutcomeFromArtifact({ cocolaToolOutcome: "success" }, true), "failed");
  assert.equal(
    toolOutcomeFromArtifact({ cocolaToolOutcome: "permission_denied" }, false),
    "success",
  );
});
