export type TaskStatus = "active" | "paused" | "completed" | "expired";
export type TaskScheduleKind = "once" | "hourly" | "daily" | "weekly" | "monthly";

export type TaskOwner = {
  id: string;
  name: string;
  email: string;
};

export type TaskAttachment = {
  id?: string;
  filename: string;
  mime: string;
  size_bytes: number;
  content_b64?: string;
};

export type ScheduledTask = {
  id: string;
  owner_user_id?: string;
  owner?: TaskOwner;
  conversation_id?: string;
  name: string;
  description?: string;
  status: TaskStatus;
  schedule_kind: TaskScheduleKind;
  schedule_spec: Record<string, unknown>;
  timezone: string;
  prompt: string;
  model_alias: string;
  expires_at?: string;
  next_run_at?: string;
  last_run_at?: string;
  run_count: number;
  last_status: string;
  last_error: string;
  attachments?: TaskAttachment[];
};

export type TaskRun = {
  id: string;
  task_id: string;
  status: string;
  session_id?: string;
  model_alias: string;
  output_text: string;
  error: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
};

export type TaskFormState = {
  name: string;
  prompt: string;
  scheduleKind: TaskScheduleKind;
  runAt: string;
  minute: string;
  hour: string;
  weekday: string;
  day: string;
  ends: "never" | "on";
  expiresAt: string;
  timezone: string;
  modelAlias: string;
  files: TaskAttachment[];
};

export type ModelOption = { alias: string; label: string };

export function detectedTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai";
}

export function emptyTaskForm(modelAlias = ""): TaskFormState {
  const now = new Date(Date.now() + 5 * 60 * 1000);
  return {
    name: "",
    prompt: "",
    scheduleKind: "once",
    runAt: toLocalInput(now.toISOString()),
    minute: "0",
    hour: "9",
    weekday: "1",
    day: "1",
    ends: "never",
    expiresAt: "",
    timezone: detectedTimezone(),
    modelAlias,
    files: [],
  };
}

export function taskToForm(task: ScheduledTask): TaskFormState {
  const hasExpiration = isUsableDate(task.expires_at);
  const timezone = task.timezone || detectedTimezone();
  return {
    name: task.name,
    prompt: task.prompt,
    scheduleKind: task.schedule_kind,
    runAt: toLocalInput(String(task.schedule_spec?.run_at ?? ""), timezone),
    minute: String(Number(task.schedule_spec?.minute ?? 0)),
    hour: String(Number(task.schedule_spec?.hour ?? 9)),
    weekday: String(Number(task.schedule_spec?.weekday ?? 1)),
    day: String(Number(task.schedule_spec?.day ?? 1)),
    ends: hasExpiration ? "on" : "never",
    expiresAt: hasExpiration ? toLocalInput(task.expires_at, timezone) : "",
    timezone,
    modelAlias: task.model_alias,
    files: [],
  };
}

export function taskPayload(
  form: TaskFormState,
  options?: { includeAttachments?: boolean; status?: TaskStatus },
): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    name: form.name.trim(),
    prompt: form.prompt.trim(),
    timezone: form.timezone,
    model_alias: form.modelAlias,
    status: options?.status ?? "active",
    expires_at:
      form.scheduleKind === "once" || form.ends === "never" || !form.expiresAt
        ? null
        : zonedLocalToISO(form.expiresAt, form.timezone),
    config_json: {},
  };
  payload.schedule_kind = form.scheduleKind;
  payload.schedule_spec = scheduleSpec(form);
  if (options?.includeAttachments) payload.attachments = form.files;
  return payload;
}

function scheduleSpec(form: TaskFormState): Record<string, unknown> {
  const minute = clampInt(form.minute, 0, 59, 0);
  const hour = clampInt(form.hour, 0, 23, 9);
  switch (form.scheduleKind) {
    case "once":
      return { run_at: zonedLocalToISO(form.runAt, form.timezone) };
    case "hourly":
      return { minute };
    case "daily":
      return { hour, minute };
    case "weekly":
      return { weekday: clampInt(form.weekday, 1, 7, 1), hour, minute };
    case "monthly":
      return { day: clampInt(form.day, 1, 31, 1), hour, minute };
  }
}

function clampInt(value: string, min: number, max: number, fallback: number): number {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? Math.min(max, Math.max(min, parsed)) : fallback;
}

