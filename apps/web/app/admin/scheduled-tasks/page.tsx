"use client";

import {
  Timer as ClockCountdown,
} from "lucide-react";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminAlert,
  AdminDrawer,
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTable,
  AdminToolbar,
} from "@/components/admin/admin-ui";
import { TaskConfirmDialog } from "@/components/scheduled-tasks/task-drawer";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  formatDateTime,
  scheduleLabel,
  sortTasks,
  type ScheduledTask,
  type TaskRun,
} from "@/lib/scheduled-tasks";

type StatusFilter = "all" | ScheduledTask["status"] | "owner-required";

export default function ScheduledTasksPage() {
  const [tasks, setTasks] = useState<ScheduledTask[]>([]);
  const [runs, setRuns] = useState<TaskRun[]>([]);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [selectedTask, setSelectedTask] = useState<ScheduledTask | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ScheduledTask | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [tasksResponse, runsResponse] = await Promise.all([
        fetch("/api/admin/scheduled-tasks", { cache: "no-store" }),
        fetch("/api/admin/scheduled-task-runs?limit=200", { cache: "no-store" }),
      ]);
      if (!tasksResponse.ok) throw new Error(await responseError(tasksResponse));
      if (!runsResponse.ok) throw new Error(await responseError(runsResponse));
      const taskBody = (await tasksResponse.json()) as { tasks?: ScheduledTask[] };
      const runBody = (await runsResponse.json()) as { runs?: TaskRun[] };
      setTasks(Array.isArray(taskBody.tasks) ? taskBody.tasks : []);
      setRuns(Array.isArray(runBody.runs) ? runBody.runs : []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const latestRun = useMemo(() => {
    const map = new Map<string, TaskRun>();
    for (const run of runs) if (!map.has(run.task_id)) map.set(run.task_id, run);
    return map;
  }, [runs]);

  const visibleTasks = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return sortTasks(tasks).filter((task) => {
      const statusMatches =
        status === "all" ||
        (status === "owner-required" ? !task.owner_user_id : task.status === status);
      if (!statusMatches) return false;
      if (!needle) return true;
      return [task.name, task.prompt, task.owner?.name, task.owner?.email]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle));
    });
  }, [query, status, tasks]);

  function view(task: ScheduledTask) {
    setSelectedTask(task);
    setDrawerOpen(true);
  }

  async function deleteTask(task: ScheduledTask) {
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/admin/scheduled-tasks/${encodeURIComponent(task.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await responseError(response));
      setDeleteTarget(null);
      if (selectedTask?.id === task.id) {
        setDrawerOpen(false);
        setSelectedTask(null);
      }
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <AdminPage>
      <AdminPageHeader
        eyebrow="Operations"
        title="Tasks"
        description="Review scheduled work across all users. Tasks can only be changed by their owners."
        icon={<ClockCountdown className="size-5" />}
        actions={
          <AdminRefreshButton
            variant="outline"
            refreshing={loading}
            disabled={loading}
            onClick={() => void load()}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      {error ? <AdminAlert tone="error">{error}</AdminAlert> : null}

      <AdminToolbar>
        <label className="min-w-64 flex-1">
          <span className="sr-only">Search scheduled tasks</span>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search task, prompt, or owner"
            className="h-10 w-full rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-ring focus:ring-2 focus:ring-ring/20"
          />
        </label>
        <label>
          <span className="sr-only">Filter by status</span>
          <select
            value={status}
            onChange={(event) => setStatus(event.target.value as StatusFilter)}
            className="h-10 min-w-44 rounded-xl border border-input bg-background px-3 text-sm text-foreground outline-none"
          >
            <option value="all">All statuses</option>
            <option value="active">Active</option>
            <option value="paused">Paused</option>
            <option value="completed">Completed</option>
            <option value="expired">Expired</option>
            <option value="owner-required">Owner required</option>
          </select>
        </label>
      </AdminToolbar>

      <AdminTable>
        {loading ? (
          <div className="p-10 text-center text-sm text-muted-foreground">Loading tasks…</div>
        ) : visibleTasks.length === 0 ? (
          <AdminEmptyState
            icon={<ClockCountdown className="size-6" />}
            title={tasks.length ? "No matching tasks" : "No scheduled tasks"}
            description={
              tasks.length
                ? "Try a different search or status filter."
                : "Tasks created by users will appear here."
            }
          />
        ) : (
          <table className="w-full min-w-[960px] text-left text-sm">
            <thead className="sticky top-0 z-10 bg-background/90 text-xs text-muted-foreground backdrop-blur-xl">
              <tr className="border-b border-border/70">
                <th className="px-4 py-3 font-medium">Task</th>
                <th className="px-4 py-3 font-medium">Owner</th>
                <th className="px-4 py-3 font-medium">Schedule</th>
                <th className="px-4 py-3 font-medium">Next run</th>
                <th className="px-4 py-3 font-medium">Last result</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="w-14 px-4 py-3">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/60">
              {visibleTasks.map((task) => {
                const run = latestRun.get(task.id);
                return (
                  <tr
                    key={task.id}
                    tabIndex={0}
                    onClick={() => view(task)}
                    onKeyDown={(event) =>
                      event.target === event.currentTarget && event.key === "Enter" && view(task)
                    }
                    className="cursor-pointer hover:bg-muted/35 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/40"
                  >
                    <td className="max-w-xs px-4 py-3">
                      <div className="truncate font-medium">{task.name}</div>
                      <div className="mt-0.5 truncate text-xs text-muted-foreground">
                        {task.prompt}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      {task.owner_user_id ? (
                        <>
                          <div className="font-medium">
                            {task.owner?.name || task.owner?.email || task.owner_user_id}
                          </div>
                          {task.owner?.name && task.owner.email ? (
                            <div className="text-xs text-muted-foreground">{task.owner.email}</div>
                          ) : null}
                        </>
                      ) : (
                        <AdminStatusBadge tone="amber">Owner required</AdminStatusBadge>
                      )}
                    </td>
                    <td className="px-4 py-3 text-xs">{scheduleLabel(task)}</td>
                    <td className="px-4 py-3 font-mono text-xs tabular-nums">
                      {formatDateTime(task.next_run_at)}
                    </td>
                    <td className="px-4 py-3">
                      {run ? (
                        <div>
                          <span className="capitalize">{run.status}</span>
                          <div className="text-xs text-muted-foreground">
                            {formatDateTime(run.finished_at || run.created_at)}
                          </div>
                        </div>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <TaskStatus status={task.status} />
                    </td>
                    <td className="px-4 py-3" onClick={(event) => event.stopPropagation()}>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`Actions for ${task.name}`}
                          >
                            <MoreHorizontal className="size-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent
                          align="end"
                          className="cocola-admin-ui rounded-xl border-white/70 bg-popover/95 shadow-xl backdrop-blur-xl"
                        >
                          <DropdownMenuItem onSelect={() => view(task)}>View</DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-destructive focus:text-destructive"
                            onSelect={() => setDeleteTarget(task)}
                          >
                            <Trash2 className="size-4" /> Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </AdminTable>

      <AdminDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        title={selectedTask?.name || "Task details"}
        description="Read-only scheduled task details"
        footer={
          selectedTask ? (
            <div className="flex items-center justify-between gap-2">
              <Button variant="destructive" onClick={() => setDeleteTarget(selectedTask)}>
                <Trash2 className="size-4" /> Delete
              </Button>
              <Button variant="outline" onClick={() => setDrawerOpen(false)}>
                Close
              </Button>
            </div>
          ) : null
        }
      >
        {selectedTask ? (
          <TaskDetails
            task={selectedTask}
            runs={runs.filter((run) => run.task_id === selectedTask.id)}
          />
        ) : null}
      </AdminDrawer>
      <TaskConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title="Delete scheduled task?"
        description={`“${deleteTarget?.name ?? "This task"}” will no longer run. Existing conversation history remains with its owner.`}
        confirmLabel="Delete task"
        busy={saving}
        destructive
        admin
        onConfirm={() => deleteTarget && void deleteTask(deleteTarget)}
      />
    </AdminPage>
  );
}

function TaskStatus({ status }: { status: ScheduledTask["status"] }) {
  const tone = status === "active" ? "green" : status === "paused" ? "amber" : "neutral";
  return (
    <AdminStatusBadge tone={tone} dot>
      {status}
    </AdminStatusBadge>
  );
}

function TaskDetails({ task, runs }: { task: ScheduledTask; runs: TaskRun[] }) {
  return (
    <div className="space-y-5">
      <dl className="grid gap-3 rounded-2xl border border-border/70 bg-card/60 p-4 sm:grid-cols-2">
        <Detail label="Owner">
          {task.owner?.name || task.owner?.email || task.owner_user_id || "Owner required"}
          {task.owner?.name && task.owner.email ? (
            <span className="block text-xs text-muted-foreground">{task.owner.email}</span>
          ) : null}
        </Detail>
        <Detail label="Status">
          <TaskStatus status={task.status} />
        </Detail>
        <Detail label="Schedule">{scheduleLabel(task)}</Detail>
        <Detail label="Timezone">{task.timezone || "—"}</Detail>
        <Detail label="Next run">{formatDateTime(task.next_run_at)}</Detail>
        <Detail label="Last run">{formatDateTime(task.last_run_at)}</Detail>
        <Detail label="Last result">
          <span className="capitalize">{task.last_status || "—"}</span>
        </Detail>
        <Detail label="Ends">{formatDateTime(task.expires_at)}</Detail>
        <Detail label="Model">
          <span className="font-mono text-xs">{task.model_alias || "—"}</span>
        </Detail>
        <Detail label="Runs">
          <span className="tabular-nums">{task.run_count}</span>
        </Detail>
      </dl>

      <section>
        <h3 className="text-xs font-medium text-muted-foreground">Prompt</h3>
        <p className="mt-2 whitespace-pre-wrap rounded-2xl border border-border/70 bg-muted/25 p-4 text-sm leading-6">
          {task.prompt}
        </p>
      </section>

      {task.last_error ? (
        <AdminAlert tone="error">
          <span className="font-medium">Last error:</span> {task.last_error}
        </AdminAlert>
      ) : null}

      {task.attachments?.length ? (
        <section>
          <h3 className="text-xs font-medium text-muted-foreground">Attachments</h3>
          <div className="mt-2 divide-y divide-border/60 rounded-2xl border border-border/70 bg-card/60 px-4">
            {task.attachments.map((attachment) => (
              <div key={attachment.id || attachment.filename} className="py-2.5 text-sm">
                {attachment.filename}
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {runs.length ? (
        <section>
          <h3 className="text-xs font-medium text-muted-foreground">Recent runs</h3>
          <div className="mt-2 divide-y divide-border/60 rounded-2xl border border-border/70 bg-card/60 px-4">
            {runs.slice(0, 8).map((run) => (
              <div key={run.id} className="py-3 text-xs">
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
        </section>
      ) : null}
    </div>
  );
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 break-words text-sm font-medium text-foreground">{children}</dd>
    </div>
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
