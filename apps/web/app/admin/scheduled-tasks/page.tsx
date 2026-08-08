"use client";

import { Timer as ClockCountdown } from "lucide-react";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { Button, Card, Dropdown, SearchField } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminAlert,
  AdminDataGrid,
  AdminDrawer,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";
import { TaskConfirmDialog } from "@/components/scheduled-tasks/task-drawer";
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

  const columns: DataGridColumn<ScheduledTask>[] = [
    {
      id: "task",
      header: "Task",
      isRowHeader: true,
      minWidth: 300,
      cell: (task) => (
        <Button
          className="h-auto min-w-0 justify-start px-0 py-1 text-left"
          variant="ghost"
          onPress={() => view(task)}
        >
          <span className="min-w-0">
            <span className="block truncate font-semibold">{task.name}</span>
            <span className="text-muted mt-0.5 block truncate text-xs">{task.prompt}</span>
          </span>
        </Button>
      ),
    },
    {
      id: "owner",
      header: "Owner",
      minWidth: 190,
      cell: (task) =>
        task.owner_user_id ? (
          <span className="min-w-0">
            <span className="block truncate text-sm font-medium">
              {task.owner?.name || task.owner?.email || task.owner_user_id}
            </span>
            {task.owner?.name && task.owner.email ? (
              <span className="text-muted block truncate text-xs">{task.owner.email}</span>
            ) : null}
          </span>
        ) : (
          <AdminStatusBadge tone="amber">Owner required</AdminStatusBadge>
        ),
    },
    {
      id: "schedule",
      header: "Schedule",
      minWidth: 150,
      cell: (task) => <span className="text-muted text-sm">{scheduleLabel(task)}</span>,
    },
    {
      id: "next",
      header: "Next run",
      minWidth: 170,
      cell: (task) => (
        <span className="text-muted text-sm tabular-nums">{formatDateTime(task.next_run_at)}</span>
      ),
    },
    {
      id: "last",
      header: "Last result",
      minWidth: 170,
      cell: (task) => {
        const run = latestRun.get(task.id);
        return run ? (
          <span>
            <span className="block text-sm capitalize">{run.status}</span>
            <span className="text-muted block text-xs tabular-nums">
              {formatDateTime(run.finished_at || run.created_at)}
            </span>
          </span>
        ) : (
          <span className="text-muted">—</span>
        );
      },
    },
    {
      id: "status",
      header: "Status",
      width: 130,
      cell: (task) => <TaskStatus status={task.status} />,
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      width: 80,
      cell: (task) => (
        <Dropdown>
          <Dropdown.Trigger
            aria-label={`Actions for ${task.name}`}
            className="text-muted hover:bg-surface-secondary mx-auto grid size-9 place-items-center rounded-xl"
          >
            <MoreHorizontal className="size-4" />
          </Dropdown.Trigger>
          <Dropdown.Popover placement="bottom end">
            <Dropdown.Menu
              aria-label={`Actions for ${task.name}`}
              onAction={(key) => {
                if (key === "view") view(task);
                if (key === "delete") setDeleteTarget(task);
              }}
            >
              <Dropdown.Item id="view" textValue="View">
                View details
              </Dropdown.Item>
              <Dropdown.Item id="delete" textValue="Delete">
                <Trash2 className="text-danger size-4" />
                <span className="text-danger">Delete</span>
              </Dropdown.Item>
            </Dropdown.Menu>
          </Dropdown.Popover>
        </Dropdown>
      ),
    },
  ];

  return (
    <AdminPage className="admin-theme-green">
      <AdminPageHeader
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

      <AdminErrorDialog
        error={error}
        title="Scheduled task operation failed"
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <SearchField
          aria-label="Search scheduled tasks"
          className="w-full lg:max-w-sm"
          value={query}
          onChange={setQuery}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder="Search task, prompt, or owner" />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        <Segment
          aria-label="Task status filter"
          selectedKey={status}
          onSelectionChange={(key) => setStatus(String(key) as StatusFilter)}
        >
          <Segment.Item id="all">All</Segment.Item>
          <Segment.Item id="active">Active</Segment.Item>
          <Segment.Item id="paused">Paused</Segment.Item>
          <Segment.Item id="completed">Completed</Segment.Item>
          <Segment.Item id="expired">Expired</Segment.Item>
          <Segment.Item id="owner-required">Owner required</Segment.Item>
        </Segment>
      </div>

      <AdminDataGrid
        aria-label="Scheduled tasks"
        columns={columns}
        contentClassName="min-w-[1060px]"
        data={visibleTasks}
        getRowId={(task) => task.id}
        selectionMode="none"
        variant="primary"
        renderEmptyState={() => (
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <ClockCountdown className="text-green-500" />
              </EmptyState.Media>
              <EmptyState.Title>
                {loading
                  ? "Loading tasks"
                  : tasks.length
                    ? "No matching tasks"
                    : "No scheduled tasks"}
              </EmptyState.Title>
              <EmptyState.Description>
                {loading
                  ? "Fetching scheduled work…"
                  : tasks.length
                    ? "Try a different search or status filter."
                    : "Tasks created by users will appear here."}
              </EmptyState.Description>
            </EmptyState.Header>
          </EmptyState>
        )}
      />

      <AdminDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        className="admin-theme-green"
        title={selectedTask?.name || "Task details"}
        description="Read-only scheduled task details"
        footer={
          selectedTask ? (
            <div className="flex items-center justify-between gap-2">
              <Button variant="danger-soft" onPress={() => setDeleteTarget(selectedTask)}>
                <Trash2 className="size-4" /> Delete
              </Button>
              <Button variant="outline" onPress={() => setDrawerOpen(false)}>
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
        className="admin-theme-green"
        onConfirm={() => deleteTarget && void deleteTask(deleteTarget)}
      />
    </AdminPage>
  );
}

function TaskStatus({ status }: { status: ScheduledTask["status"] }) {
  const tone =
    status === "active"
      ? "green"
      : status === "paused"
        ? "amber"
        : status === "completed"
          ? "green"
          : status === "expired"
            ? "red"
            : "neutral";
  return (
    <AdminStatusBadge tone={tone} dot>
      {status}
    </AdminStatusBadge>
  );
}

function TaskDetails({ task, runs }: { task: ScheduledTask; runs: TaskRun[] }) {
  return (
    <div className="space-y-5">
      <Card className="p-4">
        <Card.Content className="grid gap-3 p-0 sm:grid-cols-2">
          <Detail label="Owner">
            {task.owner?.name || task.owner?.email || task.owner_user_id || "Owner required"}
            {task.owner?.name && task.owner.email ? (
              <span className="block text-xs text-muted">{task.owner.email}</span>
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
        </Card.Content>
      </Card>

      <section>
        <h3 className="text-xs font-medium text-muted">Prompt</h3>
        <p className="bg-surface-secondary mt-2 whitespace-pre-wrap rounded-2xl p-4 text-sm leading-6">
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
          <h3 className="text-xs font-medium text-muted">Attachments</h3>
          <div className="mt-2 divide-y divide-border/60 rounded-2xl border border-border/70 bg-surface/60 px-4">
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
          <h3 className="text-xs font-medium text-muted">Recent runs</h3>
          <div className="mt-2 divide-y divide-border/60 rounded-2xl border border-border/70 bg-surface/60 px-4">
            {runs.slice(0, 8).map((run) => (
              <div key={run.id} className="py-3 text-xs">
                <div className="flex items-center justify-between gap-3">
                  <span className="font-medium capitalize">{run.status}</span>
                  <span className="text-muted">
                    {formatDateTime(run.finished_at || run.started_at || run.created_at)}
                  </span>
                </div>
                {run.error ? <p className="mt-1 text-danger">{run.error}</p> : null}
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
      <dt className="text-xs text-muted">{label}</dt>
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
