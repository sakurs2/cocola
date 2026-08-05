export type ScheduledTaskResultTone = "default" | "success" | "warning" | "danger" | "accent";

export type ScheduledTaskResultInput = {
  last_error?: string | null;
  last_status?: string | null;
  run_count?: number | null;
};

export type ScheduledTaskResultView = {
  label: string;
  detail: string;
  tone: ScheduledTaskResultTone;
};

export function scheduledTaskResultView(task: ScheduledTaskResultInput): ScheduledTaskResultView {
  const error = task.last_error?.trim() ?? "";
  if (error) {
    const normalized = error.toLowerCase();
    if (normalized.includes("worker heartbeat timeout")) {
      return { label: "Worker timeout", detail: error, tone: "danger" };
    }
    if (normalized.includes("expired")) {
      return { label: "Run expired", detail: error, tone: "danger" };
    }
    if (normalized.includes("timeout") || normalized.includes("timed out")) {
      return { label: "Timed out", detail: error, tone: "danger" };
    }
    if (normalized.includes("unauthorized") || normalized.includes("authentication")) {
      return { label: "Authentication failed", detail: error, tone: "danger" };
    }
    if (normalized.includes("rate limit") || normalized.includes("quota")) {
      return { label: "Limit reached", detail: error, tone: "warning" };
    }
    return { label: "Run failed", detail: error, tone: "danger" };
  }

  const status = task.last_status?.trim().toLowerCase() ?? "";
  if (["completed", "success", "succeeded"].includes(status)) {
    return { label: "Succeeded", detail: task.last_status?.trim() || "Succeeded", tone: "success" };
  }
  if (["failed", "error"].includes(status)) {
    return { label: "Failed", detail: task.last_status?.trim() || "Failed", tone: "danger" };
  }
  if (["running", "in_progress"].includes(status)) {
    return { label: "Running", detail: task.last_status?.trim() || "Running", tone: "accent" };
  }
  if (["cancelled", "canceled"].includes(status)) {
    return { label: "Cancelled", detail: task.last_status?.trim() || "Cancelled", tone: "warning" };
  }
  if (status) {
    return {
      label: status.replace(/[_-]+/g, " ").replace(/^./, (character) => character.toUpperCase()),
      detail: task.last_status?.trim() || status,
      tone: "default",
    };
  }
  if ((task.run_count ?? 0) > 0) {
    return { label: "Completed", detail: "Completed", tone: "success" };
  }
  return { label: "Not run", detail: "This task has not run yet.", tone: "default" };
}
