export const INTERACTION_MODE_OPTIONS = Object.freeze([
  Object.freeze({
    id: "execute",
    label: "Execute",
    description: "Make changes immediately",
  }),
  Object.freeze({
    id: "plan",
    label: "Plan",
    description: "Review a plan before changes",
  }),
]);

export const PLAN_MODE_COPY = Object.freeze({
  banner: "Plan mode · Claude will analyze the task without changing your workspace.",
  initialPlaceholder: "Describe what you want Claude to plan…",
  revisionPlaceholder: "Describe how you want to revise this plan…",
});

export const PLAN_STATUS_LABELS = Object.freeze({
  ready: "Ready for review",
  executing: "Executing",
  completed: "Completed",
  stopped: "Stopped",
  failed: "Failed",
  superseded: "Superseded",
  cancelled: "Cancelled",
});

export const PLAN_ACTION_LABELS = Object.freeze({
  approve: "Approve and execute",
  revise: "Revise plan",
  cancel: "Cancel plan",
  continue: "Continue execution",
  replan: "Re-plan",
  copy: "Copy plan",
});

export const PLAN_ERRORS = Object.freeze({
  unsupported: "Plan mode is supported only for Claude Code conversations.",
  invalidOutput: "Claude did not return a reviewable plan. Refine the request and try again.",
  notCurrent: "This plan is no longer current. Review the latest plan before executing.",
  workspaceChanged:
    "The workspace changed after this plan was created. Create a new plan before executing.",
  modelUnavailable: "The model used for this plan is no longer available. Create a new plan.",
  executionFailed: "Could not start plan execution. Try again.",
});

export function latestInteractionMode(messages) {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const mode = messages[index]?.metadata?.interaction_mode;
    if (mode === "plan" || mode === "execute") return mode;
  }
  return "execute";
}

export function interactionModeForRuntime(runtimeId, requestedMode) {
  return runtimeId === "claude-code" && requestedMode === "plan" ? "plan" : "execute";
}

export function planExecutionRequestKey(conversationId, planId, version) {
  return JSON.stringify([conversationId, planId, version]);
}

export function getOrCreatePlanExecutionRequestId(requests, key, createRequestId) {
  const existing = requests.get(key);
  if (existing) return existing;
  const requestId = createRequestId();
  requests.set(key, requestId);
  return requestId;
}

export function isRetryablePlanExecutionStatus(status) {
  return Number.isInteger(status) && status >= 500 && status <= 599;
}

export function shouldAwaitPlanStop(cursor) {
  return typeof cursor?.planId === "string" && cursor.planId.length > 0;
}
