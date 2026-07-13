"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { CalendarDots, Paperclip, UserCircle } from "@phosphor-icons/react";
import { ChevronRight, X } from "lucide-react";
import { useEffect, useState } from "react";
import {
  emptyTaskForm,
  filesToAttachments,
  formatDateTime,
  taskToForm,
  toLocalInput,
  validateTaskForm,
  type ModelOption,
  type ScheduledTask,
  type TaskFormState,
  type TaskRun,
} from "@/lib/scheduled-tasks";
import { cn } from "@/lib/utils";

type OwnerOption = { id: string; name?: string; email?: string };

export function TaskDrawer({
  open,
  onOpenChange,
  task,
  models,
  defaultModelID,
  admin = false,
  ownerOptions = [],
  recentRuns = [],
  saving,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: ScheduledTask | null;
  models: ModelOption[];
  defaultModelID?: string;
  admin?: boolean;
  ownerOptions?: OwnerOption[];
  recentRuns?: TaskRun[];
  saving: boolean;
  onSave: (form: TaskFormState, ownerUserID?: string) => Promise<void>;
}) {
  const [form, setForm] = useState<TaskFormState>(() => emptyTaskForm());
  const [ownerUserID, setOwnerUserID] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    const defaultModel = models.find((model) => model.id === defaultModelID) ?? models[0];
    setForm(
      task ? taskToForm(task) : emptyTaskForm(defaultModel?.id ?? "", defaultModel?.alias ?? ""),
    );
    setOwnerUserID(task?.owner_user_id ?? "");
    setError("");
  }, [defaultModelID, models, open, task]);

  async function submit() {
    const validation = validateTaskForm(form);
    if (validation) {
      setError(validation);
      return;
    }
    if (admin && task && !task.owner_user_id && !ownerUserID) {
      setError("Assign an owner before saving this legacy task.");
      return;
    }
    setError("");
    try {
      await onSave(form, ownerUserID || undefined);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  const scheduleAgain = task?.status === "completed" || task?.status === "expired";

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/20 backdrop-blur-sm data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out data-[state=open]:fade-in" />
        <Dialog.Content
          className={cn(
            "fixed inset-y-2 right-2 z-50 flex w-[min(32rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-3xl border border-border bg-background/95 text-foreground shadow-2xl backdrop-blur-xl outline-none",
            "data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right",
            admin ? "cocola-admin-ui admin-drawer" : "cocola-user-ui",
          )}
        >
          <header className="flex min-h-16 items-center gap-3 border-b border-border/70 px-5">
            <span className="grid size-9 place-items-center rounded-2xl bg-sky-500/10 text-sky-600">
              <CalendarDots className="size-[18px]" weight="duotone" />
            </span>
            <div className="min-w-0 flex-1">
              <Dialog.Title className="truncate text-base font-semibold">
                {task ? "Edit task" : "New task"}
              </Dialog.Title>
              <Dialog.Description className="truncate text-xs text-muted-foreground">
                Schedule Cocola to work automatically.
              </Dialog.Description>
            </div>
            <Dialog.Close className="grid size-9 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40">
              <X className="size-4" />
              <span className="sr-only">Close</span>
            </Dialog.Close>
          </header>

          <div className="min-h-0 flex-1 overflow-y-auto p-5">
            <div className="grid gap-4">
              {admin && task ? (
                <div className="rounded-2xl border border-border/70 bg-muted/30 p-3">
                  <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                    <UserCircle className="size-4" weight="duotone" /> Owner
                  </div>
                  {task.owner_user_id ? (
                    <div className="mt-1 text-sm">
                      {task.owner?.name || task.owner?.email || task.owner_user_id}
                      {task.owner?.email && task.owner.name ? (
                        <span className="ml-2 text-xs text-muted-foreground">
                          {task.owner.email}
                        </span>
                      ) : null}
                    </div>
                  ) : (
                    <select
                      className={inputClass}
                      value={ownerUserID}
                      onChange={(event) => setOwnerUserID(event.target.value)}
                    >
                      <option value="">Choose an owner</option>
                      {ownerOptions.map((owner) => (
                        <option key={owner.id} value={owner.id}>
                          {owner.name || owner.email || owner.id}
                        </option>
                      ))}
                    </select>
                  )}
                </div>
              ) : null}

              {admin && task?.last_error ? (
                <div className="rounded-xl border border-destructive/25 bg-destructive/10 px-3 py-2 text-xs leading-5 text-destructive">
                  Last error: {task.last_error}
                </div>
              ) : null}

              <Field label="Task name">
                <input
                  autoFocus
                  className={inputClass}
                  value={form.name}
                  onChange={(event) => setForm({ ...form, name: event.target.value })}
                  placeholder="Daily project summary"
                />
              </Field>

              <Field label="What should Cocola do?">
                <textarea
                  className="min-h-32 rounded-xl border border-input bg-background px-3 py-2.5 text-sm outline-none transition focus:border-ring focus:ring-2 focus:ring-ring/20"
                  value={form.prompt}
                  onChange={(event) => setForm({ ...form, prompt: event.target.value })}
                  placeholder="Describe the result you want..."
                />
              </Field>

              <Field label="Repeat">
                <select
                  className={inputClass}
                  value={form.scheduleKind}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      scheduleKind: event.target.value as TaskFormState["scheduleKind"],
                    })
                  }
                >
                  <option value="once">Does not repeat</option>
                  <option value="hourly">Every hour</option>
                  <option value="daily">Every day</option>
                  <option value="weekly">Every week</option>
                  <option value="monthly">Every month</option>
                </select>
              </Field>
              <ScheduleFields form={form} setForm={setForm} />

              <div className="rounded-2xl border border-border/70 bg-muted/25 px-3 py-2 text-xs text-muted-foreground">
                Times use <span className="font-medium text-foreground">{form.timezone}</span>.
              </div>

              <details className="group rounded-2xl border border-border/70 bg-card/60 p-3">
                <summary className="flex cursor-pointer list-none items-center gap-2 text-sm font-medium [&::-webkit-details-marker]:hidden">
                  <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
                  Advanced
                </summary>
                <div className="mt-4 grid gap-4 border-t border-border/70 pt-4">
                  <Field label="Model">
                    <select
                      className={inputClass}
                      value={form.modelRouteID}
                      onChange={(event) => {
                        const model = models.find(
                          (candidate) => candidate.id === event.target.value,
                        );
                        if (model)
                          setForm({ ...form, modelRouteID: model.id, modelAlias: model.alias });
                      }}
                    >
                      {models.map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.label || model.alias}
                        </option>
                      ))}
                    </select>
                  </Field>
                  <Field label="Attachments">
                    <label className="flex min-h-12 cursor-pointer items-center gap-2 rounded-xl border border-dashed border-border px-3 text-sm text-muted-foreground hover:bg-muted/40">
                      <Paperclip className="size-4" weight="duotone" />
                      <span className="truncate">
                        {form.files.length
                          ? form.files.map((file) => file.filename).join(", ")
                          : task?.attachments?.length
                            ? `${task.attachments.length} saved file(s) · choose to replace`
                            : "Choose files"}
                      </span>
                      <input
                        type="file"
                        multiple
                        className="sr-only"
                        onChange={async (event) =>
                          setForm({ ...form, files: await filesToAttachments(event.target.files) })
                        }
                      />
                    </label>
                  </Field>
                </div>
              </details>

              {admin && recentRuns.length ? (
                <details className="group rounded-2xl border border-border/70 bg-card/60 p-3">
                  <summary className="flex cursor-pointer list-none items-center gap-2 text-sm font-medium [&::-webkit-details-marker]:hidden">
                    <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
                    Recent runs
                  </summary>
                  <div className="mt-3 divide-y divide-border/60 border-t border-border/70 pt-1">
                    {recentRuns.slice(0, 8).map((run) => (
                      <div key={run.id} className="py-2 text-xs">
                        <div className="flex items-center justify-between gap-3">
                          <span className="font-medium capitalize">{run.status}</span>
                          <span className="text-muted-foreground">
                            {formatDateTime(run.finished_at || run.started_at || run.created_at)}
                          </span>
                        </div>
                        {run.error ? <p className="mt-1 text-destructive">{run.error}</p> : null}
                      </div>
                    ))}
                  </div>
                </details>
              ) : null}

              {error ? (
                <div className="rounded-xl border border-destructive/25 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  {error}
                </div>
              ) : null}
            </div>
          </div>

          <footer className="flex items-center justify-end gap-2 border-t border-border/70 p-4">
            <Dialog.Close className="h-10 rounded-xl border border-border bg-background px-4 text-sm font-medium hover:bg-muted">
              Cancel
            </Dialog.Close>
            <button
              type="button"
              disabled={saving}
              onClick={() => void submit()}
              className="h-10 rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground shadow-sm transition hover:bg-primary/90 disabled:opacity-60"
            >
              {saving
                ? "Saving…"
                : scheduleAgain
                  ? "Schedule again"
                  : task
                    ? "Save changes"
                    : "Create task"}
            </button>
          </footer>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function ScheduleFields({
  form,
  setForm,
}: {
  form: TaskFormState;
  setForm: (form: TaskFormState) => void;
}) {
  const minDateTime = toLocalInput(new Date().toISOString(), form.timezone);
  if (form.scheduleKind === "once") {
    return (
      <Field label="Run at">
        <input
          type="datetime-local"
          min={minDateTime}
          max={maxDateTime}
          step={60}
          className={inputClass}
          value={form.runAt}
          onChange={(event) =>
            setForm({ ...form, runAt: boundedDateTime(event.target.value, form.runAt) })
          }
        />
      </Field>
    );
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {form.scheduleKind === "weekly" ? (
        <Field label="Day">
          <select
            className={inputClass}
            value={form.weekday}
            onChange={(event) => setForm({ ...form, weekday: event.target.value })}
          >
            {["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"].map(
              (label, index) => (
                <option key={label} value={index + 1}>
                  {label}
                </option>
              ),
            )}
          </select>
        </Field>
      ) : null}
      {form.scheduleKind === "monthly" ? (
        <Field label="Day of month">
          <span className="grid gap-1">
            <input
              type="number"
              min={1}
              max={31}
              className={inputClass}
              value={form.day}
              onChange={(event) => setForm({ ...form, day: event.target.value })}
            />
            <span className="font-normal text-muted-foreground">
              Short months use their last day.
            </span>
          </span>
        </Field>
      ) : null}
      {form.scheduleKind === "hourly" ? (
        <Field label="Minute of the hour">
          <input
            type="number"
            min={0}
            max={59}
            className={inputClass}
            value={form.minute}
            onChange={(event) => setForm({ ...form, minute: event.target.value })}
          />
        </Field>
      ) : (
        <Field label="Time">
          <input
            type="time"
            className={inputClass}
            value={`${form.hour.padStart(2, "0")}:${form.minute.padStart(2, "0")}`}
            onChange={(event) => {
              const [hour = "0", minute = "0"] = event.target.value.split(":");
              setForm({ ...form, hour: hour || "0", minute: minute || "0" });
            }}
          />
        </Field>
      )}
      <Field label="Ends">
        <select
          className={inputClass}
          value={form.ends}
          onChange={(event) => setForm({ ...form, ends: event.target.value as "never" | "on" })}
        >
          <option value="never">Never</option>
          <option value="on">On a date</option>
        </select>
      </Field>
      {form.ends === "on" ? (
        <Field label="End time">
          <input
            type="datetime-local"
            min={minDateTime}
            max={maxDateTime}
            step={60}
            className={inputClass}
            value={form.expiresAt}
            onChange={(event) =>
              setForm({
                ...form,
                expiresAt: boundedDateTime(event.target.value, form.expiresAt),
              })
            }
          />
        </Field>
      ) : null}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">
      {label}
      {children}
    </label>
  );
}

