"use client";

import { Button, Card, Chip, Input, Label, TextArea, TextField } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
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
} from "lucide-react";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useCocola, type ProjectSummary } from "@/app/runtime-provider";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import {
  ProjectBaseBranchPicker,
  ProjectBranchBadge,
  ProjectTaskBranchField,
  projectTaskBranchError,
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
  change_request?: { status: string };
};

const STATUS_LABEL: Record<ProjectSummary["status"], string> = {
  ready: "Ready",
  provisioning: "Provisioning",
  failed: "Failed",
  archiving: "Archiving",
  archive_failed: "Archive failed",
  archived: "Archived",
};

const CHANGE_REQUEST_LABEL: Record<string, string> = {
  open: "In review",
  checks_pending: "Checks pending",
  conflict: "Conflict",
  merged: "Merged",
  closed: "Closed",
  failed: "Failed",
};

export default function ProjectPage() {
  const { id: projectID } = useParams<{ id: string }>();
  const router = useRouter();
  const {
    projects,
    projectsLoaded,
    refreshProjects,
    newProjectTask,
    updatePendingProjectTaskBaseRef,
    updatePendingProjectTaskBranch,
    discardPendingProjectTask,
    activeSessionId,
    serverAcceptedSessionIds,
  } = useCocola();
  const [project, setProject] = useState<ProjectSummary | null>(
    projects.find((item) => item.id === projectID) ?? null,
  );
  const [tasks, setTasks] = useState<ProjectTask[]>([]);
  const [tasksLoaded, setTasksLoaded] = useState(false);
  const [composerReady, setComposerReady] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftDescription, setDraftDescription] = useState("");
  const [selectedBaseRef, setSelectedBaseRef] = useState("");
  const [taskBranchName, setTaskBranchName] = useState("");
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
    const controller = new AbortController();
    setTasks([]);
    setTasksLoaded(false);
    setComposerReady(false);
    setError("");
    void Promise.all([
      fetch(`/api/projects/${encodeURIComponent(projectID)}`, {
        cache: "no-store",
        signal: controller.signal,
      }).then(async (response) => {
        if (!response.ok) throw new Error("Project not found");
        return (await response.json()) as ProjectSummary;
      }),
      fetch(`/api/projects/${encodeURIComponent(projectID)}/tasks`, {
        cache: "no-store",
        signal: controller.signal,
      }).then(async (response) => {
        if (!response.ok) throw new Error("Could not load project tasks");
        return (await response.json()) as ProjectTask[];
      }),
    ])
      .then(([loadedProject, loadedTasks]) => {
        if (controller.signal.aborted) return;
        setProject(loadedProject);
        setTasks(loadedTasks);
        setTasksLoaded(true);
      })
      .catch((cause) => {
        if (!controller.signal.aborted)
          setError(cause instanceof Error ? cause.message : "Could not load project");
      });
    return () => controller.abort();
  }, [projectID]);

  useEffect(() => {
    if (
      !project ||
      !tasksLoaded ||
      project.status !== "ready" ||
      !selectedBaseRef ||
      preparedProject.current === project.id
    )
      return;
    preparedProject.current = project.id;
    const pendingTask = newProjectTask(project.id, project.runtime_id, selectedBaseRef);
    preparedSession.current = pendingTask.sessionId;
    setTaskBranchName(pendingTask.branchName);
    setComposerReady(true);
  }, [newProjectTask, project, selectedBaseRef, tasks.length, tasksLoaded]);

  useEffect(
    () => () => {
      if (preparedSession.current) discardPendingProjectTask(preparedSession.current);
      preparedProject.current = null;
      preparedSession.current = null;
    },
    [discardPendingProjectTask, projectID],
  );

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

  const selectBaseRef = (branch: string) => {
    if (
      preparedSession.current &&
      !updatePendingProjectTaskBaseRef(preparedSession.current, branch)
    )
      return;
    setSelectedBaseRef(branch);
  };

  const selectTaskBranch = (branch: string) => {
    if (preparedSession.current && !updatePendingProjectTaskBranch(preparedSession.current, branch))
      return;
    setTaskBranchName(branch);
  };

  const retry = async () => {
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(projectID)}/retry`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      });
      if (!response.ok) throw new Error("Could not reconcile the GitHub repository");
      setProject((await response.json()) as ProjectSummary);
      refreshProjects();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Retry failed");
    } finally {
      setBusy(false);
    }
  };

  const startEditing = () => {
    if (!project) return;
    setDraftName(project.name);
    setDraftDescription(project.description);
    setEditing(true);
  };

  const saveSettings = async () => {
    if (!project || !draftName.trim()) return;
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(project.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          expected_version: project.version,
          name: draftName.trim(),
          description: draftDescription.trim(),
        }),
      });
      if (!response.ok) throw new Error("Could not save Project settings");
      setProject((await response.json()) as ProjectSummary);
      setEditing(false);
      refreshProjects();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not save Project");
    } finally {
      setBusy(false);
    }
  };

  const archive = async () => {
    if (!project) return;
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/projects/${encodeURIComponent(project.id)}`, {
        method: "DELETE",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ expected_version: project.version }),
      });
      const result = (await response.json().catch(() => null)) as ProjectSummary | null;
      if (!response.ok || !result) throw new Error("Could not archive Project");
      setProject(result);
      refreshProjects();
      if (result.status === "archived") {
        router.push("/projects");
        return;
      }
      throw new Error(
        result.archive_error_code
          ? `Could not archive Project (${result.archive_error_code})`
          : "Could not archive Project",
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not archive Project");
      setBusy(false);
    }
  };

  if (!project && !projectsLoaded && !error)
    return (
      <div className="cocola-web-page grid min-h-64 place-items-center p-8">
        <Loader2 className="text-muted size-5 animate-spin" />
      </div>
    );
  if (!project)
    return (
      <div className="cocola-web-page mx-auto grid min-h-72 max-w-5xl place-items-center p-8 text-center">
        <div>
          <FolderGit2 className="text-muted mx-auto size-9" />
          <h1 className="mt-3 text-lg font-semibold">Project not found</h1>
          <p className="text-muted mt-1 text-sm">
            {error || "It may have been archived or belongs to another account."}
          </p>
          <Button className="mt-4" onPress={() => router.push("/projects")}>
            Back to Projects
          </Button>
        </div>
      </div>
    );

  const isGithub = project.repository_provider === "github" || Boolean(project.repository_html_url);
  const statusColor =
    project.status === "ready"
      ? "success"
      : project.status === "failed" || project.status === "archive_failed"
        ? "danger"
        : project.status === "archived"
          ? "default"
          : "warning";
  const repositoryLabel = isGithub
    ? `${project.repository_owner}/${project.repository_name}`
    : "Cocola repository";

  return (
    <div className="cocola-web-page mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-center gap-3">
        <Button
          isIconOnly
          aria-label="Back to Projects"
          variant="ghost"
          onPress={() => router.push("/projects")}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span
          className={`${isGithub ? "bg-zinc-950 text-white dark:bg-white dark:text-zinc-950" : "bg-amber-500/15 text-amber-600 dark:text-amber-300"} flex size-11 items-center justify-center rounded-2xl`}
        >
          {isGithub ? <GitFork className="size-5" /> : <FolderGit2 className="size-5" />}
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-2xl font-semibold tracking-[-0.03em]">{project.name}</h1>
          <p className="text-muted mt-1 text-sm">{project.description || "No description"}</p>
        </div>
        {project.status === "ready" ||
        project.status === "failed" ||
        project.status === "archive_failed" ? (
          <div className="flex shrink-0 gap-2">
            <Button
              isIconOnly
              aria-label="Project settings"
              size="sm"
              variant="outline"
              onPress={startEditing}
            >
              <Pencil className="size-4" />
            </Button>
          </div>
        ) : null}
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Chip color={statusColor} size="sm" variant="soft">
          {STATUS_LABEL[project.status]}
        </Chip>
        <Chip size="sm" variant="soft">
          {project.repository_provider === "github" ? "GitHub" : "Cocola SCM"}
        </Chip>
        {isGithub ? (
          <Chip size="sm" variant="soft">
            {project.visibility}
          </Chip>
        ) : null}
        <Chip size="sm" variant="soft">
          <GitBranch className="size-3" />
          {project.default_branch || "Preparing"}
        </Chip>
        {project.repository_html_url ? (
          <Button
            className="ml-auto"
            size="sm"
            variant="ghost"
            onPress={() =>
              window.open(project.repository_html_url, "_blank", "noopener,noreferrer")
            }
          >
            {repositoryLabel}
            <ExternalLink className="size-3.5" />
          </Button>
        ) : null}
      </div>

      {project.repository_has_lfs || project.repository_has_submodules ? (
        <div className="bg-warning/10 text-warning flex items-start gap-2 rounded-2xl px-4 py-3 text-sm">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          {project.repository_has_lfs && project.repository_has_submodules
            ? "Git LFS objects and submodules are not downloaded in phase one."
            : project.repository_has_lfs
              ? "Git LFS objects are kept as pointer files in phase one."
              : "Git submodules are not initialized in phase one."}
        </div>
      ) : null}

      {project.status === "archived" ? (
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>Project archived</Card.Title>
            <Card.Description>
              New tasks are disabled. Existing tasks and saved Git snapshots remain available.
            </Card.Description>
          </Card.Header>
        </Card>
      ) : project.status === "failed" ? (
        <Card className="border-warning/30 bg-warning/10 p-5">
          <Card.Content className="flex-row items-center justify-between gap-4 p-0">
            <span>
              <span className="font-medium">Project {project.status}</span>
              <span className="text-muted mt-1 block text-sm">
                {project.provision_error_code || "Repository provisioning has not completed."}
              </span>
            </span>
            <Button isPending={busy} size="sm" variant="outline" onPress={() => void retry()}>
              <RefreshCw className="size-4" />
              Retry reconciliation
            </Button>
          </Card.Content>
        </Card>
      ) : project.status === "archiving" || project.status === "archive_failed" ? (
        <Card
          className={
            project.status === "archive_failed"
              ? "border-danger/30 bg-danger/10 p-5"
              : "border-warning/30 bg-warning/10 p-5"
          }
        >
          <Card.Content className="flex-row items-center justify-between gap-4 p-0">
            <span>
              <span className="font-medium">{STATUS_LABEL[project.status]}</span>
              <span className="text-muted mt-1 block text-sm">
                {project.archive_error_code ||
                  (project.status === "archiving"
                    ? "Repository access is being revoked."
                    : "The repository could not be archived safely.")}
              </span>
            </span>
            {project.status === "archive_failed" ? (
              <Button
                isPending={busy}
                size="sm"
                variant="outline"
                onPress={() => setArchiveOpen(true)}
              >
                <RefreshCw className="size-4" />
                Retry archive
              </Button>
            ) : null}
          </Card.Content>
        </Card>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>Repository</Card.Title>
            <Card.Description>{repositoryLabel}</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-3 p-0">
            <Info
              label="Provider"
              value={project.repository_provider === "github" ? "GitHub" : "Cocola SCM"}
            />
            <Info label="Default branch" value={project.default_branch || "Preparing"} />
            <Info label="Visibility" value={isGithub ? project.visibility : "Private"} />
          </Card.Content>
        </Card>
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>Project activity</Card.Title>
            <Card.Description>Current Project state and task history.</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-3 p-0">
            <Info label="Status" value={STATUS_LABEL[project.status]} />
            <Info label="Tasks" value={String(tasks.length)} />
          </Card.Content>
        </Card>
      </div>

      {project.status === "ready" ? (
        !tasksLoaded || !composerReady ? (
          <Card className="p-5">
            <Card.Header className="p-0">
              <Card.Title>Preparing Project workspace</Card.Title>
              <Card.Description>
                Loading tasks and preparing a conversation workspace…
              </Card.Description>
            </Card.Header>
          </Card>
        ) : (
          <Card className="p-5">
            <Card.Header className="p-0">
              <Card.Title>Start project work</Card.Title>
              <Card.Description>
                Choose the starting point and name this task branch. Both are locked when you send
                the first message.
              </Card.Description>
            </Card.Header>
            <Card.Content className="mt-4 gap-4 p-0">
              <ProjectTaskBranchField value={taskBranchName} onChange={selectTaskBranch} />
              <ConversationComposer
                disabled={Boolean(projectTaskBranchError(taskBranchName))}
                disabledReason="Choose a valid task branch before sending."
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
            </Card.Content>
          </Card>
        )
      ) : null}

      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-semibold">Project tasks</h2>
          <p className="text-muted mt-1 text-sm">
            Project tasks also appear in Chats for quick access.
          </p>
        </div>
        <Chip size="sm" variant="soft">
          {tasks.length}
        </Chip>
      </div>
      {tasks.length ? (
        <section className="cocola-web-catalog-grid grid items-stretch gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {tasks.map((task) => (
            <button
              key={task.id}
              className="cocola-web-catalog-trigger group rounded-2xl text-left outline-none focus-visible:ring-2 focus-visible:ring-focus"
              type="button"
              onClick={() =>
                router.push(
                  `/projects/${encodeURIComponent(project.id)}/tasks/${encodeURIComponent(task.id)}`,
                )
              }
            >
              <Card className="cocola-web-catalog-card h-full min-h-44 p-5">
                <Card.Content className="flex h-full min-w-0 flex-col items-start p-0">
                  <span className="flex w-full items-start justify-between gap-3">
                    <span className="bg-indigo-500/15 text-indigo-600 grid size-10 place-items-center rounded-2xl dark:text-indigo-300">
                      <GitFork className="size-5" />
                    </span>
                    {task.change_request ? (
                      <Chip
                        color={
                          task.change_request.status === "merged"
                            ? "success"
                            : task.change_request.status === "conflict" ||
                                task.change_request.status === "failed"
                              ? "danger"
                              : "accent"
                        }
                        size="sm"
                        variant="soft"
                      >
                        {CHANGE_REQUEST_LABEL[task.change_request.status] || "Working"}
                      </Chip>
                    ) : task.workspace.git_snapshot?.dirty ? (
                      <Chip color="warning" size="sm" variant="soft">
                        Modified
                      </Chip>
                    ) : null}
                  </span>
                  <span className="mt-4 block max-w-full truncate font-semibold">
                    {task.title || "Untitled task"}
                  </span>
                  <span className="text-muted mt-2 flex items-center gap-1.5 text-sm">
                    <GitBranch className="size-3.5" />
                    {task.workspace.branch_name}
                  </span>
                  <span className="text-accent mt-auto flex w-full items-center justify-end gap-1 pt-5 text-sm font-medium">
                    Open
                    <ArrowRight className="cocola-web-catalog-card-arrow size-4" />
                  </span>
                </Card.Content>
              </Card>
            </button>
          ))}
        </section>
      ) : (
        <Card className="border-separator min-h-32 border border-dashed p-6">
          <Card.Content className="text-muted flex items-center justify-center p-0 text-sm">
            No Project tasks yet.
          </Card.Content>
        </Card>
      )}

      <Sheet
        isOpen={editing}
        placement="right"
        onOpenChange={(open) => {
          if (!busy) setEditing(open);
        }}
      >
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[460px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close Project settings" />
              <Sheet.Header>
                <Sheet.Heading>Project settings</Sheet.Heading>
                <p className="text-muted text-sm">Update this Project’s name and description.</p>
              </Sheet.Header>
              <Sheet.Body className="grid content-start gap-4">
                <TextField value={draftName} variant="secondary" onChange={setDraftName}>
                  <Label>Name</Label>
                  <Input />
                </TextField>
                <TextField
                  value={draftDescription}
                  variant="secondary"
                  onChange={setDraftDescription}
                >
                  <Label>Description</Label>
                  <TextArea rows={5} />
                </TextField>
                {error ? (
                  <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                    {error}
                  </div>
                ) : null}
              </Sheet.Body>
              <Sheet.Footer className="flex-col gap-2">
                <Button
                  className="w-full"
                  isDisabled={!draftName.trim()}
                  isPending={busy}
                  onPress={() => void saveSettings()}
                >
                  Save changes
                </Button>
                <Button
                  className="w-full"
                  variant="danger-soft"
                  onPress={() => setArchiveOpen(true)}
                >
                  <Archive className="size-4" />
                  Archive Project
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>

      <ActionConfirmDialog
        busy={busy}
        confirmLabel="Archive Project"
        description={
          project.repository_provider === "github"
            ? "The GitHub repository will not be deleted."
            : "The Cocola repository becomes read-only. Existing task history remains available."
        }
        error={error || null}
        icon={Archive}
        open={archiveOpen}
        title="Archive this Project?"
        tone="danger"
        onConfirm={() => void archive()}
        onOpenChange={setArchiveOpen}
      />
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface-secondary min-w-0 rounded-2xl px-4 py-3">
      <span className="text-muted block text-xs">{label}</span>
      <span className="mt-1 block truncate text-sm font-medium">{value}</span>
    </div>
  );
}
