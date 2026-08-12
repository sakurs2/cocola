"use client";

import { Button, Card, Chip, Dropdown, Input, Label, TextArea, TextField } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import { CalendarClock, ChevronDown, ChevronRight, Paperclip, UserCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
import { ModelIcon } from "@/components/ui/model-icon";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { inferModelIconSlug } from "@/lib/model-icons";
import {
  emptyTaskForm,
  filesToAttachments,
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
  const t = useTranslations("tasks.drawer");
  const format = useFormatter();
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
      setError(t(`validation.${validation}`));
      return;
    }
    if (admin && task && !task.owner_user_id && !ownerUserID) {
      setError(t("ownerRequired"));
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
  const scheduleOptions = useMemo<Choice[]>(
    () =>
      (["once", "hourly", "daily", "weekly", "monthly"] as const).map((id) => ({
        id,
        label: t(`repeatOptions.${id}`),
      })),
    [t],
  );
  const weekdays = useMemo<Choice[]>(
    () =>
      (["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"] as const).map(
        (day, index) => ({ id: String(index + 1), label: t(`weekdays.${day}`) }),
      ),
    [t],
  );

  return (
    <Sheet isOpen={open} placement="right" onOpenChange={(next) => !saving && onOpenChange(next)}>
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[520px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label={t("close")} />
            <Sheet.Header>
              <span className="flex items-center gap-3">
                <span className="bg-accent-soft text-accent flex size-10 shrink-0 items-center justify-center rounded-2xl">
                  <CalendarClock className="size-5" />
                </span>
                <span>
                  <Sheet.Heading>{task ? t("editTitle") : t("newTitle")}</Sheet.Heading>
                  <span className="text-muted mt-1 block text-sm">{t("description")}</span>
                </span>
              </span>
            </Sheet.Header>
            <Sheet.Body className="grid content-start gap-5">
              {admin && task ? (
                <Card className="p-4">
                  <Card.Content className="p-0">
                    <p className="text-muted flex items-center gap-2 text-xs font-medium">
                      <UserCircle className="size-4" />
                      {t("owner")}
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
                        label={t("owner")}
                        value={
                          ownerOptions.find((owner) => owner.id === ownerUserID)?.name ||
                          ownerOptions.find((owner) => owner.id === ownerUserID)?.email ||
                          t("chooseOwner")
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
                  {t("lastError", { error: task.last_error })}
                </div>
              ) : null}

              <TextField
                value={form.name}
                variant="secondary"
                onChange={(name) => setForm({ ...form, name })}
              >
                <Label>{t("name")}</Label>
                <Input autoFocus placeholder={t("namePlaceholder")} />
              </TextField>
              <TextField
                value={form.prompt}
                variant="secondary"
                onChange={(prompt) => setForm({ ...form, prompt })}
              >
                <Label>{t("prompt")}</Label>
                <TextArea rows={5} placeholder={t("promptPlaceholder")} />
              </TextField>
              <ChoiceDropdown
                label={t("repeat")}
                value={
                  scheduleOptions.find((option) => option.id === form.scheduleKind)?.label ||
                  form.scheduleKind
                }
                options={scheduleOptions}
                onChange={(scheduleKind) =>
                  setForm({ ...form, scheduleKind: scheduleKind as TaskFormState["scheduleKind"] })
                }
              />
              <ScheduleFields form={form} setForm={setForm} weekdays={weekdays} />

              <div className="bg-surface-secondary text-muted rounded-2xl px-4 py-3 text-xs">
                {t.rich("timezone", {
                  strong: (chunks) => <span className="text-foreground font-medium">{chunks}</span>,
                  timezone: form.timezone,
                })}
              </div>

              <details className="group border-separator bg-default rounded-2xl border p-4">
                <summary className="flex cursor-pointer list-none items-center gap-2 text-sm font-semibold [&::-webkit-details-marker]:hidden">
                  <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
                  {t("advanced")}
                </summary>
                <div className="border-separator mt-4 grid gap-5 border-t pt-4">
                  <div>
                    <Label>{t("model")}</Label>
                    <Dropdown>
                      <Dropdown.Trigger
                        aria-label={t("selectModel")}
                        className="border-separator bg-surface-secondary hover:bg-default-hover mt-2 flex h-11 w-full items-center justify-between rounded-2xl border px-3 text-sm"
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          <ModelIcon bare className="size-5 shrink-0" icon={selectedModelIcon} />
                          <span className="truncate font-medium">
                            {model?.alias || form.modelAlias || t("modelUnavailable")}
                          </span>
                        </span>
                        <ChevronDown className="text-muted size-4" />
                      </Dropdown.Trigger>
                      <Dropdown.Popover placement="bottom start">
                        <Dropdown.Menu
                          aria-label={t("models")}
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
                    <Label>{t("attachments")}</Label>
                    <label className="border-separator bg-surface-secondary hover:bg-default-hover text-muted mt-2 flex min-h-12 cursor-pointer items-center gap-2 rounded-2xl border border-dashed px-3 text-sm transition-colors">
                      <Paperclip className="size-4" />
                      <span className="truncate">
                        {form.files.length
                          ? form.files.map((file) => file.filename).join(", ")
                          : task?.attachments?.length
                            ? t("savedFiles", { count: task.attachments.length })
                            : t("chooseFiles")}
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
                    <Card.Title>{t("recentRuns")}</Card.Title>
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
                          {format.dateTime(
                            new Date(run.finished_at || run.started_at || run.created_at),
                            { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" },
                          )}
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
                {t("cancel")}
              </Button>
              <Button isPending={saving} onPress={() => void submit()}>
                {scheduleAgain ? t("scheduleAgain") : task ? t("save") : t("create")}
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
  weekdays,
}: {
  form: TaskFormState;
  setForm: (form: TaskFormState) => void;
  weekdays: Choice[];
}) {
  const t = useTranslations("tasks.drawer");
  const minDateTime = toLocalInput(new Date().toISOString(), form.timezone);
  if (form.scheduleKind === "once") {
    return (
      <TextField
        value={form.runAt}
        variant="secondary"
        onChange={(runAt) => setForm({ ...form, runAt: boundedDateTime(runAt, form.runAt) })}
      >
        <Label>{t("runAt")}</Label>
        <Input type="datetime-local" min={minDateTime} max={maxDateTime} step={60} />
      </TextField>
    );
  }
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {form.scheduleKind === "weekly" ? (
        <ChoiceDropdown
          label={t("day")}
          value={weekdays.find((option) => option.id === form.weekday)?.label || t("monday")}
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
          <Label>{t("dayOfMonth")}</Label>
          <Input type="number" min={1} max={31} />
          <span className="text-muted text-xs">{t("shortMonth")}</span>
        </TextField>
      ) : null}
      {form.scheduleKind === "hourly" ? (
        <TextField
          value={form.minute}
          variant="secondary"
          onChange={(minute) => setForm({ ...form, minute })}
        >
          <Label>{t("minute")}</Label>
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
          <Label>{t("time")}</Label>
          <Input type="time" />
        </TextField>
      )}
      <ChoiceDropdown
        label={t("ends")}
        value={form.ends === "never" ? t("never") : t("onDate")}
        options={[
          { id: "never", label: t("never") },
          { id: "on", label: t("onDate") },
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
          <Label>{t("endTime")}</Label>
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
    <ActionConfirmDialog
      busy={busy}
      confirmLabel={confirmLabel}
      description={description}
      open={open}
      title={title}
      tone={destructive ? "danger" : "primary"}
      onConfirm={onConfirm}
      onOpenChange={onOpenChange}
    />
  );
}
