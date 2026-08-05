"use client";

import { Button, Card, Chip, Dropdown, Input, Label, TextArea, TextField } from "@heroui/react";
import { Segment } from "@heroui-pro/react/segment";
import { Sheet } from "@heroui-pro/react/sheet";
import {
  AlertTriangle,
  Archive,
  ArrowLeft,
  ArrowRight,
  ChevronDown,
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

const STATUS_LABEL: Record<ProjectSummary["status"], string> = {
  ready: "Ready",
  provisioning: "Provisioning",
  failed: "Failed",
  archived: "Archived",
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
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
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
    setDraftRuntime(project.runtime_id);
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
          runtime_id: draftRuntime || project.runtime_id,
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
    setError("");
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
              ? "Grant the GitHub App access to the repository, then retry."
              : body.error?.message || "Could not publish this Project";
        throw new Error(message);
      }
      setProject(body);
      setShowPublish(false);
      refreshProjects();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not publish this Project");
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
      if (!response.ok) throw new Error("Could not archive Project");
      refreshProjects();
      router.push("/projects");
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
    project.status === "ready" ? "success" : project.status === "failed" ? "danger" : "warning";
  const repositoryLabel = isGithub
    ? `${project.repository_owner}/${project.repository_name}`
    : "Local workspace";

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
        {project.status !== "archived" ? (
          <div className="flex shrink-0 gap-2">
            {project.repository_provider === "local" &&
            project.github_publish_status !== "published" &&
            project.primary_conversation_id ? (
              <Button size="sm" variant="outline" onPress={startPublishing}>
                <GitFork className="size-4" />
                {project.github_publish_status === "pending" ? "Retry publish" : "Publish"}
              </Button>
            ) : null}
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
          {project.repository_provider === "github" ? "GitHub" : "Local"}
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

      {showPublish ? (
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>Publish to GitHub</Card.Title>
            <Card.Description>
              Cocola will push the committed main branch using a short-lived repository token.
            </Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-4 p-0 sm:grid-cols-2">
            <TextField
              value={publishRepository}
              variant="secondary"
              onChange={setPublishRepository}
            >
              <Label>Repository name</Label>
              <Input disabled={project.github_publish_status === "pending"} />
            </TextField>
            <div>
              <Label>Visibility</Label>
              <Segment
                aria-label="Repository visibility"
                className="mt-2"
                selectedKey={publishVisibility}
                onSelectionChange={(key) =>
                  setPublishVisibility(String(key) === "public" ? "public" : "private")
                }
              >
                <Segment.Item id="private">Private</Segment.Item>
                <Segment.Item id="public">Public</Segment.Item>
              </Segment>
            </div>
          </Card.Content>
          <Card.Footer className="mt-5 justify-end gap-2 p-0">
            <Button variant="outline" onPress={() => setShowPublish(false)}>
              Cancel
            </Button>
            <Button
              isDisabled={!publishRepository.trim()}
              isPending={busy}
              onPress={() => void publish()}
            >
              <GitFork className="size-4" />
              Publish
            </Button>
          </Card.Footer>
        </Card>
      ) : null}

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
      ) : project.status !== "ready" ? (
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
              value={project.repository_provider === "github" ? "GitHub" : "Local"}
            />
            <Info label="Default branch" value={project.default_branch || "Preparing"} />
            <Info label="Visibility" value={isGithub ? project.visibility : "Local only"} />
          </Card.Content>
        </Card>
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>Workspace</Card.Title>
            <Card.Description>Runtime and repository state for new Project tasks.</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-3 p-0">
            <Info
              label="Runtime"
              value={
                runtimes.find((runtime) => runtime.id === project.runtime_id)?.label ||
                project.runtime_id
              }
            />
            <Info label="Status" value={STATUS_LABEL[project.status]} />
            <Info label="Tasks" value={String(tasks.length)} />
          </Card.Content>
        </Card>
      </div>

      {project.status === "ready" ? (
        project.repository_provider === "local" && tasks.length > 0 ? (
          <div className="bg-warning/10 text-warning flex items-start gap-2 rounded-2xl px-4 py-3 text-sm">
            <AlertTriangle className="mt-0.5 size-4" />
            Non-GitHub Projects support only a single workspace.
          </div>
        ) : !tasksLoaded || !composerReady ? (
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
              <Card.Title>
                {project.repository_provider === "local"
                  ? "Start new workspace"
                  : "Start project work"}
              </Card.Title>
              <Card.Description>
                {project.repository_provider === "local"
                  ? "Create the first conversation in this local workspace."
                  : "Choose a base branch. Cocola locks its current revision when you send the first message."}
              </Card.Description>
            </Card.Header>
            <Card.Content className="mt-4 p-0">
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
            </Card.Content>
          </Card>
        )
      ) : null}

      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-semibold">
            {project.repository_provider === "local" ? "Workspace" : "Project tasks"}
          </h2>
          <p className="text-muted mt-1 text-sm">
            Project conversations stay separate from the global Chats list.
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
                    {task.workspace.git_snapshot?.dirty ? (
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
                <p className="text-muted text-sm">
                  Update this Project’s name, runtime, and description.
                </p>
              </Sheet.Header>
              <Sheet.Body className="grid content-start gap-4">
                <TextField value={draftName} variant="secondary" onChange={setDraftName}>
                  <Label>Name</Label>
                  <Input />
                </TextField>
                {runtimePickerEnabled ? (
                  <div>
                    <Label>Default runtime</Label>
                    <Dropdown>
                      <Dropdown.Trigger
                        aria-label="Select runtime"
                        className="border-separator bg-default hover:bg-default-hover mt-2 flex h-11 w-full items-center justify-between rounded-2xl border px-3 text-sm"
                      >
                        <span className="truncate">
                          {runtimes.find((runtime) => runtime.id === draftRuntime)?.label ||
                            draftRuntime}
                        </span>
                        <ChevronDown className="text-muted size-4" />
                      </Dropdown.Trigger>
                      <Dropdown.Popover placement="bottom start">
                        <Dropdown.Menu
                          aria-label="Project runtimes"
                          onAction={(key) => setDraftRuntime(String(key))}
                        >
                          {runtimes.map((runtime) => (
                            <Dropdown.Item
                              key={runtime.id}
                              id={runtime.id}
                              textValue={runtime.label}
                            >
                              {runtime.label}
                            </Dropdown.Item>
                          ))}
                        </Dropdown.Menu>
                      </Dropdown.Popover>
                    </Dropdown>
                  </div>
                ) : null}
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

      <Sheet isOpen={archiveOpen} placement="right" onOpenChange={setArchiveOpen}>
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[420px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close archive confirmation" />
              <Sheet.Header>
                <Sheet.Heading>Archive this Project?</Sheet.Heading>
                <p className="text-muted text-sm">
                  {project.repository_provider === "github"
                    ? "The GitHub repository will not be deleted."
                    : "Its existing local workspace remains available for review."}
                </p>
              </Sheet.Header>
              <Sheet.Footer className="gap-2">
                <Button variant="outline" onPress={() => setArchiveOpen(false)}>
                  Cancel
                </Button>
                <Button isPending={busy} variant="danger-soft" onPress={() => void archive()}>
                  Archive Project
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
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
