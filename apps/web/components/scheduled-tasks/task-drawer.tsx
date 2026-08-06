"use client";

import { Button, Card, Chip, Dropdown, Input, Label, TextArea, TextField } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import { CalendarClock, ChevronDown, ChevronRight, Paperclip, UserCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { ModelIcon } from "@/components/ui/model-icon";
import { inferModelIconSlug } from "@/lib/model-icons";
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

type OwnerOption = { id: string; name?: string; email?: string };
type Choice = { id: string; label: string };

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
  const model = models.find((candidate) => candidate.id === form.modelRouteID);
  const selectedModelIcon = taskModelIcon(model, form.modelAlias);

  return (
    <Sheet isOpen={open} placement="right" onOpenChange={(next) => !saving && onOpenChange(next)}>
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[520px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label="Close task editor" />
            <Sheet.Header>
              <span className="flex items-center gap-3">
                <span className="bg-accent-soft text-accent flex size-10 shrink-0 items-center justify-center rounded-2xl">
                  <CalendarClock className="size-5" />
                </span>
                <span>
                  <Sheet.Heading>{task ? "Edit task" : "New task"}</Sheet.Heading>
                  <span className="text-muted mt-1 block text-sm">
                    Schedule Cocola to work automatically.
                  </span>
                </span>
              </span>
            </Sheet.Header>
            <Sheet.Body className="grid content-start gap-5">
              {admin && task ? (
                <Card className="p-4">
                  <Card.Content className="p-0">
                    <p className="text-muted flex items-center gap-2 text-xs font-medium">
                      <UserCircle className="size-4" />
                      Owner
                    </p>
                    {task.owner_user_id ? (
                      <p className="mt-2 text-sm font-medium">
                        {task.owner?.name || task.owner?.email || task.owner_user_id}
                        {task.owner?.email && task.owner.name ? (
                          <span className="text-muted ml-2 font-normal">{task.owner.email}</span>
                        ) : null}
                      </p>
                    ) : (
                      <ChoiceDropdown
                        label="Owner"
                        value={
                          ownerOptions.find((owner) => owner.id === ownerUserID)?.name ||
                          ownerOptions.find((owner) => owner.id === ownerUserID)?.email ||
                          "Choose an owner"
                        }
                        options={ownerOptions.map((owner) => ({
                          id: owner.id,
                          label: owner.name || owner.email || owner.id,
                        }))}
                        onChange={setOwnerUserID}
                      />
                    )}
                  </Card.Content>
                </Card>
              ) : null}

              {admin && task?.last_error ? (
                <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                  Last error: {task.last_error}
                </div>
              ) : null}

              <TextField
                value={form.name}
                variant="secondary"
                onChange={(name) => setForm({ ...form, name })}
              >
                <Label>Task name</Label>
                <Input autoFocus placeholder="Daily project summary" />
              </TextField>
              <TextField
                value={form.prompt}
                variant="secondary"
                onChange={(prompt) => setForm({ ...form, prompt })}
              >
                <Label>What should Cocola do?</Label>
                <TextArea rows={5} placeholder="Describe the result you want..." />
              </TextField>
              <ChoiceDropdown
                label="Repeat"
                value={
                  scheduleOptions.find((option) => option.id === form.scheduleKind)?.label ||
                  form.scheduleKind
                }
                options={scheduleOptions}
                onChange={(scheduleKind) =>
                  setForm({ ...form, scheduleKind: scheduleKind as TaskFormState["scheduleKind"] })
                }
              />
              <ScheduleFields form={form} setForm={setForm} />

              <div className="bg-surface-secondary text-muted rounded-2xl px-4 py-3 text-xs">
                Times use <span className="text-foreground font-medium">{form.timezone}</span>.
              </div>

              <details className="group border-separator bg-default rounded-2xl border p-4">
                <summary className="flex cursor-pointer list-none items-center gap-2 text-sm font-semibold [&::-webkit-details-marker]:hidden">
                  <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
                  Advanced
                </summary>
                <div className="border-separator mt-4 grid gap-5 border-t pt-4">
                  <div>
                    <Label>Model</Label>
                    <Dropdown>
                      <Dropdown.Trigger
                        aria-label="Select task model"
                        className="border-separator bg-surface-secondary hover:bg-default-hover mt-2 flex h-11 w-full items-center justify-between rounded-2xl border px-3 text-sm"
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          <ModelIcon bare className="size-5 shrink-0" icon={selectedModelIcon} />
                          <span className="truncate font-medium">
                            {model?.alias || form.modelAlias || "Model unavailable"}
                          </span>
                        </span>
                        <ChevronDown className="text-muted size-4" />
                      </Dropdown.Trigger>
                      <Dropdown.Popover placement="bottom start">
                        <Dropdown.Menu
                          aria-label="Task models"
                          onAction={(key) => {
                            const selected = models.find(
                              (candidate) => candidate.id === String(key),
                            );
                            if (selected)
                              setForm({
                                ...form,
                                modelRouteID: selected.id,
                                modelAlias: selected.alias,
                              });
                          }}
                        >
                          {models.map((item) => (
                            <Dropdown.Item key={item.id} id={item.id} textValue={item.alias}>
                              <span className="flex items-center gap-2">
                                <ModelIcon bare className="size-5" icon={taskModelIcon(item)} />
                                {item.alias}
                              </span>
                            </Dropdown.Item>
                          ))}
                        </Dropdown.Menu>
                      </Dropdown.Popover>
                    </Dropdown>
                  </div>
                  <div>
                    <Label>Attachments</Label>
                    <label className="border-separator bg-surface-secondary hover:bg-default-hover text-muted mt-2 flex min-h-12 cursor-pointer items-center gap-2 rounded-2xl border border-dashed px-3 text-sm transition-colors">
                      <Paperclip className="size-4" />
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
                  </div>
                </div>
              </details>

              {admin && recentRuns.length ? (
                <Card className="p-4">
                  <Card.Header className="p-0">
                    <Card.Title>Recent runs</Card.Title>
                  </Card.Header>
                  <Card.Content className="mt-3 grid gap-2 p-0">
                    {recentRuns.slice(0, 8).map((run) => (
                      <div
                        key={run.id}
                        className="bg-surface-secondary flex items-start justify-between gap-3 rounded-2xl px-3 py-2 text-xs"
                      >
                        <span>
                          <span className="font-medium capitalize">{run.status}</span>
                          {run.error ? (
                            <span className="text-danger mt-1 block">{run.error}</span>
                          ) : null}
                        </span>
                        <span className="text-muted shrink-0">
                          {formatDateTime(run.finished_at || run.started_at || run.created_at)}
                        </span>
                      </div>
                    ))}
                  </Card.Content>
                </Card>
              ) : null}

              {error ? (
                <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                  {error}
                </div>
              ) : null}
            </Sheet.Body>
            <Sheet.Footer className="gap-2">
              <Button variant="outline" onPress={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button isPending={saving} onPress={() => void submit()}>
                {scheduleAgain ? "Schedule again" : task ? "Save changes" : "Create task"}
              </Button>
            </Sheet.Footer>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
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
      <TextField
        value={form.runAt}
        variant="secondary"
        onChange={(runAt) => setForm({ ...form, runAt: boundedDateTime(runAt, form.runAt) })}
      >
        <Label>Run at</Label>
        <Input type="datetime-local" min={minDateTime} max={maxDateTime} step={60} />
      </TextField>
    );
  }
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {form.scheduleKind === "weekly" ? (
        <ChoiceDropdown
          label="Day"
          value={weekdays.find((option) => option.id === form.weekday)?.label || "Monday"}
          options={weekdays}
          onChange={(weekday) => setForm({ ...form, weekday })}
        />
      ) : null}
      {form.scheduleKind === "monthly" ? (
        <TextField
          value={form.day}
          variant="secondary"
          onChange={(day) => setForm({ ...form, day })}
        >
          <Label>Day of month</Label>
          <Input type="number" min={1} max={31} />
          <span className="text-muted text-xs">Short months use their last day.</span>
        </TextField>
      ) : null}
      {form.scheduleKind === "hourly" ? (
        <TextField
          value={form.minute}
          variant="secondary"
          onChange={(minute) => setForm({ ...form, minute })}
        >
          <Label>Minute of the hour</Label>
          <Input type="number" min={0} max={59} />
        </TextField>
      ) : (
        <TextField
          value={`${form.hour.padStart(2, "0")}:${form.minute.padStart(2, "0")}`}
          variant="secondary"
          onChange={(value) => {
            const [hour = "0", minute = "0"] = value.split(":");
            setForm({ ...form, hour: hour || "0", minute: minute || "0" });
          }}
        >
          <Label>Time</Label>
          <Input type="time" />
        </TextField>
      )}
      <ChoiceDropdown
        label="Ends"
        value={form.ends === "never" ? "Never" : "On a date"}
        options={[
          { id: "never", label: "Never" },
          { id: "on", label: "On a date" },
        ]}
        onChange={(ends) => setForm({ ...form, ends: ends as "never" | "on" })}
      />
      {form.ends === "on" ? (
        <TextField
          value={form.expiresAt}
          variant="secondary"
          onChange={(expiresAt) =>
            setForm({ ...form, expiresAt: boundedDateTime(expiresAt, form.expiresAt) })
          }
        >
          <Label>End time</Label>
          <Input type="datetime-local" min={minDateTime} max={maxDateTime} step={60} />
        </TextField>
      ) : null}
    </div>
  );
}