const inputClass =
  "h-10 min-w-0 rounded-xl border border-input bg-background px-3 text-sm text-foreground outline-none transition focus:border-ring focus:ring-2 focus:ring-ring/20 disabled:opacity-60";

const maxDateTime = "9999-12-31T23:59";

function boundedDateTime(next: string, current: string): string {
  if (!next) return "";
  const year = next.split("-", 1)[0] ?? "";
  return year.length === 4 ? next : current;
}

export function TaskConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  busy,
  destructive = false,
  admin = false,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  busy: boolean;
  destructive?: boolean;
  admin?: boolean;
  onConfirm: () => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[60] bg-slate-950/25 backdrop-blur-sm" />
        <Dialog.Content
          className={cn(
            "fixed left-1/2 top-1/2 z-[60] w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-3xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none",
            admin ? "cocola-admin-ui admin-drawer" : "cocola-user-ui",
          )}
        >
          <Dialog.Title className="text-base font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="mt-2 text-sm leading-6 text-muted-foreground">
            {description}
          </Dialog.Description>
          <div className="mt-6 flex justify-end gap-2">
            <Dialog.Close className="h-9 rounded-xl border border-border px-3 text-sm font-medium hover:bg-muted">
              Cancel
            </Dialog.Close>
            <button
              type="button"
              disabled={busy}
              onClick={onConfirm}
              className={cn(
                "h-9 rounded-xl px-3 text-sm font-medium text-white disabled:opacity-60",
                destructive
                  ? "bg-destructive hover:bg-destructive/90"
                  : "bg-primary hover:bg-primary/90",
              )}
            >
              {busy ? "Working…" : confirmLabel}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
