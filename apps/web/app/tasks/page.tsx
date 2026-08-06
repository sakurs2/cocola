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
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  WorkspacePageAction,
  WorkspacePageFrame,
  WorkspacePageHeader,
} from "@/components/heroui-workspace/workspace-ui";
import { TaskConfirmDialog, TaskDrawer } from "@/components/scheduled-tasks/task-drawer";
import { Badge } from "@/components/ui/badge";
import {
  formatDateTime,
  normalizeModelOptions,
  scheduleLabel,
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

  const columns: DataGridColumn<ScheduledTask>[] = [
    {
      id: "task",
      header: "Task",
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
      header: "Schedule",
      minWidth: 210,
      cell: (task) => (
        <span className="text-muted block min-w-0 text-sm">
          <span className="flex items-center gap-2 truncate">
            <CalendarClock className="size-4 shrink-0" />
            {scheduleLabel(task)}
          </span>
          <span className="mt-1 block truncate pl-6 text-xs">
            Next: {formatDateTime(task.next_run_at)}
          </span>
        </span>
      ),
    },
    {
      id: "lastResult",
      header: "Last result",
      minWidth: 190,
      cell: (task) => <TaskLastResult task={task} />,
    },
    {
      id: "status",
      header: "Status",
      minWidth: 110,
      cell: (task) => (
        <Chip color={statusColor(task.status)} size="sm" variant="soft">
          {statusLabel(task.status)}
        </Chip>
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      pinned: "end",
      width: 80,
      cell: (task) => (
        <Dropdown>
          <Tooltip delay={0}>
            <Dropdown.Trigger
              aria-label={`Actions for ${task.name}`}
              className="text-muted hover:bg-surface-secondary hover:text-foreground mx-auto grid size-9 place-items-center rounded-xl"
            >
              <Ellipsis className="size-4" />
            </Dropdown.Trigger>
            <Tooltip.Content>Task actions</Tooltip.Content>
          </Tooltip>
          <Dropdown.Popover placement="bottom end">
            <Dropdown.Menu
              aria-label={`Actions for ${task.name}`}
              onAction={(key) => handleAction(task, String(key))}
            >
              <Dropdown.Item id="edit" textValue="Edit">
                Edit
              </Dropdown.Item>
              {task.status === "active" || task.status === "paused" ? (
                <Dropdown.Item
                  id="toggle"
                  textValue={task.status === "paused" ? "Resume" : "Pause"}
                >
                  {task.status === "paused" ? "Resume" : "Pause"}
                </Dropdown.Item>
              ) : null}
              <Dropdown.Item
                id="result"
                isDisabled={!task.conversation_id || task.run_count === 0}
                textValue="View latest result"
              >
                View latest result
              </Dropdown.Item>
              <Dropdown.Item id="delete" textValue="Delete">
                Delete
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
            New task
          </WorkspacePageAction>
        }
        description="Schedule prompts to run once or on a recurring cadence."
        icon={<Clock className="size-5" />}
        title="Tasks"
      />

      {error || modelError ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
          {error || modelError}
        </div>
      ) : null}

      <Segment
        aria-label="Task filter"
        className="self-start"
        selectedKey={tab}
        onSelectionChange={(key) => setTab(String(key) as Tab)}
      >
        <Segment.Item id="today">Today · {todayCount}</Segment.Item>
        <Segment.Item id="all">All · {tasks.length}</Segment.Item>
      </Segment>

      {loading ? (
        <div className="grid min-h-56 place-items-center" role="status">
          {showLoadingIndicator ? (
            <LoaderCircle className="text-muted size-5 animate-spin" />
          ) : null}
          <span className="sr-only">Loading tasks</span>
        </div>
      ) : (
        <DataGrid
          aria-label="Scheduled tasks"
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
                  {tab === "today" && tasks.length
                    ? "Nothing scheduled for today"
                    : "Create your first task"}
                </EmptyState.Title>
                <EmptyState.Description>
                  {tab === "today" && tasks.length
                    ? "Your other tasks are available under All."
                    : "Describe the work once, then let Cocola run it at the right time."}
                </EmptyState.Description>
              </EmptyState.Header>
              <EmptyState.Content>
                {tab === "today" && tasks.length ? (
                  <Button size="sm" variant="outline" onPress={() => setTab("all")}>
                    Show all tasks
                  </Button>
                ) : (
                  <Button
                    isDisabled={!modelsLoaded}
                    size="sm"
                    variant="outline"
                    onPress={openCreate}
                  >
                    <Plus className="size-4" /> New task
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
        confirmLabel="Delete task"
        description={`“${deleteTarget?.name ?? "This task"}” and its schedule will be removed. Its existing conversation history will remain.`}
        destructive
        open={deleteTarget !== null}
        title="Delete task?"
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

function statusLabel(status: ScheduledTask["status"]) {
  return status.charAt(0).toUpperCase() + status.slice(1);
}

function TaskLastResult({ task }: { task: ScheduledTask }) {
  const result = scheduledTaskResultView(task);
  const hasAdditionalDetail = result.detail !== result.label;

  return (
    <span className="flex min-w-0 items-center gap-1">
      <Tooltip delay={0}>
        <span className="inline-flex min-w-0">
          <Badge variant={taskResultBadgeVariant[result.tone]}>{result.label}</Badge>
        </span>
        <Tooltip.Content className="max-w-sm break-words">{result.detail}</Tooltip.Content>
      </Tooltip>
      {hasAdditionalDetail ? <TaskResultCopyButton detail={result.detail} /> : null}
    </span>
  );
}

function TaskResultCopyButton({ detail }: { detail: string }) {
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
        aria-label={copied ? "Last result copied" : "Copy full last result"}
        className="text-muted size-7 min-w-7 shrink-0"
        size="sm"
        variant="ghost"
        onClick={(event) => event.stopPropagation()}
        onPress={() => void copyDetail()}
      >
        {copied ? <Check className="text-success size-3.5" /> : <Copy className="size-3.5" />}
      </Button>
      <Tooltip.Content>{copied ? "Copied" : "Copy full result"}</Tooltip.Content>
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
