"use client";

import { Button, Chip, Dropdown, Tooltip } from "@heroui/react";
import { DataGrid, type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import {
  AlarmClock,
  CalendarClock,
  Check,
  Clock,
  Copy,
  Ellipsis,
  LoaderCircle,
  Plus,
} from "lucide-react";
import { useSession } from "next-auth/react";
import { useRouter } from "next/navigation";
import { useFormatter, useTranslations } from "next-intl";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  WorkspacePageAction,
  WorkspacePageFrame,
  WorkspacePageHeader,
} from "@/components/heroui-workspace/workspace-ui";
import { TaskConfirmDialog, TaskDrawer } from "@/components/scheduled-tasks/task-drawer";
import { Badge } from "@/components/ui/badge";
import {
  normalizeModelOptions,
  sortTasks,
  taskIsToday,
  taskPayload,
  type ModelOption,
  type ScheduledTask,
  type TaskFormState,
} from "@/lib/scheduled-tasks";
import {
  readScheduledTaskPageCache,
  writeScheduledTaskPageCache,
} from "@/lib/scheduled-task-page-cache.mjs";
import { scheduledTaskResultView } from "@/lib/scheduled-task-result";

const taskResultBadgeVariant = {
  default: "default",
  success: "success",
  warning: "warning",
  danger: "danger",
  accent: "brand",
} as const;

type Tab = "today" | "all";

