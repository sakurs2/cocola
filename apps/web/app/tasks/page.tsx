"use client";

import { CalendarClock, Clock, LoaderCircle, MoreHorizontal, Plus, Sparkles } from "lucide-react";
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
        const body = (await response.json()) as ModelOption[] | { models?: ModelOption[] };
        const availableModels = Array.isArray(body)
          ? body
          : Array.isArray(body.models)
            ? body.models
            : [];
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

  const visibleTasks = useMemo(() => {
    const sorted = sortTasks(tasks);
    return tab === "today" ? sorted.filter(taskIsToday) : sorted;
  }, [tab, tasks]);

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

  return (
    <div className="h-full overflow-y-auto px-4 py-5 sm:px-7 sm:py-7 lg:px-10">
      <div className="mx-auto max-w-7xl">
        <header className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold tracking-[-0.03em]">Tasks</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Schedule Cocola to work for you, even when you are away.
            </p>
          </div>
          {!loading && tasks.length ? (
            <Button onClick={openCreate} disabled={!modelsLoaded} className="rounded-xl">
              <Plus className="size-4" /> New task
            </Button>
          ) : null}
        </header>

        <div className="mt-7 flex gap-6 border-b border-border/60">
          {(["today", "all"] as const).map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setTab(value)}
              className={cn(
                "relative pb-3 text-sm capitalize text-muted-foreground transition-colors hover:text-foreground",
                tab === value && "font-medium text-foreground",
              )}
            >
              {value}
              {tab === value ? (
                <span className="absolute inset-x-0 bottom-0 h-0.5 rounded-full bg-primary" />
              ) : null}
            </button>
          ))}
        </div>

        {error || modelError ? (
          <div className="mt-5 rounded-2xl border border-destructive/25 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error || modelError}
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
            <span className="grid size-14 place-items-center rounded-2xl bg-sky-500/10 text-sky-600">
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
              <Button
                variant="outline"
                className="mt-4 rounded-xl"
                disabled={!modelsLoaded}
                onClick={openCreate}
              >
                <Plus className="size-4" /> New task
              </Button>
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
      className="group relative min-h-44 cursor-pointer rounded-2xl border border-border bg-card p-5 shadow-card transition duration-200 hover:-translate-y-0.5 hover:shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
    >
      <div className="flex items-start gap-3">
        <span className="mt-0.5 grid size-9 shrink-0 place-items-center rounded-2xl bg-sky-500/10 text-sky-600">
          <Sparkles className="size-[18px]" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-sm font-semibold">{task.name}</h2>
            <StatusBadge status={task.status} />
          </div>
          <p className="mt-1 line-clamp-2 min-h-10 text-sm leading-5 text-muted-foreground">
            {task.prompt}
          </p>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={`Actions for ${task.name}`}
              onClick={(event) => event.stopPropagation()}
              className="grid size-8 shrink-0 place-items-center rounded-xl text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
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
          <CalendarClock className="size-4 text-sky-600" />
          <span className="truncate">{scheduleLabel(task)}</span>
        </div>
        <div className="mt-1.5 pl-6 tabular-nums">Next: {formatDateTime(task.next_run_at)}</div>
      </div>
    </article>
  );
}

function StatusBadge({ status }: { status: ScheduledTask["status"] }) {
  return (
    <span
      className={cn(
        "shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-medium capitalize",
        status === "active" && "border-emerald-500/20 bg-emerald-500/10 text-emerald-700",
        status === "paused" && "border-amber-500/20 bg-amber-500/10 text-amber-700",
        (status === "completed" || status === "expired") &&
          "border-border bg-muted/60 text-muted-foreground",
      )}
    >
      {status}
    </span>
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