export function validateTaskForm(form: TaskFormState): string {
  if (!form.name.trim()) return "Task name is required.";
  if (!form.prompt.trim()) return "Tell Cocola what the task should do.";
  if (!form.modelAlias) return "Choose a model.";
  if (form.scheduleKind === "once") {
    if (!isValidDateTimeInput(form.runAt)) {
      return "Choose a valid run date with a four-digit year.";
    }
    const runAt = new Date(zonedLocalToISO(form.runAt, form.timezone)).getTime();
    if (!Number.isFinite(runAt) || runAt <= Date.now()) return "Run time must be in the future.";
  }
  if (form.scheduleKind !== "once" && form.ends === "on") {
    if (!isValidDateTimeInput(form.expiresAt)) {
      return "Choose a valid end date with a four-digit year.";
    }
    const expiresAt = new Date(zonedLocalToISO(form.expiresAt, form.timezone)).getTime();
    if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
      return "End time must be in the future.";
    }
  }
  return "";
}

function isValidDateTimeInput(value: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) return false;
  const year = Number(match[1]);
  return year >= 1 && year <= 9999;
}

export function scheduleLabel(task: ScheduledTask): string {
  const spec = task.schedule_spec ?? {};
  const minute = two(Number(spec.minute ?? 0));
  const hour = two(Number(spec.hour ?? 0));
  switch (task.schedule_kind) {
    case "once":
      return `Once · ${formatDateTime(String(spec.run_at ?? ""))}`;
    case "hourly":
      return `Every hour · :${minute}`;
    case "daily":
      return `Every day · ${hour}:${minute}`;
    case "weekly":
      return `Every ${weekdayLabel(Number(spec.weekday ?? 1))} · ${hour}:${minute}`;
    case "monthly":
      return `Monthly on day ${Number(spec.day ?? 1)} · ${hour}:${minute}`;
  }
}

export function formatDateTime(value?: string): string {
  if (!isUsableDate(value)) return "—";
  const date = new Date(value);
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function taskIsToday(task: ScheduledTask): boolean {
  if (task.status === "completed" || task.status === "expired") return false;
  return isToday(task.next_run_at) || isToday(task.last_run_at);
}

function isToday(value?: string): boolean {
  if (!value) return false;
  const date = new Date(value);
  const now = new Date();
  return (
    !Number.isNaN(date.getTime()) &&
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()
  );
}

export function sortTasks(tasks: ScheduledTask[]): ScheduledTask[] {
  const rank: Record<TaskStatus, number> = { active: 0, paused: 1, completed: 2, expired: 3 };
  return [...tasks].sort((a, b) => {
    const status = rank[a.status] - rank[b.status];
    if (status !== 0) return status;
    return timeValue(a.next_run_at) - timeValue(b.next_run_at);
  });
}

function timeValue(value?: string): number {
  if (!value) return Number.MAX_SAFE_INTEGER;
  const parsed = new Date(value).getTime();
  return Number.isFinite(parsed) ? parsed : Number.MAX_SAFE_INTEGER;
}

function weekdayLabel(value: number): string {
  return (
    ["", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"][value] ||
    "Monday"
  );
}

function two(value: number): string {
  return String(Number.isFinite(value) ? value : 0).padStart(2, "0");
}

export function toLocalInput(value?: string, timezone = detectedTimezone()): string {
  if (!isUsableDate(value)) return "";
  const date = new Date(value);
  const parts = dateParts(date, timezone);
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}`;
}

function zonedLocalToISO(value: string, timezone: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) return "";
  const target = Date.UTC(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
    Number(match[4]),
    Number(match[5]),
  );
  let instant = target;
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const parts = dateParts(new Date(instant), timezone);
    const represented = Date.UTC(
      Number(parts.year),
      Number(parts.month) - 1,
      Number(parts.day),
      Number(parts.hour),
      Number(parts.minute),
    );
    instant = target - (represented - instant);
  }
  return new Date(instant).toISOString();
}

function dateParts(
  date: Date,
  timezone: string,
): Record<"year" | "month" | "day" | "hour" | "minute", string> {
  const values = Object.fromEntries(
    new Intl.DateTimeFormat("en-CA", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    })
      .formatToParts(date)
      .filter((part) => part.type !== "literal")
      .map((part) => [part.type, part.value]),
  );
  return values as Record<"year" | "month" | "day" | "hour" | "minute", string>;
}

function isUsableDate(value?: string): value is string {
  if (!value) return false;
  const date = new Date(value);
  return !Number.isNaN(date.getTime()) && date.getUTCFullYear() > 1970;
}

export async function filesToAttachments(files: FileList | null): Promise<TaskAttachment[]> {
  if (!files) return [];
  return Promise.all(
    Array.from(files).map(async (file) => ({
      filename: file.name,
      mime: file.type || "application/octet-stream",
      size_bytes: file.size,
      content_b64: await fileToBase64(file),
    })),
  );
}

async function fileToBase64(file: File): Promise<string> {
  const buf = await file.arrayBuffer();
  const bytes = new Uint8Array(buf);
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}
