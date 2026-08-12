export type ScheduledTaskResultTone = "default" | "success" | "warning" | "danger" | "accent";

export type ScheduledTaskResultInput = {
  last_error?: string | null;
  last_status?: string | null;
  run_count?: number | null;
};

export type ScheduledTaskResultView = {
  key:
    | "workerTimeout"
    | "runExpired"
    | "timedOut"
    | "authenticationFailed"
    | "limitReached"
    | "runFailed"
    | "succeeded"
    | "failed"
    | "running"
    | "cancelled"
    | "completed"
    | "notRun"
    | "unknown";
  detail?: string;
  fallbackLabel?: string;
  tone: ScheduledTaskResultTone;
};

export function scheduledTaskResultView(task: ScheduledTaskResultInput): ScheduledTaskResultView {
  const error = task.last_error?.trim() ?? "";
  if (error) {
    const normalized = error.toLowerCase();
    if (normalized.includes("worker heartbeat timeout")) {
      return { key: "workerTimeout", detail: error, tone: "danger" };
    }
    if (normalized.includes("expired")) {
      return { key: "runExpired", detail: error, tone: "danger" };
    }
    if (normalized.includes("timeout") || normalized.includes("timed out")) {
      return { key: "timedOut", detail: error, tone: "danger" };
    }
    if (normalized.includes("unauthorized") || normalized.includes("authentication")) {
      return { key: "authenticationFailed", detail: error, tone: "danger" };
    }
    if (normalized.includes("rate limit") || normalized.includes("quota")) {
      return { key: "limitReached", detail: error, tone: "warning" };
    }
    return { key: "runFailed", detail: error, tone: "danger" };
  }

  const status = task.last_status?.trim().toLowerCase() ?? "";
  if (["completed", "success", "succeeded"].includes(status)) {
    return { key: "succeeded", tone: "success" };
  }
  if (["failed", "error"].includes(status)) {
    return { key: "failed", tone: "danger" };
  }
  if (["running", "in_progress"].includes(status)) {
    return { key: "running", tone: "accent" };
  }
  if (["cancelled", "canceled"].includes(status)) {
    return { key: "cancelled", tone: "warning" };
  }
  if (status) {
    return {
      key: "unknown",
      fallbackLabel: status
        .replace(/[_-]+/g, " ")
        .replace(/^./, (character) => character.toUpperCase()),
      detail: task.last_status?.trim() || status,
      tone: "default",
    };
  }
  if ((task.run_count ?? 0) > 0) {
    return { key: "completed", tone: "success" };
  }
  return { key: "notRun", tone: "default" };
}
