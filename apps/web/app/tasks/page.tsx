"use client";

import {
  CalendarCheck,
  CalendarClock,
  CalendarDays,
  CircleCheck,
  Clock,
  LoaderCircle,
  MoreHorizontal,
  Plus,
  Sparkles,
} from "lucide-react";
import { useSession } from "next-auth/react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { TaskConfirmDialog, TaskDrawer } from "@/components/scheduled-tasks/task-drawer";
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
import { cn } from "@/lib/utils";

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
        const tasksResponse = await fetch("/api/scheduled-tasks", {
          cache: "no-store",
          signal: options?.signal,
        });
        if (!tasksResponse.ok) throw new Error(await responseError(tasksResponse));
        const taskBody = (await tasksResponse.json()) as { tasks?: ScheduledTask[] };
        const nextTasks = Array.isArray(taskBody.tasks) ? taskBody.tasks : [];
        if (options?.signal?.aborted) return;
        setTasks(nextTasks);
        writeScheduledTaskPageCache(ownerID, { tasks: nextTasks });
      } catch (cause) {
        if (options?.signal?.aborted) return;
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
        const body = (await response.json()) as unknown;
        const availableModels = normalizeModelOptions(body);
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
    if (cached?.tasks != null) {
      setTasks(cached.tasks);
      setLoading(false);
    } else {
      setTasks([]);
      setLoading(true);
    }
    if (cached?.models != null) {
      setModels(cached.models);
      setModelsLoaded(true);
    } else {
      setModels([]);
      setModelsLoaded(false);
    }
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

  const metrics = useMemo(() => {
    const total = tasks.length;
    const active = tasks.filter((task) => task.status === "active").length;
    const dueToday = sortedTasks.filter(taskIsToday).length;
    return { total, active, dueToday };
  }, [tasks, sortedTasks]);

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

  const hasTasks = !loading && tasks.length > 0;

  return (
    <div className="user-canvas user-page user-theme-blue h-full overflow-y-auto px-4 py-5 sm:px-7 sm:py-7 lg:px-10">
      <div className="mx-auto max-w-7xl">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex items-center gap-3.5">
            <span className="user-page-icon">
              <CalendarCheck className="size-6" />
            </span>
            <div className="space-y-1">
              <div className="user-eyebrow">Automation</div>
              <h1 className="text-2xl font-bold tracking-tight">Tasks</h1>
              <p className="text-sm text-muted-foreground">
                Schedule Cocola to work for you, even when you are away.
              </p>
            </div>
          </div>
          {hasTasks ? (
            <button
              type="button"
              onClick={openCreate}
              disabled={!modelsLoaded}
              className="user-accent-btn inline-flex h-11 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Plus className="size-4" /> New task
            </button>
          ) : null}
        </header>

        {error || modelError ? (
          <div className="mt-5 rounded-2xl border border-destructive/25 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error || modelError}
          </div>
        ) : null}

        {hasTasks ? (
          <div className="mt-7 grid gap-4 sm:grid-cols-3">
            <MetricCard
              tone="blue"
              icon={<CalendarDays className="size-[22px]" />}
              label="Total tasks"
              value={metrics.total}
              detail="Across all schedules"
            />
            <MetricCard
              tone="emerald"
              icon={<CircleCheck className="size-[22px]" />}
              label="Active"
              value={metrics.active}
              detail="Running on schedule"
            />
            <MetricCard
              tone="sky"
              icon={<Clock className="size-[22px]" />}
              label="Due today"
              value={metrics.dueToday}
              detail="Next run within 24h"
            />
          </div>
        ) : null}

        {hasTasks ? (
          <div className="mt-7 flex gap-6 border-b border-border/60">
            {([
              ["today", todayCount],
              ["all", tasks.length],
            ] as const).map(([value, count]) => (
              <button
                key={value}
                type="button"
                onClick={() => setTab(value)}
                className={cn(
                  "relative flex items-center gap-2 pb-3 text-sm capitalize text-muted-foreground transition-colors hover:text-foreground",
                  tab === value && "font-medium text-foreground",
                )}
              >
                {value}
                <span className="rounded-full border border-border bg-muted/60 px-1.5 py-px text-[11px] font-semibold tabular-nums text-muted-foreground">
                  {count}
                </span>
                {tab === value ? (
                  <span
                    className="absolute inset-x-0 bottom-0 h-0.5 rounded-full"
                    style={{ background: "var(--page-accent-grad)" }}
                  />
                ) : null}
              </button>
            ))}
          </div>
        ) : null}

        {loading ? (
          <div
            className="flex min-h-[13.75rem] items-center justify-center py-7 text-muted-foreground"
            role="status"
            aria-live="polite"
          >
            {showLoadingIndicator ? <LoaderCircle className="size-5 animate-spin" /> : null}
            <span className="sr-only">Loading tasks</span>
          </div>
        ) : visibleTasks.length ? (
          <div className="grid gap-4 py-7 md:grid-cols-2 xl:grid-cols-3">
            {visibleTasks.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                onEdit={() => openEdit(task)}
                onToggle={() => void mutate(task, task.status === "paused" ? "resume" : "pause")}
                onResult={() =>
                  task.conversation_id &&
                  router.push(`/?conversation=${encodeURIComponent(task.conversation_id)}`)
                }
                onDelete={() => setDeleteTarget(task)}
              />
            ))}
          </div>
        ) : (
          <div className="flex min-h-[55vh] flex-col items-center justify-center text-center">
            <span className="user-page-icon size-14 rounded-2xl">
              <Clock className="size-7" />
            </span>
            <h2 className="mt-4 text-base font-semibold">
              {tab === "today" && tasks.length
                ? "Nothing scheduled for today"
                : "Create your first task"}
            </h2>
            <p className="mt-1 max-w-sm text-sm leading-6 text-muted-foreground">
              {tab === "today" && tasks.length
                ? "Your other tasks are available under All."
                : "Describe the work once, then let Cocola run it at the right time."}
            </p>
            {tab === "today" && tasks.length ? (
              <Button variant="outline" className="mt-4 rounded-xl" onClick={() => setTab("all")}>
                View all tasks
              </Button>
            ) : (
              <button
                type="button"
                disabled={!modelsLoaded}
                onClick={openCreate}
                className="user-accent-btn mt-4 inline-flex h-11 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Plus className="size-4" /> New task
              </button>
            )}
          </div>
        )}
      </div>

      <TaskDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        task={selectedTask}
        models={models}
        defaultModelID={models.find((model) => model.is_default)?.id ?? models[0]?.id}
        saving={saving}
        onSave={save}
      />
      <TaskConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title="Delete task?"
        description={`“${deleteTarget?.name ?? "This task"}” and its schedule will be removed. Its existing conversation history will remain.`}
        confirmLabel="Delete task"
        busy={saving}
        destructive
        onConfirm={() => deleteTarget && void mutate(deleteTarget, "delete")}
      />
    </div>
  );
}

