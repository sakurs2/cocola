"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import {
  Archive,
  ChevronRight,
  ExternalLink,
  FolderGit2,
  GitBranch,
  Loader2,
  Pencil,
  RefreshCw,
} from "lucide-react";
import { useCocola, type ProjectSummary } from "@/app/runtime-provider";
import { ConversationComposer } from "@/components/assistant-ui/thread";

type ProjectTask = {
  id: string;
  title: string;
  runtime_id: string;
  created_at: string;
  updated_at: string;
  workspace: {
    branch_name: string;
    bootstrap_status: string;
    git_snapshot?: { dirty?: boolean; captured_at?: string };
  };
};

export default function ProjectPage() {
  const params = useParams<{ id: string }>();
  const projectID = params.id;
  const router = useRouter();
  const {
    projects,
    projectsLoaded,
    refreshProjects,
    newProjectTask,
    discardPendingProjectTask,
    activeSessionId,
    runningSessionIds,
    runtimes,
  } = useCocola();
  const [project, setProject] = useState<ProjectSummary | null>(
    projects.find((item) => item.id === projectID) ?? null,
  );
  const [tasks, setTasks] = useState<ProjectTask[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftDescription, setDraftDescription] = useState("");
  const [draftRuntime, setDraftRuntime] = useState("");
  const preparedProject = useRef<string | null>(null);
  const preparedSession = useRef<string | null>(null);

  useEffect(() => {
    const cached = projects.find((item) => item.id === projectID);
    if (cached) setProject(cached);
  }, [projectID, projects]);

  useEffect(() => {
    void Promise.all([
      fetch(`/api/projects/${encodeURIComponent(projectID)}`, { cache: "no-store" }).then(
        async (response) => {
          if (!response.ok) throw new Error("Project not found");
          setProject((await response.json()) as ProjectSummary);
        },
      ),
      fetch(`/api/projects/${encodeURIComponent(projectID)}/tasks`, { cache: "no-store" }).then(
        async (response) => {
          if (!response.ok) throw new Error("Could not load project tasks");
          setTasks((await response.json()) as ProjectTask[]);
        },
      ),
    ]).catch((loadError) =>
      setError(loadError instanceof Error ? loadError.message : "Could not load project"),
    );
  }, [projectID]);

  useEffect(() => {
    if (!project || project.status !== "ready" || preparedProject.current === project.id) return;
    preparedProject.current = project.id;
    preparedSession.current = newProjectTask(project.id, project.runtime_id);
  }, [newProjectTask, project]);

  useEffect(
    () => () => {
      if (preparedSession.current) {
        discardPendingProjectTask(preparedSession.current);
      }
      preparedProject.current = null;
      preparedSession.current = null;
    },
    [discardPendingProjectTask],
  );

  useEffect(() => {
    if (
      preparedProject.current === projectID &&
      preparedSession.current === activeSessionId &&
      runningSessionIds.has(activeSessionId)
    ) {
      router.push(
        `/projects/${encodeURIComponent(projectID)}/tasks/${encodeURIComponent(activeSessionId)}`,
      );
    }
  }, [activeSessionId, projectID, router, runningSessionIds]);

  const retry = async () => {
    setBusy(true);
    setError(null);
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(projectID)}/retry`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      });
      if (!response.ok) throw new Error("Could not reconcile the GitHub repository");
      const value = (await response.json()) as ProjectSummary;
      setProject(value);
      refreshProjects();
    } catch (retryError) {
      setError(retryError instanceof Error ? retryError.message : "Retry failed");
    } finally {
      setBusy(false);
    }
  };

  const startEditing = () => {
    if (!project) return;
    setDraftName(project.name);
    setDraftDescription(project.description);
    setDraftRuntime(project.runtime_id);
    setEditing(true);
  };

  const saveSettings = async () => {
    if (!project) return;
    setBusy(true);
    setError(null);
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(project.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          expected_version: project.version,
          name: draftName.trim(),
          description: draftDescription.trim(),
          runtime_id: draftRuntime,
        }),
      });
      if (!response.ok) throw new Error("Could not save project settings");
      setProject((await response.json()) as ProjectSummary);
      setEditing(false);
      refreshProjects();
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "Could not save project");
    } finally {
      setBusy(false);
    }
  };

  const archive = async () => {
    if (
      !project ||
      !window.confirm("Archive this Cocola project? The GitHub repository will not be deleted.")
    )
      return;
    setBusy(true);
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(project.id)}`, {
        method: "DELETE",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ expected_version: project.version }),
      });
      if (!response.ok) throw new Error("Could not archive project");
      refreshProjects();
      router.push("/");
    } catch (archiveError) {
      setError(archiveError instanceof Error ? archiveError.message : "Could not archive project");
      setBusy(false);
    }
  };

  if (!project && !projectsLoaded && !error)
    return (
      <div className="grid h-full place-items-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  if (!project)
    return (
      <div className="grid h-full place-items-center px-6 text-center">
        <div>
          <FolderGit2 className="mx-auto size-9 text-muted-foreground" />
          <h1 className="mt-3 text-lg font-semibold">Project not found</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {error || "It may have been archived or belongs to another account."}
          </p>
        </div>
      </div>
    );

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8 lg:px-12">
      <main className="mx-auto w-full max-w-4xl pb-16">
        <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <span>Projects</span>
          <ChevronRight className="size-3.5" />
          <span className="truncate text-foreground/75">{project.name}</span>
        </div>
        <section className="mt-7 rounded-2xl border border-border bg-card p-5 sm:p-6">
          <div className="flex items-start gap-4">
            <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-indigo-500/10 text-indigo-600">
              <FolderGit2 className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <h1 className="truncate text-2xl font-semibold tracking-tight">{project.name}</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                {project.description || "No description"}
              </p>
              <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                <a
                  href={project.repository_html_url}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 hover:text-foreground"
                >
                  {project.repository_owner}/{project.repository_name}
                  <ExternalLink className="size-3" />
                </a>
                <span className="inline-flex items-center gap-1">
                  <GitBranch className="size-3" />
                  {project.default_branch || "Preparing"}
                </span>
                <span className="capitalize">{project.visibility}</span>
              </div>
            </div>
            {project.status !== "archived" ? (
              <button
                type="button"
                onClick={startEditing}
                className="grid size-9 shrink-0 place-items-center rounded-xl text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <Pencil className="size-4" />
              </button>
            ) : null}
          </div>
        </section>

        {project.repository_has_lfs || project.repository_has_submodules ? (
          <section className="mt-5 rounded-2xl border border-amber-500/25 bg-amber-500/5 px-5 py-4 text-sm">
            <span className="font-medium">Repository notice</span>
            <span className="ml-2 text-muted-foreground">
              {project.repository_has_lfs && project.repository_has_submodules
                ? "Git LFS objects and submodules are not downloaded in phase one."
                : project.repository_has_lfs
                  ? "Git LFS objects are kept as pointer files in phase one."
                  : "Git submodules are not initialized in phase one."}
            </span>
          </section>
        ) : null}

        {project.status === "archived" ? (
          <section className="mt-5 rounded-2xl border border-border bg-muted/40 p-5">
            <h2 className="font-semibold">Project archived</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              New tasks are disabled. Existing tasks and saved Git snapshots remain available.
            </p>
          </section>
        ) : project.status !== "ready" ? (
          <section className="mt-5 rounded-2xl border border-amber-500/25 bg-amber-500/5 p-5">
            <h2 className="font-semibold">Project {project.status}</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {project.provision_error_code || "GitHub repository provisioning has not completed."}
            </p>
            <button
              type="button"
              disabled={busy}
              onClick={() => void retry()}
              className="mt-4 inline-flex items-center gap-2 rounded-xl border border-border bg-background px-3 py-2 text-sm"
            >
              <RefreshCw className="size-4" /> Retry reconciliation
            </button>
          </section>
        ) : (
          <section className="mt-8">
            <h2 className="text-sm font-semibold">New task</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              A task gets its own conversation workspace and branch from the current default
              revision.
            </p>
            <div className="mt-4">
              <ConversationComposer placeholder={`Ask Cocola to work on ${project.name}…`} />
            </div>
          </section>
        )}

        {editing ? (
          <section className="mt-8 rounded-2xl border border-border p-5">
            <div className="flex items-center justify-between">
              <h2 className="font-semibold">Project settings</h2>
              <button
                type="button"
                onClick={() => setEditing(false)}
                className="text-sm text-muted-foreground"
              >
                Cancel
              </button>
            </div>
            <div className="mt-4 grid gap-4 sm:grid-cols-2">
              <label className="space-y-1.5">
                <span className="text-sm font-medium">Name</span>
                <input
                  value={draftName}
                  onChange={(event) => setDraftName(event.target.value)}
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm"
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-sm font-medium">Default runtime</span>
                <select
                  value={draftRuntime}
                  onChange={(event) => setDraftRuntime(event.target.value)}
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm"
                >
                  {runtimes.map((runtime) => (
                    <option key={runtime.id} value={runtime.id}>
                      {runtime.label}
                    </option>
                  ))}
                </select>
              </label>
              <label className="space-y-1.5 sm:col-span-2">
                <span className="text-sm font-medium">Description</span>
                <input
                  value={draftDescription}
                  onChange={(event) => setDraftDescription(event.target.value)}
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm"
                />
              </label>
            </div>
            <div className="mt-5 flex items-center justify-between">
              <button
                type="button"
                disabled={busy}
                onClick={() => void archive()}
                className="inline-flex items-center gap-2 text-sm text-red-600"
              >
                <Archive className="size-4" /> Archive
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() => void saveSettings()}
                className="rounded-xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
              >
                Save
              </button>
            </div>
          </section>
        ) : null}

        <section className="mt-12">
          <div className="border-b border-border pb-3">
            <h2 className="text-sm font-semibold">Tasks</h2>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {tasks.length} {tasks.length === 1 ? "task" : "tasks"}
            </p>
          </div>
          {tasks.length === 0 ? (
            <div className="py-10 text-center text-sm text-muted-foreground">
              No project tasks yet.
            </div>
          ) : (
            <div className="divide-y divide-border/70">
              {tasks.map((task) => (
                <button
                  type="button"
                  key={task.id}
                  onClick={() =>
                    router.push(
                      `/projects/${encodeURIComponent(project.id)}/tasks/${encodeURIComponent(task.id)}`,
                    )
                  }
                  className="flex w-full items-center gap-3 rounded-xl px-3 py-4 text-left hover:bg-muted"
                >
                  <div className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">
                      {task.title || "Untitled task"}
                    </span>
                    <span className="mt-1 inline-flex items-center gap-1 text-xs text-muted-foreground">
                      <GitBranch className="size-3" />
                      {task.workspace.branch_name}
                    </span>
                  </div>
                  {task.workspace.git_snapshot?.dirty ? (
                    <span className="rounded-full bg-amber-500/10 px-2 py-1 text-[11px] font-medium text-amber-700">
                      Modified
                    </span>
                  ) : null}
                  <ChevronRight className="size-4 text-muted-foreground" />
                </button>
              ))}
            </div>
          )}
        </section>
        {error ? (
          <p className="mt-5 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-600">{error}</p>
        ) : null}
      </main>
    </div>
  );
}