export default function TasksPage() {
  const t = useTranslations("tasks");
  const common = useTranslations("common.actions");
  const format = useFormatter();
  const router = useRouter();
  const { data: session, status: sessionStatus } = useSession();
  const ownerID = session?.user?.id ?? "";
  const initialCache = readScheduledTaskPageCache(ownerID);
  const [tasks, setTasks] = useState<ScheduledTask[]>(() => initialCache?.tasks ?? []);
  const [models, setModels] = useState<ModelOption[]>(() => initialCache?.models ?? []);
  const [tab, setTab] = useState<Tab>("today");
  const [loading, setLoading] = useState(() => initialCache?.tasks == null);
  const [modelsLoaded, setModelsLoaded] = useState(() => initialCache?.models != null);
  const [showLoadingIndicator, setShowLoadingIndicator] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [modelError, setModelError] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectedTask, setSelectedTask] = useState<ScheduledTask | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ScheduledTask | null>(null);

  const loadTasks = useCallback(
    async (options?: { foreground?: boolean; signal?: AbortSignal }) => {
      if (!ownerID) return;
      const foreground = options?.foreground ?? false;
      if (foreground) setLoading(true);
      setError("");
      try {
        const response = await fetch("/api/scheduled-tasks", {
          cache: "no-store",
          signal: options?.signal,
        });
        if (!response.ok) throw new Error(await responseError(response));
        const body = (await response.json()) as { tasks?: ScheduledTask[] };
        const nextTasks = Array.isArray(body.tasks) ? body.tasks : [];
        if (options?.signal?.aborted) return;
        setTasks(nextTasks);
        writeScheduledTaskPageCache(ownerID, { tasks: nextTasks });
      } catch (cause) {
        if (!options?.signal?.aborted)
          setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (foreground && !options?.signal?.aborted) setLoading(false);
      }
    },
    [ownerID],
  );

  const loadModels = useCallback(
    async (signal?: AbortSignal) => {
      if (!ownerID) return;
      setModelError("");
      try {
        const response = await fetch("/api/models", { cache: "no-store", signal });
        if (!response.ok) throw new Error(await responseError(response));
        const availableModels = normalizeModelOptions(await response.json());
        const nextModels = availableModels.filter(
          (model) => !model.protocols || model.protocols.includes("anthropic-messages"),
        );
        if (signal?.aborted) return;
        setModels(nextModels);
        setModelsLoaded(true);
        writeScheduledTaskPageCache(ownerID, { models: nextModels });
      } catch (cause) {
        if (signal?.aborted) return;
        setModelsLoaded(readScheduledTaskPageCache(ownerID)?.models != null);
        setModelError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [ownerID],
  );

  useEffect(() => {
    if (sessionStatus === "loading") return;
    if (!ownerID) {
      setTasks([]);
      setModels([]);
      setLoading(false);
      setModelsLoaded(false);
      return;
    }
    const cached = readScheduledTaskPageCache(ownerID);
    setTasks(cached?.tasks ?? []);
    setModels(cached?.models ?? []);
    setLoading(cached?.tasks == null);
    setModelsLoaded(cached?.models != null);
    const controller = new AbortController();
    void loadTasks({ foreground: cached?.tasks == null, signal: controller.signal });
    void loadModels(controller.signal);
    return () => controller.abort();
  }, [loadModels, loadTasks, ownerID, sessionStatus]);

  useEffect(() => {
    if (!loading) {
      setShowLoadingIndicator(false);
      return;
    }
    const timer = window.setTimeout(() => setShowLoadingIndicator(true), 180);
    return () => window.clearTimeout(timer);
  }, [loading]);

  const sortedTasks = useMemo(() => sortTasks(tasks), [tasks]);
  const visibleTasks = useMemo(
    () => (tab === "today" ? sortedTasks.filter(taskIsToday) : sortedTasks),
    [tab, sortedTasks],
  );
  const todayCount = useMemo(() => sortedTasks.filter(taskIsToday).length, [sortedTasks]);

  function openCreate() {
    if (!modelsLoaded) return;
    setSelectedTask(null);
    setDrawerOpen(true);
  }

  function openEdit(task: ScheduledTask) {
    setSelectedTask(task);
    setDrawerOpen(true);
  }

  async function save(form: TaskFormState) {
    setSaving(true);
    try {
      const editing = selectedTask !== null;
      const response = await fetch(
        editing
          ? `/api/scheduled-tasks/${encodeURIComponent(selectedTask.id)}`
          : "/api/scheduled-tasks",
        {
          method: editing ? "PATCH" : "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(
            taskPayload(form, {
              includeAttachments: !editing || form.files.length > 0,
              status:
                selectedTask?.status === "completed" || selectedTask?.status === "expired"
                  ? "active"
                  : selectedTask?.status,
            }),
          ),
        },
      );
      if (!response.ok) throw new Error(await responseError(response));
      setDrawerOpen(false);
      await loadTasks();
    } finally {
      setSaving(false);
    }
  }

  async function mutate(task: ScheduledTask, action: "pause" | "resume" | "delete") {
    setSaving(true);
    setError("");
    try {
      const response = await fetch(
        action === "delete"
          ? `/api/scheduled-tasks/${encodeURIComponent(task.id)}`
          : `/api/scheduled-tasks/${encodeURIComponent(task.id)}/${action}`,
        { method: action === "delete" ? "DELETE" : "POST" },
      );
      if (!response.ok) throw new Error(await responseError(response));
      setDeleteTarget(null);
      await loadTasks();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  const handleAction = (task: ScheduledTask, action: string) => {
    if (action === "edit") openEdit(task);
    if (action === "toggle") void mutate(task, task.status === "paused" ? "resume" : "pause");
    if (action === "result" && task.conversation_id) {
      router.push(`/?conversation=${encodeURIComponent(task.conversation_id)}`);
    }
    if (action === "delete") setDeleteTarget(task);
  };

  const localizedDateTime = (value?: string) => {
    if (!value || Number.isNaN(Date.parse(value))) return "—";
    return format.dateTime(new Date(value), {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const localizedScheduleLabel = (task: ScheduledTask) => {
    const spec = task.schedule_spec ?? {};
    const minute = String(Number(spec.minute ?? 0)).padStart(2, "0");
    const hour = String(Number(spec.hour ?? 0)).padStart(2, "0");
    const time = `${hour}:${minute}`;
    if (task.schedule_kind === "once") {
      return t("schedule.once", { date: localizedDateTime(String(spec.run_at ?? "")) });
    }
    if (task.schedule_kind === "hourly") return t("schedule.hourly", { minute });
    if (task.schedule_kind === "daily") return t("schedule.daily", { time });
    if (task.schedule_kind === "weekly") {
      const weekday = Math.min(7, Math.max(1, Number(spec.weekday ?? 1)));
      const date = new Date(Date.UTC(2024, 0, weekday));
      return t("schedule.weekly", {
        time,
        weekday: format.dateTime(date, { weekday: "long", timeZone: "UTC" }),
      });
    }
    return t("schedule.monthly", { day: Number(spec.day ?? 1), time });
  };

  const columns: DataGridColumn<ScheduledTask>[] = [
    {
      id: "task",
      header: t("columns.task"),
      isRowHeader: true,
      minWidth: 300,
      cell: (task) => (
        <div className="flex min-w-0 items-center gap-3 py-1">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-2xl bg-blue-100 text-blue-600 dark:bg-blue-950 dark:text-blue-300">
            <AlarmClock className="size-5" />
          </span>
          <span className="min-w-0">
            <span className="text-foreground block truncate text-sm font-semibold">
              {task.name}
            </span>
            <span className="text-muted mt-1 block truncate text-xs">{task.prompt}</span>
          </span>
        </div>
      ),
    },
    {
      id: "schedule",
      header: t("columns.schedule"),
      minWidth: 210,
      cell: (task) => (
        <span className="text-muted block min-w-0 text-sm">
          <span className="flex items-center gap-2 truncate">
            <CalendarClock className="size-4 shrink-0" />
            {localizedScheduleLabel(task)}
          </span>
          <span className="mt-1 block truncate pl-6 text-xs">
            {t("next", { date: localizedDateTime(task.next_run_at) })}
          </span>
        </span>
      ),
    },
    {
      id: "lastResult",
      header: t("columns.lastResult"),
      minWidth: 190,
      cell: (task) => <TaskLastResult task={task} />,
    },
    {
      id: "status",
      header: t("columns.status"),
      minWidth: 110,
      cell: (task) => (
        <Chip color={statusColor(task.status)} size="sm" variant="soft">
          {t(`status.${task.status}`)}
        </Chip>
      ),
    },
    {
      id: "actions",
      header: t("columns.actions"),
      align: "center",
      pinned: "end",
      width: 80,
      cell: (task) => (
        <Dropdown>
          <Tooltip delay={0}>
            <Dropdown.Trigger
              aria-label={t("actionsFor", { name: task.name })}
              className="text-muted hover:bg-surface-secondary hover:text-foreground mx-auto grid size-9 place-items-center rounded-xl"
            >
              <Ellipsis className="size-4" />
            </Dropdown.Trigger>
            <Tooltip.Content>{t("taskActions")}</Tooltip.Content>
          </Tooltip>
          <Dropdown.Popover placement="bottom end">
            <Dropdown.Menu
              aria-label={t("actionsFor", { name: task.name })}
              onAction={(key) => handleAction(task, String(key))}
            >
              <Dropdown.Item id="edit" textValue={t("edit")}>
                {t("edit")}
              </Dropdown.Item>
              {task.status === "active" || task.status === "paused" ? (
                <Dropdown.Item
                  id="toggle"
                  textValue={task.status === "paused" ? t("resume") : t("pause")}
                >
                  {task.status === "paused" ? t("resume") : t("pause")}
                </Dropdown.Item>
              ) : null}
              <Dropdown.Item
                id="result"
                isDisabled={!task.conversation_id || task.run_count === 0}
                textValue={t("viewResult")}
              >
                {t("viewResult")}
              </Dropdown.Item>
              <Dropdown.Item id="delete" textValue={common("delete")}>
                {common("delete")}
              </Dropdown.Item>
            </Dropdown.Menu>
          </Dropdown.Popover>
        </Dropdown>
      ),
    },
  ];

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        action={
          <WorkspacePageAction isDisabled={!modelsLoaded} onPress={openCreate}>
            <Plus className="size-4" />
            {t("new")}
          </WorkspacePageAction>
        }
        description={t("description")}
        icon={<Clock className="size-5" />}
        title={t("title")}
      />

      {error || modelError ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
          {error || modelError}
        </div>
      ) : null}

      <Segment
        aria-label={t("filter")}
        className="self-start"
        selectedKey={tab}
        onSelectionChange={(key) => setTab(String(key) as Tab)}
      >
        <Segment.Item id="today">{t("today", { count: todayCount })}</Segment.Item>
        <Segment.Item id="all">{t("all", { count: tasks.length })}</Segment.Item>
      </Segment>

      {loading ? (
        <div className="grid min-h-56 place-items-center" role="status">
          {showLoadingIndicator ? (
            <LoaderCircle className="text-muted size-5 animate-spin" />
          ) : null}
          <span className="sr-only">{t("loading")}</span>
        </div>
      ) : (
        <DataGrid
          aria-label={t("grid")}
          className="cocola-web-task-grid"
          columns={columns}
          contentClassName="min-w-[880px]"
          data={visibleTasks}
          getRowId={(task) => task.id}
          selectionMode="none"
          scrollContainerClassName="overflow-x-auto"
          verticalAlign="middle"
          variant="primary"
          renderEmptyState={() => (
            <EmptyState>
              <EmptyState.Header>
                <EmptyState.Media variant="icon">
                  <Clock className="text-blue-500" />
                </EmptyState.Media>
                <EmptyState.Title>
                  {tab === "today" && tasks.length ? t("todayEmpty") : t("empty")}
                </EmptyState.Title>
                <EmptyState.Description>
                  {tab === "today" && tasks.length
                    ? t("todayEmptyDescription")
                    : t("emptyDescription")}
                </EmptyState.Description>
              </EmptyState.Header>
              <EmptyState.Content>
                {tab === "today" && tasks.length ? (
                  <Button size="sm" variant="outline" onPress={() => setTab("all")}>
                    {t("showAll")}
                  </Button>
                ) : (
                  <Button
                    isDisabled={!modelsLoaded}
                    size="sm"
                    variant="outline"
                    onPress={openCreate}
                  >
                    <Plus className="size-4" /> {t("new")}
                  </Button>
                )}
              </EmptyState.Content>
            </EmptyState>
          )}
          onRowAction={(key) => {
            const task = tasks.find((item) => item.id === String(key));
            if (task) openEdit(task);
          }}
        />
      )}

      <TaskDrawer
        defaultModelID={models.find((model) => model.is_default)?.id ?? models[0]?.id}
        models={models}
        open={drawerOpen}
        saving={saving}
        task={selectedTask}
        onOpenChange={setDrawerOpen}
        onSave={save}
      />
      <TaskConfirmDialog
        busy={saving}
        confirmLabel={t("deleteConfirm")}
        description={t("deleteDescription", { name: deleteTarget?.name ?? t("thisTask") })}
        destructive
        open={deleteTarget !== null}
        title={t("deleteTitle")}
        onConfirm={() => deleteTarget && void mutate(deleteTarget, "delete")}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      />
    </WorkspacePageFrame>
  );
}

function statusColor(status: ScheduledTask["status"]) {
  if (status === "active" || status === "completed") return "success" as const;
  if (status === "paused") return "warning" as const;
  return "danger" as const;
}

function TaskLastResult({ task }: { task: ScheduledTask }) {
  const t = useTranslations("tasks.lastResult");
  const result = scheduledTaskResultView(task);
  const label = result.key === "unknown" ? result.fallbackLabel || t("unknown") : t(result.key);
  const detail = result.detail || label;
  const hasAdditionalDetail = detail !== label;

  return (
    <span className="flex min-w-0 items-center gap-1">
      <Tooltip delay={0}>
        <span className="inline-flex min-w-0">
          <Badge variant={taskResultBadgeVariant[result.tone]}>{label}</Badge>
        </span>
        <Tooltip.Content className="max-w-sm break-words">{detail}</Tooltip.Content>
      </Tooltip>
      {hasAdditionalDetail ? <TaskResultCopyButton detail={detail} /> : null}
    </span>
  );
}

function TaskResultCopyButton({ detail }: { detail: string }) {
  const t = useTranslations("tasks");
  const [copied, setCopied] = useState(false);

  async function copyDetail() {
    await navigator.clipboard.writeText(detail);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  }

  return (
    <Tooltip delay={0}>
      <Button
        isIconOnly
        aria-label={copied ? t("lastResultCopied") : t("copyFullResult")}
        className="text-muted size-7 min-w-7 shrink-0"
        size="sm"
        variant="ghost"
        onClick={(event) => event.stopPropagation()}
        onPress={() => void copyDetail()}
      >
        {copied ? <Check className="text-success size-3.5" /> : <Copy className="size-3.5" />}
      </Button>
      <Tooltip.Content>{copied ? t("copied") : t("copyResult")}</Tooltip.Content>
    </Tooltip>
  );
}

async function responseError(response: Response): Promise<string> {
  const text = await response.text();
  if (!text) return `${response.status} ${response.statusText}`;
  try {
    const body = JSON.parse(text) as {
      error?: string | { code?: string; message?: string };
      message?: string;
    };
    if (typeof body.error === "object") return body.error.message || body.error.code || text;
    return body.message || body.error || text;
  } catch {
    return text;
  }
}
