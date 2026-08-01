"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import * as Dialog from "@radix-ui/react-dialog";
import {
  AlertTriangle,
  Archive,
  ArrowLeft,
  ArrowRight,
  ExternalLink,
  FolderGit2,
  GitBranch,
  GitFork,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  X,
} from "lucide-react";
import { useCocola, type ProjectSummary } from "@/app/runtime-provider";
import {
  ProjectBaseBranchPicker,
  ProjectBranchBadge,
} from "@/components/assistant-ui/project-branch-control";
import { ConversationComposer } from "@/components/assistant-ui/thread";
import { SelectControl } from "@/components/ui/select-control";
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
    <main className="user-canvas user-page user-theme-indigo h-full min-w-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6 sm:py-7">
      <div className="mx-auto w-full max-w-7xl pb-16">
        {/* Back */}
        <button
          type="button"
          onClick={() => router.push("/projects")}
          className="group inline-flex items-center gap-1.5 rounded-full border border-black/[0.06] bg-white px-4 py-2 text-[13px] font-medium text-muted-foreground shadow-[0_1px_2px_0_rgb(15_23_42/0.06),0_6px_16px_-10px_rgb(15_23_42/0.25)] transition-all duration-200 hover:-translate-y-0.5 hover:text-foreground hover:shadow-[0_2px_4px_0_rgb(15_23_42/0.08),0_12px_24px_-12px_rgb(15_23_42/0.35)] active:translate-y-0 active:shadow-[0_1px_2px_0_rgb(15_23_42/0.06)]"
        >
          <ArrowLeft className="size-4 transition-transform duration-200 group-hover:-translate-x-0.5" />
          Back
        </button>

        {/* Header */}
        <header className="mt-4 flex flex-wrap items-start gap-4">
          <div className="proj-mono group size-[58px] text-2xl">{initials(project.name)}</div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2.5">
              <h1 className="truncate text-[26px] font-semibold tracking-tight">{project.name}</h1>
              {isGithub ? (
                <span className="user-tag user-tag--muted shrink-0 font-mono text-[10px] uppercase">
                  {project.visibility}
                </span>
              ) : null}
            </div>
            <p className={"user-card-desc mt-1.5" + (project.description ? "" : " opacity-50")}>
              {project.description || "No description"}
            </p>
            {/* meta row */}
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[12.5px] text-muted-foreground">
              {isGithub ? (
                <a
                  href={project.repository_html_url}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1.5 transition-colors hover:text-[color:var(--page-accent)]"
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
          {project.status !== "archived" ? (
            <div className="flex shrink-0 items-center gap-2">
              {project.repository_provider === "local" &&
              project.github_publish_status !== "published" &&
              project.primary_conversation_id ? (
                <button
                  type="button"
                  onClick={startPublishing}
                  className="user-tbtn user-tbtn--ghost px-3.5"
                >
                  <GitFork className="size-4" />
                  {project.github_publish_status === "pending" ? "Retry publish" : "Publish"}
                </button>
              ) : null}
              <button
                type="button"
                onClick={startEditing}
                aria-label="Edit project"
                className="user-iconbtn"
              >
                <Pencil className="size-4" />
              </button>
            </div>
          ) : null}
        </header>

        {showPublish ? (
          <section className="user-panel mt-6">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="user-section-title">Publish to GitHub</h2>
                <p className="user-card-desc mt-1">
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
                  className="user-search-input user-field-input h-10 w-full rounded-xl px-3 text-sm disabled:opacity-60"
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">Visibility</span>
                <SelectControl
                  value={publishVisibility}
                  disabled={project.github_publish_status === "pending"}
                  onValueChange={(value) =>
                    setPublishVisibility(value === "public" ? "public" : "private")
                  }
                  options={[
                    { value: "private", label: "Private" },
                    { value: "public", label: "Public" },
                  ]}
                  className="focus-visible:border-primary disabled:opacity-60"
                  contentClassName="cocola-user-ui"
                />
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
                className="user-accent-btn inline-flex h-9 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:opacity-50"
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
          <section className="user-panel mt-6">
            <h2 className="user-section-title">Project archived</h2>
            <p className="user-card-desc mt-1">
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
              className="user-tbtn user-tbtn--ghost mt-4 px-4"
            >
              <RefreshCw className="size-4" /> Retry reconciliation
            </button>
          </section>
        ) : project.repository_provider === "local" && tasks.length > 0 ? (
          <section className="mt-8 flex items-center gap-3 rounded-2xl border border-amber-500/25 bg-amber-500/5 px-5 py-4 text-sm">
            <AlertTriangle className="size-4 shrink-0 text-amber-500" />
            <p className="text-muted-foreground">
              Non-GitHub projects support only a single workspace.
            </p>
          </section>
        ) : !tasksLoaded || !composerReady ? (
          <section className="user-panel mt-8">
            <h2 className="user-section-title">Preparing Project workspace</h2>
            <p className="user-card-desc mt-1">
              Loading Project tasks and preparing a conversation workspace…
            </p>
          </section>
        ) : (
          <section className="user-panel mt-8">
            <div className="flex items-center gap-3">
              <div className="user-panel-glyph">
                <Plus className="size-[18px]" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="user-section-title">
                  {project.repository_provider === "local" ? "Start new workspace" : "New task"}
                </div>
                {project.repository_provider === "local" ? null : (
                  <p className="user-card-desc mt-0.5">
                    Choose a base branch. Cocola locks its current revision when you send the first
                    message.
                  </p>
                )}
              </div>
            </div>
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

        <Dialog.Root open={editing} onOpenChange={(next) => !busy && setEditing(next)}>
          <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/20 backdrop-blur-sm data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out data-[state=open]:fade-in" />
            <Dialog.Content className="cocola-user-ui user-theme-indigo fixed inset-y-2 right-2 z-50 flex w-[min(30rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-2xl border border-border bg-background text-foreground shadow-2xl outline-none data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right">
              <header className="flex min-h-16 items-center gap-3 border-b border-border/70 px-5">
                <span className="user-panel-glyph">
                  <Pencil className="size-[18px]" />
                </span>
                <div className="min-w-0 flex-1">
                  <Dialog.Title className="truncate text-base font-semibold">
                    Project settings
                  </Dialog.Title>
                  <Dialog.Description className="truncate text-xs text-muted-foreground">
                    Update this Project’s name, runtime, and description.
                  </Dialog.Description>
                </div>
                <Dialog.Close
                  aria-label="Close"
                  className="grid size-9 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                >
                  <X className="size-4" />
                </Dialog.Close>
              </header>
              <div className="min-h-0 flex-1 overflow-y-auto p-5">
                <div className="grid gap-4">
                  <label className="space-y-1.5">
                    <span className="text-sm font-medium">Name</span>
                    <input
                      value={draftName}
                      onChange={(event) => setDraftName(event.target.value)}
                      className="user-search-input user-field-input h-10 w-full rounded-xl px-3 text-sm"
                    />
                  </label>
                  {runtimePickerEnabled ? (
                    <label className="space-y-1.5">
                      <span className="text-sm font-medium">Default runtime</span>
                      <SelectControl
                        value={draftRuntime}
                        onValueChange={setDraftRuntime}
                        options={runtimes.map((runtime) => ({
                          value: runtime.id,
                          label: runtime.label,
                        }))}
                        className="focus-visible:border-primary"
                        contentClassName="cocola-user-ui"
                      />
                    </label>
                  ) : null}
                  <label className="space-y-1.5">
                    <span className="text-sm font-medium">Description</span>
                    <input
                      value={draftDescription}
                      onChange={(event) => setDraftDescription(event.target.value)}
                      className="user-search-input user-field-input h-10 w-full rounded-xl px-3 text-sm"
                    />
                  </label>
                </div>
              </div>
              <div className="flex items-center justify-between border-t border-border/70 p-4">
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
                  className="user-accent-btn inline-flex h-9 items-center rounded-xl px-5 text-sm font-semibold disabled:opacity-50"
                >
                  Save
                </button>
              </div>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>

        {/* Tasks */}
        <section className="mt-12">
          <div className="mb-3 flex items-center gap-2">
            <span className="user-section-title">
              {project.repository_provider === "local" ? "Workspace" : "Tasks"}
            </span>
            <span className="user-count-badge">{tasks.length}</span>
          </div>
          {tasks.length === 0 ? (
            <div className="user-empty">
              <p className="text-sm text-muted-foreground">No project tasks yet.</p>
            </div>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {tasks.map((task) => (
                <button
                  type="button"
                  key={task.id}
                  onClick={() =>
                    router.push(
                      `/projects/${encodeURIComponent(project.id)}/tasks/${encodeURIComponent(task.id)}`,
                    )
                  }
                  className="task-card w-full text-left"
                >
                  <div className="task-card-head">
                    <span className="task-card-icon">
                      <GitFork className="size-[18px]" />
                    </span>
                    <span className="task-card-title truncate">
                      {task.title || "Untitled task"}
                    </span>
                    {task.workspace.git_snapshot?.dirty ? (
                      <span className="user-tag user-tag--warn shrink-0">
                        <span className="user-tag-dot" />
                        Modified
                      </span>
                    ) : null}
                  </div>
                  <span className="task-card-summary inline-flex items-center gap-1.5">
                    <GitBranch className="size-3.5 shrink-0" />
                    {task.workspace.branch_name}
                  </span>
                  <div className="mt-auto flex w-full justify-end">
                    <span className="task-card-cta">
                      Open
                      <ArrowRight className="size-3.5" />
                    </span>
                  </div>
                </button>
              ))}
            </div>
          )}
        </section>
        {error ? (
          <p className="mt-6 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-600">{error}</p>
        ) : null}
      </div>
    </main>
  );
}
