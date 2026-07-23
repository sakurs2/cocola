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
import {
  ProjectBaseBranchPicker,
  ProjectBranchBadge,
} from "@/components/assistant-ui/project-branch-control";
import { ConversationComposer } from "@/components/assistant-ui/thread";
import { shouldOpenProjectTask } from "@/lib/project-task-intent.mjs";

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

const BRAND_GRADIENT = "linear-gradient(135deg,#2563eb,#7c3aed)";

const STATUS_META: Record<ProjectSummary["status"], { label: string; color: string }> = {
  ready: { label: "Ready", color: "#16a34a" },
  provisioning: { label: "Provisioning", color: "#d97706" },
  failed: { label: "Failed", color: "#dc2626" },
  archived: { label: "Archived", color: "#6b7280" },
};

function initials(name: string) {
  const parts = name.replace(/[_/-]/g, " ").split(/\s+/).filter(Boolean);
  const raw = parts.length > 1 ? `${parts[0]![0]}${parts[1]![0]}` : name.slice(0, 2);
  return raw.toUpperCase();
}

export default function ProjectPage() {
  const params = useParams<{ id: string }>();
  const projectID = params.id;
  const router = useRouter();
  const {
    projects,
    projectsLoaded,
    refreshProjects,
    newProjectTask,
    updatePendingProjectTaskBaseRef,
    discardPendingProjectTask,
    activeSessionId,
    serverAcceptedSessionIds,
    runtimes,
    runtimePickerEnabled,
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
  const [selectedBaseRef, setSelectedBaseRef] = useState("");
  const [publishRepository, setPublishRepository] = useState("");
  const [publishVisibility, setPublishVisibility] = useState<"private" | "public">("private");
  const preparedProject = useRef<string | null>(null);
  const preparedSession = useRef<string | null>(null);
  const initializedBaseProject = useRef<string | null>(null);

  useEffect(() => {
    const cached = projects.find((item) => item.id === projectID);
    if (cached) setProject(cached);
  }, [projectID, projects]);

  useEffect(() => {
    if (!project || initializedBaseProject.current === project.id) return;
    initializedBaseProject.current = project.id;
    setSelectedBaseRef(project.default_branch || "main");
  }, [project]);

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
      !selectedBaseRef ||
      preparedProject.current === project.id ||
      (project.repository_provider === "local" && tasks.length > 0)
    )
      return;
    preparedProject.current = project.id;
    preparedSession.current = newProjectTask(project.id, project.runtime_id, selectedBaseRef);
    setComposerReady(true);
  }, [newProjectTask, project, selectedBaseRef, tasks.length, tasksLoaded]);

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

  const selectBaseRef = (branch: string) => {
    if (
      preparedSession.current &&
      !updatePendingProjectTaskBaseRef(preparedSession.current, branch)
    ) {
      return;
    }
    setSelectedBaseRef(branch);
  };

  useEffect(() => {
    if (
      shouldOpenProjectTask({
        projectId: projectID,
        preparedProjectId: preparedProject.current,
        activeSessionId,
        preparedSessionId: preparedSession.current,
        serverAccepted: serverAcceptedSessionIds.has(activeSessionId),
      })
    ) {
      router.push(
        `/projects/${encodeURIComponent(projectID)}/tasks/${encodeURIComponent(activeSessionId)}`,
      );
    }
  }, [activeSessionId, projectID, router, serverAcceptedSessionIds]);

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
          runtime_id: draftRuntime || project.runtime_id,
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

  const status = STATUS_META[project.status];
  const isGithub = project.repository_provider === "github" || Boolean(project.repository_html_url);

  return (
    <div className="h-full overflow-y-auto px-3 py-8 sm:px-5">
      <main className="mx-auto w-full max-w-5xl pb-16">
        {/* Breadcrumb */}
        <div className="flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
          <Link href="/projects" className="transition-colors hover:text-foreground">
            Projects
          </Link>
          <ChevronRight className="size-3" />
          <span className="truncate normal-case tracking-normal text-foreground">
            {project.name}
          </span>
        </div>

        {/* Editorial header with a strong baseline rule */}
        <header className="mt-4 flex flex-wrap items-start justify-between gap-4 border-b-2 border-foreground pb-6">
          <div className="flex min-w-0 items-start gap-4">
            {/* monogram */}
            <div
              className="grid size-14 shrink-0 place-items-center rounded-2xl text-2xl font-bold tracking-tight text-white shadow-[inset_0_-10px_20px_-12px_rgba(0,0,0,0.4)]"
              style={{ background: BRAND_GRADIENT }}
            >
              {initials(project.name)}
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2.5">
                <h1 className="truncate text-3xl font-semibold tracking-tight">{project.name}</h1>
                {isGithub ? (
                  <span className="shrink-0 rounded-md border border-border px-1.5 py-px font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                    {project.visibility}
                  </span>
                ) : null}
              </div>
              <p
                className={
                  "mt-1 text-sm text-muted-foreground" + (project.description ? "" : " opacity-50")
                }
              >
                {project.description || "No description"}
              </p>
              {/* meta row */}
              <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
                {isGithub ? (
                  <a
                    href={project.repository_html_url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 transition-colors hover:text-foreground"
                  >
                    <GitFork className="size-3.5" />
                    {project.repository_owner}/{project.repository_name}
                    <ExternalLink className="size-3" />
                  </a>
                ) : (
                  <span className="inline-flex items-center gap-1.5">
                    <FolderGit2 className="size-3.5" />
                    Local only
                  </span>
                )}
                <span className="inline-flex items-center gap-1.5">
                  <GitBranch className="size-3.5" />
                  {project.default_branch || "Preparing"}
                </span>
                <span className="inline-flex items-center gap-1.5" style={{ color: status.color }}>
                  <span className="size-[7px] rounded-full" style={{ background: status.color }} />
                  {status.label}
                </span>
              </div>
            </div>
          </div>
          {project.status !== "archived" ? (
            <div className="flex shrink-0 items-center gap-2">
              {project.repository_provider === "local" &&
              project.github_publish_status !== "published" &&
              project.primary_conversation_id ? (
                <button
                  type="button"
                  onClick={startPublishing}
                  className="inline-flex h-9 items-center gap-2 rounded-full border border-border bg-background px-4 text-xs font-semibold text-foreground transition-colors hover:bg-muted"
                >
                  <GitFork className="size-4" />
                  {project.github_publish_status === "pending" ? "Retry publish" : "Publish"}
                </button>
              ) : null}
              <button
                type="button"
                onClick={startEditing}
                aria-label="Edit project"
                className="grid size-9 place-items-center rounded-full border border-border bg-background text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              >
                <Pencil className="size-4" />
              </button>
            </div>
          ) : null}
        </header>

        {showPublish ? (
          <section className="mt-6 rounded-2xl border border-border bg-card p-5">
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
                className="text-xs text-muted-foreground transition-colors hover:text-foreground"
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
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition-colors focus:border-primary disabled:opacity-60"
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
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition-colors focus:border-primary disabled:opacity-60"
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
                className="inline-flex h-9 items-center gap-2 rounded-full px-4 text-sm font-semibold text-white shadow-lg shadow-primary/20 transition-transform hover:-translate-y-0.5 disabled:opacity-50 disabled:hover:translate-y-0"
                style={{ background: BRAND_GRADIENT }}
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
          <section className="mt-6 flex items-start gap-3 rounded-2xl border border-amber-500/25 bg-amber-500/5 px-5 py-4 text-sm">
            <span className="mt-1.5 size-[7px] shrink-0 rounded-full bg-amber-500" />
            <p>
              <span className="font-semibold">Repository notice</span>
              <span className="ml-2 text-muted-foreground">
                {project.repository_has_lfs && project.repository_has_submodules
                  ? "Git LFS objects and submodules are not downloaded in phase one."
                  : project.repository_has_lfs
                    ? "Git LFS objects are kept as pointer files in phase one."
                    : "Git submodules are not initialized in phase one."}
              </span>
            </p>
          </section>
        ) : null}

        {project.status === "archived" ? (
          <section className="mt-6 rounded-2xl border border-border bg-muted/40 p-5">
            <h2 className="font-semibold">Project archived</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              New tasks are disabled. Existing tasks and saved Git snapshots remain available.
            </p>
          </section>
        ) : project.status !== "ready" ? (
          <section className="mt-6 rounded-2xl border border-amber-500/25 bg-amber-500/5 p-5">
            <div className="flex items-center gap-2">
              <span
                className="size-[7px] shrink-0 rounded-full"
                style={{ background: status.color }}
              />
              <h2 className="font-semibold capitalize">Project {project.status}</h2>
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              {project.provision_error_code || "GitHub repository provisioning has not completed."}
            </p>
            <button
              type="button"
              disabled={busy}
              onClick={() => void retry()}
              className="mt-4 inline-flex items-center gap-2 rounded-full border border-border bg-background px-4 py-2 text-sm font-semibold transition-colors hover:bg-muted disabled:opacity-50"
            >
              <RefreshCw className="size-4" /> Retry reconciliation
            </button>
          </section>
        ) : project.repository_provider === "local" && tasks.length > 0 ? (
          <section className="mt-8 rounded-2xl border border-border bg-muted/30 p-6">
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
              className="mt-4 inline-flex h-10 items-center rounded-full px-5 text-sm font-semibold text-white shadow-lg shadow-primary/20 transition-transform hover:-translate-y-0.5"
              style={{ background: BRAND_GRADIENT }}
            >
              Continue workspace
            </button>
          </section>
        ) : !tasksLoaded || !composerReady ? (
          <section className="mt-8 rounded-2xl border border-border bg-muted/30 p-6">
            <h2 className="text-sm font-semibold">Preparing Project workspace</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Loading Project tasks and preparing a conversation workspace…
            </p>
          </section>
        ) : (
          <section className="mt-8">
            <p className="font-mono text-[11px] uppercase tracking-[0.22em] text-muted-foreground">
              {project.repository_provider === "local" ? "Start workspace" : "New task"}
            </p>
            <p className="mt-1.5 text-xs text-muted-foreground">
              {project.repository_provider === "local"
                ? "The first message creates this Project’s single persistent workspace on main."
                : "Choose a base branch. Cocola locks its current revision when you send the first message."}
            </p>
            <div className="mt-4">
              <ConversationComposer
                placeholder={`Ask Cocola to work on ${project.name}…`}
                branchControl={
                  project.repository_provider === "github" ? (
                    <ProjectBaseBranchPicker
                      projectID={project.id}
                      value={selectedBaseRef}
                      onChange={selectBaseRef}
                    />
                  ) : (
                    <ProjectBranchBadge branch="main" baseRef="main" />
                  )
                }
              />
            </div>
          </section>
        )}

        {editing ? (
          <section className="mt-8 rounded-2xl border border-border bg-card p-5">
            <div className="flex items-center justify-between">
              <h2 className="font-semibold">Project settings</h2>
              <button
                type="button"
                onClick={() => setEditing(false)}
                className="text-sm text-muted-foreground transition-colors hover:text-foreground"
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
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition-colors focus:border-primary"
                />
              </label>
              {runtimePickerEnabled ? (
                <label className="space-y-1.5">
                  <span className="text-sm font-medium">Default runtime</span>
                  <select
                    value={draftRuntime}
                    onChange={(event) => setDraftRuntime(event.target.value)}
                    className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition-colors focus:border-primary"
                  >
                    {runtimes.map((runtime) => (
                      <option key={runtime.id} value={runtime.id}>
                        {runtime.label}
                      </option>
                    ))}
                  </select>
                </label>
              ) : null}
              <label className="space-y-1.5 sm:col-span-2">
                <span className="text-sm font-medium">Description</span>
                <input
                  value={draftDescription}
                  onChange={(event) => setDraftDescription(event.target.value)}
                  className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition-colors focus:border-primary"
                />
              </label>
            </div>
            <div className="mt-5 flex items-center justify-between">
              <button
                type="button"
                disabled={busy}
                onClick={() => void archive()}
                className="inline-flex items-center gap-2 text-sm font-medium text-red-600 transition-opacity hover:opacity-80"
              >
                <Archive className="size-4" /> Archive
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() => void saveSettings()}
                className="rounded-full px-5 py-2 text-sm font-semibold text-white shadow-lg shadow-primary/20 transition-transform hover:-translate-y-0.5 disabled:opacity-50 disabled:hover:translate-y-0"
                style={{ background: BRAND_GRADIENT }}
              >
                Save
              </button>
            </div>
          </section>
        ) : null}

        <section className="mt-12">
          <div className="flex items-end justify-between border-b border-border pb-3">
            <div>
              <p className="font-mono text-[11px] uppercase tracking-[0.22em] text-muted-foreground">
                {project.repository_provider === "local" ? "Workspace" : "Tasks"}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                {tasks.length} {tasks.length === 1 ? "task" : "tasks"}
              </p>
            </div>
          </div>
          {tasks.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-border px-6 py-12 text-center">
              <p className="text-sm text-muted-foreground">No project tasks yet.</p>
            </div>
          ) : (
            <div className="border-t border-border">
              {tasks.map((task) => (
                <button
                  type="button"
                  key={task.id}
                  onClick={() =>
                    router.push(
                      `/projects/${encodeURIComponent(project.id)}/tasks/${encodeURIComponent(task.id)}`,
                    )
                  }
                  className="group flex w-full items-center gap-3 border-b border-border py-4 pl-0 text-left transition-[padding,background] hover:bg-muted hover:pl-3"
                >
                  <div className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-semibold group-hover:text-primary">
                      {task.title || "Untitled task"}
                    </span>
                    <span className="mt-1 inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                      <GitBranch className="size-3.5" />
                      {task.workspace.branch_name}
                    </span>
                  </div>
                  {task.workspace.git_snapshot?.dirty ? (
                    <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/10 px-2.5 py-1 text-[11px] font-semibold text-amber-700">
                      <span className="size-[6px] rounded-full bg-amber-500" />
                      Modified
                    </span>
                  ) : null}
                  <ChevronRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                </button>
              ))}
            </div>
          )}
        </section>
        {error ? (
          <p className="mt-6 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-600">{error}</p>
        ) : null}
      </main>
    </div>
  );
}
