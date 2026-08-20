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
import { useTranslations } from "next-intl";
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

const CHANGE_REQUEST_KEYS = {
  open: "changeRequest.open",
  checks_pending: "changeRequest.checks_pending",
  conflict: "changeRequest.conflict",
  merged: "changeRequest.merged",
  closed: "changeRequest.closed",
  failed: "changeRequest.failed",
} as const;

export default function ProjectPage() {
  const { id: projectID } = useParams<{ id: string }>();
  const router = useRouter();
  const t = useTranslations("projects.detail");
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
    modelsLoaded,
    selectedRuntime,
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
        if (!response.ok) throw new Error(t("errors.notFound"));
        return (await response.json()) as ProjectSummary;
      }),
      fetch(`/api/projects/${encodeURIComponent(projectID)}/tasks`, {
        cache: "no-store",
        signal: controller.signal,
      }).then(async (response) => {
        if (!response.ok) throw new Error(t("errors.tasks"));
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
          setError(cause instanceof Error ? cause.message : t("errors.load"));
      });
    return () => controller.abort();
  }, [projectID, t]);

  useEffect(() => {
    if (
      !project ||
      !tasksLoaded ||
      !modelsLoaded ||
      !selectedRuntime ||
      project.status !== "ready" ||
      !selectedBaseRef ||
      preparedProject.current === project.id
    )
      return;
    preparedProject.current = project.id;
    const pendingTask = newProjectTask(project.id, selectedBaseRef);
    preparedSession.current = pendingTask.sessionId;
    setTaskBranchName(pendingTask.branchName);
    setComposerReady(true);
  }, [
    modelsLoaded,
    newProjectTask,
    project,
    selectedBaseRef,
    selectedRuntime,
    tasks.length,
    tasksLoaded,
  ]);

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
      if (!response.ok) throw new Error(t("errors.reconcile"));
      setProject((await response.json()) as ProjectSummary);
      refreshProjects();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("errors.retry"));
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
      if (!response.ok) throw new Error(t("errors.save"));
      setProject((await response.json()) as ProjectSummary);
      setEditing(false);
      refreshProjects();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("errors.save"));
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
      if (!response.ok || !result) throw new Error(t("errors.archive"));
      setProject(result);
      refreshProjects();
      if (result.status === "archived") {
        router.push("/projects");
        return;
      }
      throw new Error(
        result.archive_error_code
          ? t("errors.archiveCode", { code: result.archive_error_code })
          : t("errors.archive"),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("errors.archive"));
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
          <h1 className="mt-3 text-lg font-semibold">{t("notFound.title")}</h1>
          <p className="text-muted mt-1 text-sm">{error || t("notFound.description")}</p>
          <Button className="mt-4" onPress={() => router.push("/projects")}>
            {t("back")}
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
    : t("repository.cocola");

  return (
    <div className="cocola-web-page mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-center gap-3">
        <Button
          isIconOnly
          aria-label={t("back")}
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
          <p className="text-muted mt-1 text-sm">{project.description || t("noDescription")}</p>
        </div>
        {project.status === "ready" ||
        project.status === "failed" ||
        project.status === "archive_failed" ? (
          <div className="flex shrink-0 gap-2">
            <Button
              isIconOnly
              aria-label={t("settings.open")}
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
          {t(`status.${project.status}`)}
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
          {project.default_branch || t("preparing")}
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
            ? t("limitations.both")
            : project.repository_has_lfs
              ? t("limitations.lfs")
              : t("limitations.submodules")}
        </div>
      ) : null}

      {project.status === "archived" ? (
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>{t("archived.title")}</Card.Title>
            <Card.Description>{t("archived.description")}</Card.Description>
          </Card.Header>
        </Card>
      ) : project.status === "failed" ? (
        <Card className="border-warning/30 bg-warning/10 p-5">
          <Card.Content className="flex-row items-center justify-between gap-4 p-0">
            <span>
              <span className="font-medium">{t("failed.title")}</span>
              <span className="text-muted mt-1 block text-sm">
                {project.provision_error_code || t("failed.description")}
              </span>
            </span>
            <Button isPending={busy} size="sm" variant="outline" onPress={() => void retry()}>
              <RefreshCw className="size-4" />
              {t("failed.retry")}
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
              <span className="font-medium">{t(`status.${project.status}`)}</span>
              <span className="text-muted mt-1 block text-sm">
                {project.archive_error_code ||
                  (project.status === "archiving" ? t("archive.archiving") : t("archive.failed"))}
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
                {t("archive.retry")}
              </Button>
            ) : null}
          </Card.Content>
        </Card>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>{t("repository.title")}</Card.Title>
            <Card.Description>{repositoryLabel}</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-3 p-0">
            <Info
              label={t("repository.provider")}
              value={project.repository_provider === "github" ? "GitHub" : "Cocola SCM"}
            />
            <Info
              label={t("repository.defaultBranch")}
              value={project.default_branch || t("preparing")}
            />
            <Info
              label={t("repository.visibility")}
              value={isGithub ? project.visibility : t("repository.private")}
            />
          </Card.Content>
        </Card>
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>{t("activity.title")}</Card.Title>
            <Card.Description>{t("activity.description")}</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-3 p-0">
            <Info label={t("activity.status")} value={t(`status.${project.status}`)} />
            <Info label={t("activity.tasks")} value={String(tasks.length)} />
          </Card.Content>
        </Card>
      </div>

      {project.status === "ready" ? (
        !tasksLoaded || !composerReady ? (
          <Card className="p-5">
            <Card.Header className="p-0">
              <Card.Title>{t("start.preparingTitle")}</Card.Title>
              <Card.Description>{t("start.preparingDescription")}</Card.Description>
            </Card.Header>
          </Card>
        ) : (
          <Card className="p-5">
            <Card.Header className="p-0">
              <Card.Title>{t("start.title")}</Card.Title>
              <Card.Description>{t("start.description")}</Card.Description>
            </Card.Header>
            <Card.Content className="mt-4 gap-4 p-0">
              <ProjectTaskBranchField value={taskBranchName} onChange={selectTaskBranch} />
              <ConversationComposer
                disabled={Boolean(projectTaskBranchError(taskBranchName))}
                disabledReason={t("start.invalidBranch")}
                placeholder={t("start.placeholder", { project: project.name })}
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
          <h2 className="font-semibold">{t("tasks.title")}</h2>
          <p className="text-muted mt-1 text-sm">{t("tasks.description")}</p>
        </div>
        <Chip size="sm" variant="soft">
          {tasks.length}
        </Chip>
      </div>
      {tasks.length ? (
        <section className="cocola-web-catalog-grid cocola-resource-card-grid">
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
                        {task.change_request.status in CHANGE_REQUEST_KEYS
                          ? t(
                              CHANGE_REQUEST_KEYS[
                                task.change_request.status as keyof typeof CHANGE_REQUEST_KEYS
                              ],
                            )
                          : t("changeRequest.working")}
                      </Chip>
                    ) : task.workspace.git_snapshot?.dirty ? (
                      <Chip color="warning" size="sm" variant="soft">
                        {t("tasks.modified")}
                      </Chip>
                    ) : null}
                  </span>
                  <span className="mt-4 block max-w-full truncate font-semibold">
                    {task.title || t("tasks.untitled")}
                  </span>
                  <span className="text-muted mt-2 flex items-center gap-1.5 text-sm">
                    <GitBranch className="size-3.5" />
                    {task.workspace.branch_name}
                  </span>
                  <span className="text-accent mt-auto flex w-full items-center justify-end gap-1 pt-5 text-sm font-medium">
                    {t("tasks.open")}
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
            {t("tasks.empty")}
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
              <Sheet.CloseTrigger aria-label={t("settings.close")} />
              <Sheet.Header>
                <Sheet.Heading>{t("settings.title")}</Sheet.Heading>
                <p className="text-muted text-sm">{t("settings.description")}</p>
              </Sheet.Header>
              <Sheet.Body className="grid content-start gap-4">
                <TextField value={draftName} variant="secondary" onChange={setDraftName}>
                  <Label>{t("settings.name")}</Label>
                  <Input />
                </TextField>
                <TextField
                  value={draftDescription}
                  variant="secondary"
                  onChange={setDraftDescription}
                >
                  <Label>{t("settings.projectDescription")}</Label>
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
                  {t("settings.save")}
                </Button>
                <Button
                  className="w-full"
                  variant="danger-soft"
                  onPress={() => setArchiveOpen(true)}
                >
                  <Archive className="size-4" />
                  {t("archive.action")}
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>

      <ActionConfirmDialog
        busy={busy}
        confirmLabel={t("archive.action")}
        description={
          project.repository_provider === "github"
            ? t("archive.githubDescription")
            : t("archive.cocolaDescription")
        }
        error={error || null}
        icon={Archive}
        open={archiveOpen}
        title={t("archive.confirmTitle")}
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
