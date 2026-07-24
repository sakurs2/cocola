import assert from "node:assert/strict";
import test from "node:test";

import {
  COMPOSER_SLASH_COPY,
  PLAN_ACTION_LABELS,
  PLAN_ERRORS,
  PLAN_MODE_COMMAND,
  PLAN_MODE_COPY,
  PLAN_STATUS_LABELS,
  getOrCreatePlanExecutionRequestId,
  interactionModeForRuntime,
  isPlanModeCommandAvailable,
  isRetryablePlanExecutionStatus,
  latestInteractionMode,
  planExecutionRequestKey,
  shouldAwaitPlanStop,
} from "./plan-mode.mjs";

test("Plan Mode defaults to Execute and restores the latest conversation mode", () => {
  assert.equal(latestInteractionMode([]), "execute");
  assert.equal(
    latestInteractionMode([
      { metadata: { interaction_mode: "plan" } },
      { metadata: {} },
      { metadata: { interaction_mode: "execute" } },
    ]),
    "execute",
  );
  assert.equal(
    latestInteractionMode([
      { metadata: { interaction_mode: "execute" } },
      { metadata: { interaction_mode: "plan" } },
    ]),
    "plan",
  );
});

test("Plan Mode is available only for Claude Code", () => {
  assert.equal(interactionModeForRuntime("claude-code", "plan"), "plan");
  assert.equal(interactionModeForRuntime("claude-code", "execute"), "execute");
  assert.equal(interactionModeForRuntime("codex", "plan"), "execute");
  assert.equal(isPlanModeCommandAvailable("claude-code", "execute", false), true);
  assert.equal(isPlanModeCommandAvailable("claude-code", "plan", false), false);
  assert.equal(isPlanModeCommandAvailable("claude-code", "execute", true), false);
  assert.equal(isPlanModeCommandAvailable("codex", "execute", false), false);
});

test("Plan Mode product copy is stable and English", () => {
  assert.deepEqual(PLAN_MODE_COMMAND, {
    id: "command:plan-mode",
    label: "Plan mode",
    description: "Review a plan before changes",
  });
  assert.deepEqual(COMPOSER_SLASH_COPY, {
    defaultPlaceholder: 'Ask anything, use "/" to select a skill or command',
    menuAriaLabel: "Choose a skill or command",
    commandsTab: "Commands",
    skillsTab: "Skills",
    noCommands: "No commands available.",
    noSkills: "No skills found.",
    loadingSkills: "Loading skills…",
  });
  assert.deepEqual(PLAN_MODE_COPY, {
    activeLabel: "Plan mode",
    responseLabel: "Plan mode · Read-only",
    cancelLabel: "Exit Plan mode",
    lockedLabel: "Plan mode is fixed while Claude is responding",
    initialPlaceholder: "Describe what you want Claude to plan…",
    revisionPlaceholder: "Describe how you want to revise this plan…",
  });
  assert.deepEqual(PLAN_STATUS_LABELS, {
    ready: "Ready for review",
    executing: "Executing",
    completed: "Completed",
    stopped: "Stopped",
    failed: "Failed",
    superseded: "Superseded",
    cancelled: "Cancelled",
  });
  assert.deepEqual(PLAN_ACTION_LABELS, {
    approve: "Approve and execute",
    revise: "Revise plan",
    cancel: "Cancel plan",
    continue: "Continue execution",
    replan: "Re-plan",
    copy: "Copy plan",
  });
  assert.deepEqual(PLAN_ERRORS, {
    unsupported: "Plan mode is supported only for Claude Code conversations.",
    invalidOutput: "Claude did not return a reviewable plan. Refine the request and try again.",
    notCurrent: "This plan is no longer current. Review the latest plan before executing.",
    workspaceChanged:
      "The workspace changed after this plan was created. Create a new plan before executing.",
    modelUnavailable: "The model used for this plan is no longer available. Create a new plan.",
    executionFailed: "Could not start plan execution. Try again.",
  });
});

test("Plan execution retries reuse one idempotency key", () => {
  const requests = new Map();
  const key = planExecutionRequestKey("conversation-1", "plan-1", 2);
  let generated = 0;
  const createRequestId = () => `request-${++generated}`;

  const first = getOrCreatePlanExecutionRequestId(requests, key, createRequestId);
  const retry = getOrCreatePlanExecutionRequestId(requests, key, createRequestId);

  assert.equal(first, "request-1");
  assert.equal(retry, first);
  assert.equal(generated, 1);
  assert.equal(isRetryablePlanExecutionStatus(502), true);
  assert.equal(isRetryablePlanExecutionStatus(503), true);
  assert.equal(isRetryablePlanExecutionStatus(409), false);
});

test("Only Plan execution waits for the authoritative stopped event", () => {
  assert.equal(shouldAwaitPlanStop({ planId: "plan-1" }), true);
  assert.equal(shouldAwaitPlanStop({}), false);
  assert.equal(shouldAwaitPlanStop(null), false);
});
