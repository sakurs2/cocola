"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  Archive,
  ChevronRight,
  ExternalLink,
  FolderGit2,
  GitBranch,
  GitFork,
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
  const [tasksLoaded, setTasksLoaded] = useState(false);
  const [composerReady, setComposerReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftDescription, setDraftDescription] = useState("");
  const [draftRuntime, setDraftRuntime] = useState("");
  const [showPublish, setShowPublish] = useState(false);
  const [publishRepository, setPublishRepository] = useState("");
  const [publishVisibility, setPublishVisibility] = useState<"private" | "public">("private");
  const preparedProject = useRef<string | null>(null);
  const preparedSession = useRef<string | null>(null);

  useEffect(() => {
    const cached = projects.find((item) => item.id === projectID);
    if (cached) setProject(cached);
  }, [projectID, projects]);

  useEffect(() => {
    let cancelled = false;
    setProject((current) => (current?.id === projectID ? current : null));
    setTasks([]);
    setTasksLoaded(false);
    setComposerReady(false);
    void Promise.all([
      fetch(`/api/projects/${encodeURIComponent(projectID)}`, { cache: "no-store" }).then(
        async (response) => {
          if (!response.ok) throw new Error("Project not found");
          const loaded = (await response.json()) as ProjectSummary;
          if (!cancelled) setProject(loaded);
        },
      ),
      fetch(`/api/projects/${encodeURIComponent(projectID)}/tasks`, { cache: "no-store" }).then(
        async (response) => {
          if (!response.ok) throw new Error("Could not load project tasks");
          const loaded = (await response.json()) as ProjectTask[];
          if (!cancelled) {
            setTasks(loaded);
            setTasksLoaded(true);
          }
        },
      ),
    ]).catch((loadError) => {
      if (!cancelled) {
        setError(loadError instanceof Error ? loadError.message : "Could not load project");
      }
    });
    return () => {
      cancelled = true;
    };
  }, [projectID]);

  useEffect(() => {
    if (
      !project ||
      !tasksLoaded ||
      project.status !== "ready" ||
      preparedProject.current === project.id ||
      (project.repository_provider === "local" && tasks.length > 0)
    )
      return;
    preparedProject.current = project.id;
    preparedSession.current = newProjectTask(project.id, project.runtime_id);
    setComposerReady(true);
  }, [newProjectTask, project, tasks.length, tasksLoaded]);

  useEffect(
    () => () => {
      if (preparedSession.current) {
        discardPendingProjectTask(preparedSession.current);
      }
      preparedProject.current = null;
      preparedSession.current = null;
    },
    [discardPendingProjectTask, projectID],
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

  const startPublishing = () => {
    if (!project) return;
    const fallback = project.name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 100);
    setPublishRepository(project.repository_name || fallback || "cocola-project");
    setPublishVisibility(project.visibility === "public" ? "public" : "private");
    setShowPublish(true);
  };

  const publish = async () => {
    if (!project || !publishRepository.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(project.id)}/publish`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          expected_version: project.version,
          repository_name: publishRepository.trim(),
          visibility: publishVisibility,
        }),
      });
      const body = (await response.json().catch(() => ({}))) as ProjectSummary & {
        error?: { code?: string; message?: string };
      };
      if (!response.ok) {
        const message =
          body.error?.code === "GITHUB_CONNECTION_REQUIRED"
            ? "Connect GitHub in Connectors before publishing."
            : body.error?.code === "REPOSITORY_NOT_INSTALLED"
              ? "Grant your GitHub App access to the new repository, then retry publishing."
              : body.error?.message || "Could not publish this Project";
        throw new Error(message);
      }
      setProject(body);
      setShowPublish(false);
      refreshProjects();
    } catch (publishError) {
      const latest = await fetch(`/api/projects/${encodeURIComponent(project.id)}`, {
        cache: "no-store",
      }).catch(() => null);
      if (latest?.ok) setProject((await latest.json()) as ProjectSummary);
      setError(
        publishError instanceof Error ? publishError.message : "Could not publish this Project",
      );
    } finally {
      setBusy(false);
    }
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
    if (!project) return;
    const message =
      project.repository_provider === "github"
        ? "Archive this Cocola project? The GitHub repository will not be deleted."
        : "Archive this local project? Its existing workspace remains available for review.";
    if (!window.confirm(message)) return;
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
          <Link href="/projects" className="hover:text-foreground">
            Projects
          </Link>
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
                {project.repository_provider === "github" || project.repository_html_url ? (
                  <a
                    href={project.repository_html_url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 hover:text-foreground"
                  >
                    {project.repository_owner}/{project.repository_name}
                    <ExternalLink className="size-3" />
                  </a>
                ) : (
                  <span>Local only</span>
                )}
                <span className="inline-flex items-center gap-1">
                  <GitBranch className="size-3" />
                  {project.default_branch || "Preparing"}
                </span>
                {project.repository_provider === "github" || project.repository_html_url ? (
                  <span className="capitalize">{project.visibility}</span>
                ) : null}
              </div>
            </div>
            {project.status !== "archived" ? (
              <div className="flex shrink-0 items-center gap-1">
                {project.repository_provider === "local" &&
                project.github_publish_status !== "published" &&
                project.primary_conversation_id ? (
                  <button
                    type="button"
                    onClick={startPublishing}
                    className="inline-flex h-9 items-center gap-2 rounded-xl border border-border px-3 text-xs font-medium hover:bg-muted"
                  >
                    <GitFork className="size-4" />
                    {project.github_publish_status === "pending" ? "Retry publish" : "Publish"}
                  </button>
                ) : null}
                <button
                  type="button"
                  onClick={startEditing}
                  className="grid size-9 place-items-center rounded-xl text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <Pencil className="size-4" />
                </button>
              </div>
            ) : null}
          </div>
        </section>

        {showPublish ? (
          <section className="mt-5 rounded-2xl border border-border bg-card p-5">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="text-sm font-semibold">Publish to GitHub</h2>
                <p className="mt-1 text-xs text-muted-foreground">
                  Cocola will push the committed main branch using a short-lived repository token.
                </p>
              </div>
              <button
                type="button"
                onClick={() => setShowPublish(false)}
                className="text-xs text-muted-foreground hover:text-foreground"
              >
                Cancel
              </button>
            </div>
            <div className="mt-4 grid gap-4 sm:grid-cols-2">
              <label className="space-y-1.5">
                <span className="text-xs font-medium">Repository name</span>
                <input
                  value={publishRepository}
                  disabled={project.github_publish_status === "pending"}
                  onChange={(event) => setPublishRepository(event.target.value)}
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm disabled:opacity-60"
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">Visibility</span>
                <select
                  value={publishVisibility}
                  disabled={project.github_publish_status === "pending"}
                  onChange={(event) =>
                    setPublishVisibility(event.target.value === "public" ? "public" : "private")
                  }
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm disabled:opacity-60"
                >
                  <option value="private">Private</option>
                  <option value="public">Public</option>
                </select>
              </label>
            </div>
            <div className="mt-4 flex items-center justify-between gap-3">
              <p className="text-xs text-muted-foreground">
                Uncommitted changes must be committed before publishing.
              </p>
              <button
                type="button"
                disabled={busy || !publishRepository.trim()}
                onClick={() => void publish()}
                className="inline-flex h-9 items-center gap-2 rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-50"
              >
                {busy ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <GitFork className="size-4" />
                )}
                Publish
              </button>
            </div>
          </section>
        ) : null}

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
        ) : project.repository_provider === "local" && tasks.length > 0 ? (
          <section className="mt-8 rounded-2xl border border-border bg-muted/30 p-5">
            <h2 className="text-sm font-semibold">Project workspace</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Empty Projects keep one persistent conversation and develop directly on main.
            </p>
            <button
              type="button"
              onClick={() =>
                router.push(
                  `/projects/${encodeURIComponent(project.id)}/tasks/${encodeURIComponent(tasks[0]!.id)}`,
                )
              }
              className="mt-4 inline-flex h-9 items-center rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground"
            >
              Continue workspace
            </button>
          </section>
        ) : !tasksLoaded || !composerReady ? (
          <section className="mt-8 rounded-2xl border border-border bg-muted/30 p-5">
            <h2 className="text-sm font-semibold">Preparing Project workspace</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Loading Project tasks and preparing a conversation workspace…
            </p>
          </section>
        ) : (
          <section className="mt-8">
            <h2 className="text-sm font-semibold">
              {project.repository_provider === "local" ? "Start workspace" : "New task"}
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              {project.repository_provider === "local"
                ? "The first message creates this Project’s single persistent workspace on main."
                : "A task gets its own conversation workspace and branch from the current default revision."}
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
            <h2 className="text-sm font-semibold">
              {project.repository_provider === "local" ? "Workspace" : "Tasks"}
            </h2>
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