function taskModelIcon(model?: ModelOption, fallbackAlias = "") {
  const alias = model?.alias || fallbackAlias;
  return (
    model?.icon ?? {
      type: "lobe-icons" as const,
      slug: inferModelIconSlug(alias, model?.provider) || alias,
    }
  );
}

function ChoiceDropdown({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Choice[];
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <Label>{label}</Label>
      <Dropdown>
        <Dropdown.Trigger
          aria-label={label}
          className="border-separator bg-default hover:bg-default-hover mt-2 flex h-11 w-full min-w-0 items-center justify-between rounded-2xl border px-3 text-sm"
        >
          <span className="truncate">{value}</span>
          <ChevronDown className="text-muted size-4" />
        </Dropdown.Trigger>
        <Dropdown.Popover placement="bottom start">
          <Dropdown.Menu aria-label={label} onAction={(key) => onChange(String(key))}>
            {options.map((option) => (
              <Dropdown.Item key={option.id} id={option.id} textValue={option.label}>
                {option.label}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown.Popover>
      </Dropdown>
    </div>
  );
}

const scheduleOptions: Choice[] = [
  { id: "once", label: "Does not repeat" },
  { id: "hourly", label: "Every hour" },
  { id: "daily", label: "Every day" },
  { id: "weekly", label: "Every week" },
  { id: "monthly", label: "Every month" },
];
const weekdays = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"].map(
  (label, index) => ({ id: String(index + 1), label }),
);
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
  className?: string;
}) {
  return (
    <Sheet isOpen={open} placement="right" onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[440px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label="Close confirmation" />
            <Sheet.Header>
              <Sheet.Heading>{title}</Sheet.Heading>
              <p className="text-muted text-sm leading-6">{description}</p>
            </Sheet.Header>
            <Sheet.Body>
              <div
                className={`${destructive ? "bg-danger/10 text-danger" : "bg-accent-soft text-accent"} rounded-2xl px-4 py-3 text-sm`}
              >
                {destructive
                  ? "This action cannot be undone."
                  : "Confirm this operation to continue."}
              </div>
            </Sheet.Body>
            <Sheet.Footer className="gap-2">
              <Button variant="outline" onPress={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button
                isPending={busy}
                variant={destructive ? "danger" : "primary"}
                onPress={onConfirm}
              >
                {confirmLabel}
              </Button>
            </Sheet.Footer>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
  );
}