function MetricCard({
  tone,
  icon,
  label,
  value,
  detail,
}: {
  tone: "blue" | "cyan" | "emerald" | "sky";
  icon: React.ReactNode;
  label: string;
  value: number;
  detail: string;
}) {
  return (
    <div className="user-metric-card" data-tone={tone}>
      <div className="user-metric-head">
        <span className="user-metric-glyph">{icon}</span>
        <span className="user-metric-key">{label}</span>
      </div>
      <div className="user-metric-val">{value}</div>
      <div className="user-metric-detail">{detail}</div>
    </div>
  );
}

function TaskCard({
  task,
  onEdit,
  onToggle,
  onResult,
  onDelete,
}: {
  task: ScheduledTask;
  onEdit: () => void;
  onToggle: () => void;
  onResult: () => void;
  onDelete: () => void;
}) {
  return (
    <article
      role="button"
      tabIndex={0}
      onClick={onEdit}
      onKeyDown={(event) =>
        event.target === event.currentTarget && event.key === "Enter" && onEdit()
      }
      className="user-card user-card--hover group relative min-h-44 cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
    >
      <div className="flex items-start gap-3">
        <span className="user-card-glyph mt-0.5">
          <Sparkles className="size-[18px]" />
        </span>
        <div className="min-w-0 flex-1 pr-7">
          <div className="flex items-center gap-2">
            <h2 className="user-card-name truncate">{task.name}</h2>
            <StatusBadge status={task.status} />
          </div>
          <p className="user-card-desc mt-1 line-clamp-2 min-h-10">{task.prompt}</p>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={`Actions for ${task.name}`}
              onClick={(event) => event.stopPropagation()}
              className="absolute right-3.5 top-3.5 grid size-8 shrink-0 place-items-center rounded-xl text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
            >
              <MoreHorizontal className="size-5" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="end"
            onClick={(event) => event.stopPropagation()}
            className="cocola-user-ui rounded-xl border-border bg-popover shadow-xl"
          >
            <DropdownMenuItem onSelect={onEdit}>Edit</DropdownMenuItem>
            {(task.status === "active" || task.status === "paused") && (
              <DropdownMenuItem onSelect={onToggle}>
                {task.status === "paused" ? "Resume" : "Pause"}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem
              disabled={!task.conversation_id || task.run_count === 0}
              onSelect={onResult}
            >
              View latest result
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onSelect={onDelete}
            >
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className="mt-4 border-t border-border/45 pt-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-2 text-foreground/80">
          <CalendarClock className="size-4" style={{ color: "var(--page-accent)" }} />
          <span className="truncate">{scheduleLabel(task)}</span>
        </div>
        <div className="mt-1.5 pl-6 tabular-nums">Next: {formatDateTime(task.next_run_at)}</div>
      </div>
    </article>
  );
}

function StatusBadge({ status }: { status: ScheduledTask["status"] }) {
  if (status === "active") {
    return (
      <span className="user-tag user-tag--ok shrink-0 text-[10px]">
        <span className="user-tag-dot" />
        active
      </span>
    );
  }
  if (status === "paused") {
    return (
      <span className="user-tag user-tag--warn shrink-0 text-[10px]">
        <span className="user-tag-dot" />
        paused
      </span>
    );
  }
  return (
    <span className="user-tag user-tag--muted shrink-0 capitalize text-[10px]">{status}</span>
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
