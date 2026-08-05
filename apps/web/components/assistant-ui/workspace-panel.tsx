"use client";

import { useThread } from "@assistant-ui/react";
import { Button, Chip, ScrollShadow, Tooltip } from "@heroui/react";
import { EmptyState } from "@heroui-pro/react/empty-state";
import { ListView } from "@heroui-pro/react/list-view";
import { Segment } from "@heroui-pro/react/segment";
import { Sheet } from "@heroui-pro/react/sheet";
import type { ArtifactPreview } from "@/app/runtime-provider";
import {
  ReadonlyFilePreview,
  formatBytes,
  isHtmlPreview,
  type PreviewFile,
} from "@/components/assistant-ui/file-preview";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { ShellPage } from "@/components/assistant-ui/shell-page";
import {
  type GitChange,
  type GitCommit,
  type GitCommitDetail,
  type GitCommitFile,
  type GitDiff,
  type GitSnapshot,
  useGitWorkspace,
} from "@/components/assistant-ui/use-git-workspace";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { artifactPreviewTabID } from "@/lib/artifact-preview-tab.mjs";
import {
  buildCodeEditorURL,
  classifyCodeEditorProbe,
  codeEditorRetryDelay,
  codeEditorTabID,
  codeEditorWaitExpired,
  normalizeCodeEditorWorkspacePath,
  probeCodeEditorStatus,
} from "@/lib/code-editor-readiness.mjs";
import { resolveFileType } from "@/lib/file-type";
import {
  formatGitRelativeTime,
  gitChangeCode,
  gitCommitBadges,
  gitCommitDescription,
  gitDiffGutterWidth,
} from "@/lib/git-history.mjs";
import { MaterialFileIcon } from "@/lib/material-file-icons";
import { dockPageInstanceID, dockPageInstanceLabel } from "@/lib/workspace-dock-tabs.mjs";
import { Diff as DiffView, Hunk, parseDiff, tokenize } from "react-diff-view";
import { refractor } from "refractor";
import refractorMarkup from "refractor/lang/markup.js";
import refractorCss from "refractor/lang/css.js";
import refractorJavascript from "refractor/lang/javascript.js";
import refractorTypescript from "refractor/lang/typescript.js";
import refractorJsx from "refractor/lang/jsx.js";
import refractorTsx from "refractor/lang/tsx.js";
import refractorGo from "refractor/lang/go.js";
import refractorJson from "refractor/lang/json.js";
import refractorPython from "refractor/lang/python.js";
import refractorBash from "refractor/lang/bash.js";
import refractorYaml from "refractor/lang/yaml.js";
import refractorMarkdown from "refractor/lang/markdown.js";
import {
  AlertTriangle,
  ArrowLeft,
  ChevronRight,
  Code2,
  Download,
  Eye,
  File,
  FileCode2,
  FileQuestion,
  Folder,
  FolderOpen,
  Globe,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  LoaderCircle,
  Plus,
  RefreshCw,
  SquareTerminal,
  ExternalLink,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

// -- Extensible workspace dock ------------------------------------------------
//
// The right-hand dock is a tabbed container: a strip of open sub-pages plus a
// "+" menu to add and switch to another sub-page. Workspace files, Shell, and
// Preview are registered base page templates. Each launcher/menu action creates
// an independent page instance; generated-file previews remain stable per artifact.

type DockPageContext = {
  sessionID: string;
  active: boolean;
  workspaceRoot: string;
  // Lets a page publish header controls (e.g. refresh) into the shared dock
  // header, so no sub-page needs its own toolbar row.
  setHeaderActions: (node: ReactNode) => void;
  openCodeFolder: (workspacePath: string) => void;
};

type DockPage = {
  id: string;
  kind: "files" | "shell" | "preview" | "git" | "code" | "artifact";
  label: string;
  title?: string;
  icon: LucideIcon;
  artifact?: ArtifactPreview;
  unmountWhenInactive?: boolean;
  render: (context: DockPageContext) => ReactNode;
};

const BASE_DOCK_PAGES: DockPage[] = [
  {
    id: "files",
    kind: "files",
    label: "Workspace files",
    icon: FolderOpen,
    render: ({ sessionID, active, setHeaderActions, openCodeFolder, workspaceRoot }) => (
      <WorkspaceFilesPage
        sessionID={sessionID}
        active={active}
        setHeaderActions={setHeaderActions}
        onOpenCode={openCodeFolder}
        workspaceRoot={workspaceRoot}
      />
    ),
  },
  {
    id: "shell",
    kind: "shell",
    label: "Shell",
    icon: SquareTerminal,
    render: ({ sessionID, active, setHeaderActions }) => (
      <ShellPage
        key={sessionID}
        sessionID={sessionID}
        active={active}
        setHeaderActions={setHeaderActions}
      />
    ),
  },
  {
    id: "preview",
    kind: "preview",
    label: "Preview",
    icon: Globe,
    render: ({ sessionID, active, setHeaderActions }) => (
      <PreviewPage sessionID={sessionID} active={active} setHeaderActions={setHeaderActions} />
    ),
  },
];

function createGitPage(): DockPage {
  return {
    id: "git",
    kind: "git",
    label: "Git",
    icon: GitBranch,
    render: ({ sessionID, active, setHeaderActions }) => (
      <GitPage sessionID={sessionID} active={active} setHeaderActions={setHeaderActions} />
    ),
  };
}

function createCodePage(workspacePath: string, workspaceRoot = ""): DockPage {
  const normalizedPath = normalizeCodeEditorWorkspacePath(workspacePath);
  const normalizedRoot = normalizeCodeEditorWorkspacePath(workspaceRoot);
  const folder = normalizedPath ? `/workspace/${normalizedPath}` : "/workspace";
  const label =
    normalizedPath === normalizedRoot
      ? normalizedRoot
        ? "Project"
        : "Workspace"
      : normalizedPath.split("/").pop() || "Workspace";
  return {
    id: codeEditorTabID(normalizedPath),
    kind: "code",
    label,
    title: folder,
    icon: Code2,
    render: ({ sessionID, active, setHeaderActions }) => (
      <CodePage
        sessionID={sessionID}
        workspacePath={normalizedPath}
        active={active}
        setHeaderActions={setHeaderActions}
      />
    ),
  };
}

function createArtifactPage(artifact: ArtifactPreview): DockPage {
  return {
    id: artifactPreviewTabID(artifact.sessionId, artifact.id),
    kind: "artifact",
    label: artifact.filename,
    title: artifact.filename,
    icon: FileCode2,
    artifact,
    unmountWhenInactive: true,
    render: ({ active, setHeaderActions }) => (
      <ArtifactPreviewPage
        artifact={artifact}
        active={active}
        setHeaderActions={setHeaderActions}
      />
    ),
  };
}

export function WorkspaceDock({
  sessionID,
  artifact,
  projectTask = false,
  onArtifactClose,
  onClose,
}: {
  sessionID: string;
  artifact: ArtifactPreview | null;
  projectTask?: boolean;
  onArtifactClose: () => void;
  onClose: () => void;
}) {
  // Opening the workspace dock must not contact code-server. Code tabs only
  // exist after a directory action explicitly creates one.
  const [openPages, setOpenPages] = useState<DockPage[]>([]);
  const [activePageId, setActivePageId] = useState<string>("");
  const pageInstanceOrdinalsRef = useRef<Record<string, number>>({});
  // The active page publishes its header controls here; keyed by page id so a
  // backgrounded page can never leak its actions into the header.
  const [headerActions, setHeaderActions] = useState<Record<string, ReactNode>>({});

  const workspaceRoot = projectTask ? "project" : "";
  const basePages = useMemo(
    () => (projectTask ? [...BASE_DOCK_PAGES, createGitPage()] : BASE_DOCK_PAGES),
    [projectTask],
  );

  const addablePages = basePages;

  const createPageInstance = useCallback((template: DockPage): DockPage => {
    const ordinal = (pageInstanceOrdinalsRef.current[template.id] ?? 0) + 1;
    pageInstanceOrdinalsRef.current[template.id] = ordinal;
    return {
      ...template,
      id: dockPageInstanceID(template.id, ordinal),
      label: dockPageInstanceLabel(template.label, ordinal),
    };
  }, []);

  const openPage = useCallback(
    (id: string) => {
      const page = basePages.find((candidate) => candidate.id === id);
      if (!page) return;
      const instance = createPageInstance(page);
      setOpenPages((current) => [...current, instance]);
      setActivePageId(instance.id);
    },
    [basePages, createPageInstance],
  );

  const openCodeFolder = useCallback(
    (workspacePath: string) => {
      const instance = createPageInstance(createCodePage(workspacePath, workspaceRoot));
      setOpenPages((current) => [...current, instance]);
      setActivePageId(instance.id);
    },
    [createPageInstance, workspaceRoot],
  );

  useEffect(() => {
    if (!artifact || artifact.sessionId !== sessionID) return;
    const page = createArtifactPage(artifact);
    setOpenPages((current) => {
      const index = current.findIndex((candidate) => candidate.id === page.id);
      if (index === -1) return [...current, page];
      const next = [...current];
      next[index] = page;
      return next;
    });
    setActivePageId(page.id);
  }, [artifact, sessionID]);

  const publishHeaderActions = useCallback((pageID: string, node: ReactNode) => {
    setHeaderActions((current) => {
      if (node == null) {
        if (!(pageID in current)) return current;
        const next = { ...current };
        delete next[pageID];
        return next;
      }
      return { ...current, [pageID]: node };
    });
  }, []);

  useEffect(() => {
    if (projectTask) return;
    const gitPages = openPages.filter((page) => page.kind === "git");
    if (gitPages.length === 0) return;
    const gitPageIDs = new Set(gitPages.map((page) => page.id));
    const remainingPages = openPages.filter((page) => page.kind !== "git");
    setOpenPages(remainingPages);
    setActivePageId((current) =>
      gitPageIDs.has(current) ? (remainingPages[remainingPages.length - 1]?.id ?? "") : current,
    );
    setHeaderActions((current) => {
      const next = { ...current };
      for (const id of gitPageIDs) delete next[id];
      return next;
    });
  }, [openPages, projectTask]);

  const closePage = useCallback(
    (id: string) => {
      const closingPage = openPages.find((page) => page.id === id);
      if (
        closingPage?.artifact &&
        artifact &&
        closingPage.artifact.sessionId === artifact.sessionId &&
        closingPage.artifact.id === artifact.id
      ) {
        onArtifactClose();
      }
      setOpenPages((current) => {
        const next = current.filter((page) => page.id !== id);
        // Closing the last tab returns to the launcher; the dock stays open (the
        // header close button collapses the whole dock).
        setActivePageId((active) => (active === id ? (next[next.length - 1]?.id ?? "") : active));
        return next;
      });
      publishHeaderActions(id, null);
    },
    [artifact, onArtifactClose, openPages, publishHeaderActions],
  );

  const activePage = openPages.find((page) => page.id === activePageId) ?? openPages[0];
  const hasOpenPages = openPages.length > 0;

  return (
    <div className="flex h-full min-h-0 flex-col bg-surface">
      <header className="flex min-h-11 items-center gap-1 border-b border-border pl-2 pr-1">
        <div role="tablist" className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {openPages.map((page) => {
            const Icon = page.icon;
            const active = page.id === activePage?.id;
            return (
              <div
                key={page.id}
                title={page.title}
                className="group flex h-8 shrink-0 items-center gap-0.5"
              >
                <Button
                  aria-selected={active}
                  className="h-8 rounded-xl px-2.5 text-xs"
                  size="sm"
                  variant={active ? "secondary" : "ghost"}
                  onPress={() => setActivePageId(page.id)}
                >
                  <Icon
                    className={cn("size-4 shrink-0", active ? "text-accent" : "text-accent/70")}
                  />
                  <span className="max-w-32 truncate font-medium">{page.label}</span>
                </Button>
                <Button
                  isIconOnly
                  aria-label={`Close ${page.label}`}
                  className="size-6 min-w-6 rounded-full text-muted/70 opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100"
                  size="sm"
                  variant="ghost"
                  onPress={() => closePage(page.id)}
                >
                  <X className="size-3" />
                </Button>
              </div>
            );
          })}

          {hasOpenPages ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  isIconOnly
                  aria-label="Add a panel"
                  className="shrink-0"
                  size="sm"
                  variant="ghost"
                >
                  <Plus className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="cocola-user-ui min-w-44 bg-overlay">
                {addablePages.length > 0 ? (
                  addablePages.map((page) => {
                    const Icon = page.icon;
                    return (
                      <DropdownMenuItem key={page.id} onSelect={() => openPage(page.id)}>
                        <Icon className="size-4 text-accent/80" />
                        <span>{page.label}</span>
                      </DropdownMenuItem>
                    );
                  })
                ) : (
                  <div className="px-2 py-1.5 text-xs text-muted">empty</div>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>

        {activePage ? (headerActions[activePage.id] ?? null) : null}

        <Button
          isIconOnly
          aria-label="Close side panel"
          className="shrink-0"
          size="sm"
          variant="ghost"
          onPress={onClose}
        >
          <X className="size-4" />
        </Button>
      </header>

      <div className="min-h-0 flex-1">
        {hasOpenPages ? null : <WorkspaceLauncher pages={basePages} onOpen={openPage} />}
        {openPages.map((page) => {
          const isActive = page.id === activePage?.id;
          if (!isActive && page.unmountWhenInactive) return null;
          return (
            <DockPagePanel
              key={page.id}
              page={page}
              sessionID={sessionID}
              active={isActive}
              workspaceRoot={workspaceRoot}
              openCodeFolder={openCodeFolder}
              publishHeaderActions={publishHeaderActions}
            />
          );
        })}
      </div>
    </div>
  );
}

function DockPagePanel({
  page,
  sessionID,
  active,
  workspaceRoot,
  openCodeFolder,
  publishHeaderActions,
}: {
  page: DockPage;
  sessionID: string;
  active: boolean;
  workspaceRoot: string;
  openCodeFolder: (workspacePath: string) => void;
  publishHeaderActions: (pageID: string, node: ReactNode) => void;
}) {
  const setHeaderActions = useCallback(
    (node: ReactNode) => publishHeaderActions(page.id, node),
    [page.id, publishHeaderActions],
  );
  return (
    <div
      role="tabpanel"
      hidden={!active}
      className={cn("h-full min-h-0", active ? "flex flex-col" : "hidden")}
    >
      {page.render({
        sessionID,
        active,
        workspaceRoot,
        setHeaderActions,
        openCodeFolder,
      })}
    </div>
  );
}

function GitPage({
  sessionID,
  active,
  setHeaderActions,
}: {
  sessionID: string;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
}) {
  const { closeCommit, closeDiff, commitDetail, diff, error, inspect, loading, snapshot } =
    useGitWorkspace(sessionID, active);
  const [refreshConfirmOpen, setRefreshConfirmOpen] = useState(false);

  useEffect(() => {
    if (!active) return;
    setHeaderActions(
      <Tooltip delay={0}>
        <Button
          isIconOnly
          aria-label="Refresh Git status"
          isDisabled={loading}
          size="sm"
          variant="ghost"
          onPress={() => setRefreshConfirmOpen(true)}
        >
          <RefreshCw className={cn("size-4", loading && "animate-spin")} />
        </Button>
        <Tooltip.Content>Refresh Git status (may restore the sandbox)</Tooltip.Content>
      </Tooltip>,
    );
    return () => setHeaderActions(null);
  }, [active, inspect, loading, setHeaderActions]);

  const changes = snapshot?.changes ?? [];
  const commits = snapshot?.commits ?? [];
  const stagedChanges = changes.filter((change) => change.area === "staged");
  const unstagedChanges = changes.filter((change) => change.area !== "staged");

  return (
    <>
      <div className="cocola-git-panel flex h-full min-h-0 flex-col bg-background">
        <GitSnapshotHeader snapshot={snapshot} />
        {error ? (
          <div className="border-danger/20 bg-danger-soft text-danger m-3 rounded-2xl border px-3 py-2 text-sm">
            {error}
          </div>
        ) : null}
        {loading && !snapshot ? (
          <div className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
            <LoaderCircle className="size-4 animate-spin" /> Loading saved history…
          </div>
        ) : diff ? (
          <GitDiffPanel diff={diff} onBack={closeDiff} />
        ) : commitDetail ? (
          <GitCommitPanel
            detail={commitDetail}
            snapshot={snapshot}
            onBack={closeCommit}
            onOpenDiff={(file) =>
              void inspect("commit", { path: file.path, commitSHA: commitDetail.commit.sha })
            }
          />
        ) : (
          <ScrollShadow hideScrollBar className="min-h-0 flex-1 overflow-y-auto">
            {changes.length === 0 && snapshot ? (
              <div className="flex items-center gap-2 border-b border-border/60 px-4 py-3 text-[12px] text-muted">
                <span className="grid size-4 place-items-center rounded bg-success/10 text-[9px] font-bold text-success">
                  ✓
                </span>
                Working tree clean · no uncommitted changes
              </div>
            ) : null}
            <GitChangeSection
              title="Staged Changes"
              changes={stagedChanges}
              onOpenDiff={(change) =>
                void inspect("diff", { path: change.path, diffTarget: "staged" })
              }
            />
            <GitChangeSection
              title="Changes"
              changes={unstagedChanges}
              onOpenDiff={(change) =>
                void inspect("diff", {
                  path: change.path,
                  diffTarget: change.area === "staged" ? "staged" : "working",
                })
              }
            />
            {snapshot?.truncated ? (
              <div className="px-4 py-1.5 text-[11px] text-warning">
                Showing the first 500 changed paths.
              </div>
            ) : null}
            <GitSectionHeader title="Commits" count={commits.length || undefined} />
            {commits.length ? (
              <ListView
                aria-label="Git commit history"
                className="rounded-none border-0 bg-transparent pb-2 shadow-none"
                selectionMode="none"
                variant="secondary"
                onAction={(key) => void inspect("commit", { commitSHA: String(key) })}
              >
                {commits.map((commit, index) => (
                  <GitCommitLogRow
                    key={commit.sha}
                    commit={commit}
                    snapshot={snapshot}
                    last={index === commits.length - 1}
                  />
                ))}
              </ListView>
            ) : snapshot ? (
              <GitEmptyState
                icon={<GitCommitHorizontal className="size-5 text-accent" />}
                title="No commit history"
                description="Refresh Git status to load the latest saved history."
              />
            ) : (
              <GitEmptyState
                icon={<GitBranch className="size-5 text-accent" />}
                title="No Git snapshot"
                description="Refresh to inspect this project workspace."
              />
            )}
            {snapshot?.history_truncated ? (
              <div className="border-t border-border px-4 py-2 text-center text-xs text-muted">
                Showing the latest 50 commits.
              </div>
            ) : null}
          </ScrollShadow>
        )}
      </div>

      <Sheet
        isOpen={refreshConfirmOpen}
        placement="right"
        onOpenChange={(open) => {
          if (!loading) setRefreshConfirmOpen(open);
        }}
      >
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[420px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close Git refresh confirmation" />
              <Sheet.Header>
                <Sheet.Heading>Refresh Git status?</Sheet.Heading>
                <p className="text-sm text-muted">
                  Refreshing may restore the project sandbox if its workspace has been reclaimed.
                </p>
              </Sheet.Header>
              <Sheet.Footer className="gap-2">
                <Button variant="outline" onPress={() => setRefreshConfirmOpen(false)}>
                  Cancel
                </Button>
                <Button
                  isPending={loading}
                  onPress={() => {
                    setRefreshConfirmOpen(false);
                    void inspect("status");
                  }}
                >
                  Refresh
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
    </>
  );
}

function GitSnapshotHeader({ snapshot }: { snapshot: GitSnapshot | null }) {
  const capturedLabel = snapshot?.captured_at
    ? `Captured ${formatGitRelativeTime(snapshot.captured_at)}`
    : "No saved snapshot yet";
  const revisionLabel =
    snapshot?.base_sha && snapshot?.head_sha
      ? `${snapshot.base_sha.slice(0, 7)} → ${snapshot.head_sha.slice(0, 7)}`
      : "";

  return (
    <div className="flex min-w-0 items-center gap-2 border-b border-border bg-surface-secondary/20 px-3 py-2">
      <span className="grid size-6 shrink-0 place-items-center rounded-md bg-accent/10 text-accent">
        <GitBranch className="size-3.5" />
      </span>
      <span className="shrink-0 truncate text-[13px] font-semibold">
        {snapshot?.branch || "Project branch"}
      </span>
      <span
        className="min-w-0 flex-1 truncate text-[10.5px] text-muted"
        title={revisionLabel ? `${capturedLabel} · ${revisionLabel}` : capturedLabel}
      >
        {capturedLabel}
        {revisionLabel ? (
          <>
            <span className="px-1.5 opacity-50" aria-hidden="true">
              ·
            </span>
            <span className="font-mono">{revisionLabel}</span>
          </>
        ) : null}
      </span>
      {snapshot?.ahead ? (
        <Chip color="success" size="sm" variant="soft">
          ↑ {snapshot.ahead} ahead
        </Chip>
      ) : null}
    </div>
  );
}

function GitSectionHeader({ title, count }: { title: string; count?: number }) {
  return (
    <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border/60 bg-background/95 px-3 py-2 text-xs font-semibold text-foreground backdrop-blur">
      <span>{title}</span>
      {count != null ? (
        <Chip className="ml-auto" size="sm" variant="soft">
          {count}
        </Chip>
      ) : null}
    </div>
  );
}

function GitStatusLetter({ status }: { status: string }) {
  const code = gitChangeCode(status);
  return (
    <span
      className={cn(
        "w-[15px] shrink-0 text-center font-mono text-[11px] font-bold",
        code === "A" && "text-success",
        code === "D" && "text-danger",
        code === "R" && "text-accent",
        code === "U" && "text-accent",
        !["A", "D", "R", "U"].includes(code) && "text-warning",
      )}
    >
      {code}
    </span>
  );
}

function gitChangeKey(change: GitChange) {
  return `${change.area}:${change.path}`;
}

function GitChangeRow({ change }: { change: GitChange }) {
  const slash = change.path.lastIndexOf("/");
  const name = slash < 0 ? change.path : change.path.slice(slash + 1);
  const dir = slash < 0 ? "" : change.path.slice(0, slash);
  return (
    <ListView.Item
      id={gitChangeKey(change)}
      textValue={change.old_path ? `${change.old_path} ${change.path}` : change.path}
      className="group rounded-none px-4 py-2"
    >
      <ListView.ItemContent className="gap-2">
        <MaterialFileIcon
          name={resolveFileType(name).icon}
          className="flex size-4 shrink-0 items-center justify-center"
        />
        <span className="flex min-w-0 flex-col">
          <ListView.Title>{name}</ListView.Title>
          {dir ? <ListView.Description>{dir}</ListView.Description> : null}
        </span>
      </ListView.ItemContent>
      <ListView.ItemAction>
        <GitStatusLetter status={change.status} />
        <ChevronRight className="size-3.5 text-muted/60 transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
      </ListView.ItemAction>
    </ListView.Item>
  );
}

function GitChangeSection({
  title,
  changes,
  onOpenDiff,
}: {
  title: string;
  changes: GitChange[];
  onOpenDiff: (change: GitChange) => void;
}) {
  if (!changes.length) return null;
  return (
    <>
      <GitSectionHeader title={title} count={changes.length} />
      <ListView
        aria-label={title}
        className="rounded-none border-0 bg-transparent shadow-none"
        selectionMode="none"
        variant="secondary"
        onAction={(key) => {
          const change = changes.find((candidate) => gitChangeKey(candidate) === String(key));
          if (change) onOpenDiff(change);
        }}
      >
        {changes.map((change) => (
          <GitChangeRow key={gitChangeKey(change)} change={change} />
        ))}
      </ListView>
    </>
  );
}

function GitEmptyState({
  icon,
  title,
  description,
}: {
  icon: ReactNode;
  title: string;
  description: string;
}) {
  return (
    <EmptyState size="sm" className="mx-auto my-8 max-w-xs">
      <EmptyState.Header>
        <EmptyState.Media variant="icon">{icon}</EmptyState.Media>
        <EmptyState.Title>{title}</EmptyState.Title>
        <EmptyState.Description>{description}</EmptyState.Description>
      </EmptyState.Header>
    </EmptyState>
  );
}

const GIT_AVATAR_COLORS = [
  "bg-accent",
  "bg-success",
  "bg-warning",
  "bg-danger",
  "bg-foreground",
  "bg-accent/70",
];

function gitAuthorInitials(name: string) {
  const parts = String(name || "?")
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  const initials = parts
    .map((word) => word[0])
    .slice(0, 2)
    .join("");
  return (initials || "?").toUpperCase();
}

function gitAuthorColor(name: string) {
  let hash = 0;
  for (let index = 0; index < name.length; index += 1) {
    hash = (hash * 31 + name.charCodeAt(index)) >>> 0;
  }
  return GIT_AVATAR_COLORS[hash % GIT_AVATAR_COLORS.length];
}

function GitAuthorAvatar({ name, merge = false }: { name: string; merge?: boolean }) {
  return (
    <span
      className={cn(
        "grid size-[18px] shrink-0 place-items-center rounded-full text-[8.5px] font-bold text-white",
        merge ? "bg-accent" : gitAuthorColor(name),
      )}
      title={name || "Unknown author"}
    >
      {merge ? <GitMerge className="size-2.5" /> : gitAuthorInitials(name)}
    </span>
  );
}

function GitRefBadges({ commit, snapshot }: { commit: GitCommit; snapshot: GitSnapshot | null }) {
  const badges = gitCommitBadges(commit, snapshot);
  if (!badges.length) return null;
  return (
    <span className="flex shrink-0 items-center gap-1">
      {badges.map((badge) => (
        <span
          key={`${badge.tone}:${badge.label}`}
          className={cn(
            "max-w-28 truncate rounded px-1.5 py-px text-[9px] font-bold",
            badge.tone === "head" && "bg-accent/10 text-accent",
            badge.tone === "base" && "bg-foreground/10 text-foreground",
            badge.tone === "tag" && "bg-warning/10 text-warning",
            badge.tone === "ref" && "bg-success/10 text-success",
          )}
        >
          {badge.label}
        </span>
      ))}
    </span>
  );
}

function GitCommitLogRow({
  commit,
  snapshot,
  last,
}: {
  commit: GitCommit;
  snapshot: GitSnapshot | null;
  last: boolean;
}) {
  const merge = (commit.parents?.length ?? 0) > 1;
  const isBase = commit.sha === snapshot?.base_sha;
  return (
    <ListView.Item
      id={commit.sha}
      textValue={`${commit.subject} ${commit.author_name} ${commit.sha}`}
      className="group relative rounded-none py-2 pl-3.5 pr-3"
    >
      {!last ? (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute bottom-0 left-[23px] top-0 w-0.5 bg-border"
        />
      ) : null}
      <span
        aria-hidden="true"
        className={cn(
          "relative z-[1] ml-[5px] size-[9px] shrink-0 rounded-full border-2 border-background",
          isBase || merge ? "bg-foreground" : "bg-accent",
        )}
      />
      <ListView.ItemContent className="gap-2">
        <GitAuthorAvatar name={commit.author_name} merge={merge} />
        <span className="flex min-w-0 flex-col">
          <ListView.Title>{commit.subject || "Untitled commit"}</ListView.Title>
          <ListView.Description>{commit.author_name || "Unknown author"}</ListView.Description>
        </span>
      </ListView.ItemContent>
      <ListView.ItemAction className="gap-2">
        <GitRefBadges commit={commit} snapshot={snapshot} />
        <span className="hidden shrink-0 text-[10px] text-muted sm:inline">
          {formatGitRelativeTime(commit.authored_at)}
        </span>
        <span className="shrink-0 font-mono text-[9.5px] text-muted/80">
          {commit.sha.slice(0, 7)}
        </span>
        <ChevronRight className="size-3.5 text-muted/60 transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
      </ListView.ItemAction>
    </ListView.Item>
  );
}

function GitPanelBackHeader({ title, onBack }: { title: string; onBack: () => void }) {
  return (
    <div className="flex min-h-11 shrink-0 items-center gap-2 border-b border-border px-2">
      <Button
        isIconOnly
        aria-label="Back to Git history"
        size="sm"
        variant="ghost"
        onPress={onBack}
      >
        <ArrowLeft className="size-4" />
      </Button>
      <span className="min-w-0 flex-1 truncate text-xs font-semibold">{title}</span>
    </div>
  );
}

function GitCommitPanel({
  detail,
  snapshot,
  onBack,
  onOpenDiff,
}: {
  detail: GitCommitDetail;
  snapshot: GitSnapshot | null;
  onBack: () => void;
  onOpenDiff: (file: GitCommitFile) => void;
}) {
  const { commit, files } = detail;
  const badges = gitCommitBadges(commit, snapshot);
  const description = gitCommitDescription(commit);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <GitPanelBackHeader title={commit.subject || "Commit details"} onBack={onBack} />
      <ScrollShadow hideScrollBar className="min-h-0 flex-1 overflow-y-auto">
        <div className="border-b border-border bg-surface-secondary/20 px-4 py-4">
          <div className="flex items-start gap-3">
            <span className="border-accent/20 bg-accent-soft text-accent grid size-9 shrink-0 place-items-center rounded-xl border">
              {(commit.parents?.length ?? 0) > 1 ? (
                <GitMerge className="size-4" />
              ) : (
                <GitCommitHorizontal className="size-4" />
              )}
            </span>
            <div className="min-w-0 flex-1">
              <h3 className="text-sm font-semibold leading-5 text-foreground">{commit.subject}</h3>
              {description ? (
                <p className="mt-1 whitespace-pre-wrap text-xs leading-5 text-muted">
                  {description}
                </p>
              ) : null}
            </div>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-1.5 pl-12">
            {badges.map((badge) => (
              <Chip key={`${badge.tone}:${badge.label}`} size="sm" variant="soft">
                {badge.label}
              </Chip>
            ))}
            <Chip size="sm" variant="secondary">
              <span className="font-mono">{commit.sha.slice(0, 12)}</span>
            </Chip>
          </div>
          <div className="mt-3 grid grid-cols-[1fr_auto] gap-x-3 gap-y-1 pl-12 text-[11px] text-muted">
            <span className="truncate">{commit.author_name || "Unknown author"}</span>
            <span>{formatGitRelativeTime(commit.authored_at)}</span>
            <span>{commit.files_changed ?? files.length} files changed</span>
            <span className="font-mono">
              <span className="text-success">+{commit.additions ?? 0}</span>{" "}
              <span className="text-danger">−{commit.deletions ?? 0}</span>
            </span>
          </div>
        </div>
        {files.length ? (
          <ListView
            aria-label="Files changed in commit"
            className="rounded-none border-0 bg-transparent shadow-none"
            selectionMode="none"
            variant="secondary"
            onAction={(key) => {
              const file = files.find(
                (candidate) => `${candidate.status}:${candidate.path}` === String(key),
              );
              if (file) onOpenDiff(file);
            }}
          >
            {files.map((file) => (
              <GitFileRow key={`${file.status}:${file.path}`} file={file} />
            ))}
          </ListView>
        ) : (
          <GitEmptyState
            icon={<File className="size-5 text-accent" />}
            title="No file changes"
            description="This commit does not contain any file changes."
          />
        )}
      </ScrollShadow>
      {detail.truncated ? (
        <div className="border-t border-border px-3 py-2 text-xs text-warning">
          Showing the first 500 changed paths.
        </div>
      ) : null}
    </div>
  );
}

function GitFileRow({ file }: { file: GitCommitFile }) {
  const { path, old_path: oldPath, status, binary } = file;
  const slash = path.lastIndexOf("/");
  const name = slash < 0 ? path : path.slice(slash + 1);
  return (
    <ListView.Item
      id={`${status}:${path}`}
      textValue={`${path} ${oldPath ?? ""}`}
      className="group rounded-none px-4 py-2.5"
    >
      <ListView.ItemContent className="gap-2.5">
        <MaterialFileIcon
          name={resolveFileType(name).icon}
          className="flex size-4 shrink-0 items-center justify-center"
        />
        <span className="flex min-w-0 flex-col">
          <ListView.Title>{path}</ListView.Title>
          {oldPath || binary ? (
            <ListView.Description>
              {oldPath ? `${oldPath} → ${path}` : "Binary file"}
            </ListView.Description>
          ) : null}
        </span>
      </ListView.ItemContent>
      <ListView.ItemAction>
        <GitStatusLetter status={status} />
        <ChevronRight className="size-3.5 text-muted/60 transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
      </ListView.ItemAction>
    </ListView.Item>
  );
}

let gitDiffLanguagesRegistered = false;
function ensureGitDiffLanguages() {
  if (gitDiffLanguagesRegistered) return;
  gitDiffLanguagesRegistered = true;
  for (const lang of [
    refractorMarkup,
    refractorCss,
    refractorJavascript,
    refractorTypescript,
    refractorJsx,
    refractorTsx,
    refractorGo,
    refractorJson,
    refractorPython,
    refractorBash,
    refractorYaml,
    refractorMarkdown,
  ]) {
    try {
      refractor.register(lang);
    } catch {
      /* already registered */
    }
  }
}

// react-diff-view@3 expects refractor.highlight() to return a node array, but
// refractor@4 returns a hast root object. Adapt by unwrapping .children.
const gitDiffRefractor = {
  highlight: (value: string, language: string) => refractor.highlight(value, language).children,
} as unknown as typeof refractor;

function gitDiffLanguage(path: string): string | null {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  switch (ext) {
    case "ts":
      return "typescript";
    case "tsx":
      return "tsx";
    case "js":
    case "cjs":
    case "mjs":
      return "javascript";
    case "jsx":
      return "jsx";
    case "go":
      return "go";
    case "json":
      return "json";
    case "css":
    case "scss":
    case "less":
      return "css";
    case "html":
    case "htm":
    case "xml":
    case "svg":
      return "markup";
    case "py":
      return "python";
    case "sh":
    case "bash":
    case "zsh":
      return "bash";
    case "yml":
    case "yaml":
      return "yaml";
    case "md":
    case "markdown":
    case "mdx":
      return "markdown";
    default:
      return null;
  }
}

function GitDiffPanel({ diff, onBack }: { diff: GitDiff; onBack: () => void }) {
  const [viewType, setViewType] = useState<"unified" | "split">("unified");
  const parsed = useMemo(() => {
    if (!diff.text) return { files: [], error: "" };
    try {
      return { files: parseDiff(diff.text), error: "" };
    } catch {
      return { files: [], error: "This patch could not be rendered." };
    }
  }, [diff.text]);

  const tokensByFile = useMemo(() => {
    ensureGitDiffLanguages();
    return parsed.files.map((file) => {
      const language = gitDiffLanguage(file.newPath || file.oldPath || diff.path);
      if (!language) return undefined;
      try {
        return tokenize(file.hunks, {
          highlight: true,
          refractor: gitDiffRefractor,
          language,
        });
      } catch {
        return undefined;
      }
    });
  }, [parsed.files, diff.path]);

  const gutterWidth = useMemo(() => {
    let maxLineNumber = 0;
    for (const file of parsed.files) {
      for (const hunk of file.hunks) {
        maxLineNumber = Math.max(
          maxLineNumber,
          hunk.oldStart + Math.max(0, hunk.oldLines - 1),
          hunk.newStart + Math.max(0, hunk.newLines - 1),
        );
      }
    }
    return gitDiffGutterWidth(maxLineNumber);
  }, [parsed.files]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-11 shrink-0 items-center gap-2 border-b border-border px-2">
        <Button
          isIconOnly
          aria-label="Back to changed files"
          size="sm"
          variant="ghost"
          onPress={onBack}
        >
          <ArrowLeft className="size-4" />
        </Button>
        {(() => {
          const s = diff.path.lastIndexOf("/");
          const fileName = s < 0 ? diff.path : diff.path.slice(s + 1);
          const fileDir = s < 0 ? "" : diff.path.slice(0, s + 1);
          return (
            <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-muted">
              {fileDir}
              <span className="font-semibold text-foreground">{fileName}</span>
            </span>
          );
        })()}
        <Segment
          aria-label="Diff layout"
          selectedKey={viewType}
          size="sm"
          onSelectionChange={(key) => setViewType(String(key) === "split" ? "split" : "unified")}
        >
          <Segment.Item id="unified">Unified</Segment.Item>
          <Segment.Item id="split">Split</Segment.Item>
        </Segment>
      </div>
      {diff.binary ? (
        <GitEmptyState
          icon={<FileQuestion className="size-5 text-warning" />}
          title="Binary file"
          description="Binary differences cannot be displayed in the browser."
        />
      ) : parsed.error ? (
        <GitEmptyState
          icon={<AlertTriangle className="size-5 text-danger" />}
          title="Diff unavailable"
          description={parsed.error}
        />
      ) : parsed.files.length ? (
        <div
          className="cocola-git-diff min-h-0 flex-1 overflow-auto bg-background text-xs"
          style={{ "--cocola-diff-gutter-width": gutterWidth } as CSSProperties}
        >
          {parsed.files.map((file, index) => (
            <div
              key={`${file.oldPath}:${file.newPath}:${index}`}
              className="border-b border-border last:border-b-0"
            >
              <DiffView
                viewType={viewType}
                diffType={file.type}
                hunks={file.hunks}
                tokens={tokensByFile[index]}
              >
                {(hunks) =>
                  hunks.map((hunk) => (
                    <Hunk key={`${hunk.oldStart}:${hunk.newStart}`} hunk={hunk} />
                  ))
                }
              </DiffView>
            </div>
          ))}
        </div>
      ) : (
        <GitEmptyState
          icon={<File className="size-5 text-accent" />}
          title="No changes"
          description="There is no diff for this target."
        />
      )}
      {diff.truncated ? (
        <div className="bg-warning-soft text-warning-soft-foreground border-t border-border px-3 py-2 text-xs">
          Diff truncated at 512 KiB.
        </div>
      ) : null}
    </div>
  );
}

function ArtifactPreviewPage({
  artifact,
  active,
  setHeaderActions,
}: {
  artifact: ArtifactPreview;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
}) {
  const [htmlSourceMode, setHtmlSourceMode] = useState(false);
  const canHtml = isHtmlPreview(artifact.mimeType, artifact.filename);
  const previewFile = useMemo<PreviewFile>(
    () => ({
      filename: artifact.filename,
      size: artifact.size,
      mimeType: artifact.mimeType,
      url: artifact.downloadUrl,
    }),
    [artifact],
  );

  useEffect(() => {
    setHtmlSourceMode(false);
  }, [artifact.downloadUrl, artifact.id]);

  useEffect(() => {
    if (!active) return;
    setHeaderActions(
      <div className="flex items-center gap-1">
        {canHtml ? (
          <button
            type="button"
            aria-label={htmlSourceMode ? "Preview HTML" : "View HTML source"}
            title={htmlSourceMode ? "Preview HTML" : "View source"}
            onClick={() => setHtmlSourceMode((value) => !value)}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
          >
            {htmlSourceMode ? <Eye className="size-4" /> : <Code2 className="size-4" />}
          </button>
        ) : null}
        {artifact.downloadUrl ? (
          <a
            href={artifact.downloadUrl}
            download={artifact.filename}
            title="Download"
            aria-label={`Download ${artifact.filename}`}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
          >
            <Download className="size-4" />
          </a>
        ) : null}
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, artifact, canHtml, htmlSourceMode, setHeaderActions]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex min-h-9 items-center border-b border-border px-3 text-xs text-muted">
        <span className="truncate">
          {formatBytes(artifact.size)} · {artifact.mimeType}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <ReadonlyFilePreview
          file={previewFile}
          renderHtml={canHtml && !htmlSourceMode}
          fetchBinary
          unsupportedMessage="Download the file to open it locally."
        />
      </div>
    </div>
  );
}

// Empty-state launcher: lists the available panels centered in the dock so the
// user can pick one to open (mirrors a command-menu style row list).
function WorkspaceLauncher({ pages, onOpen }: { pages: DockPage[]; onOpen: (id: string) => void }) {
  return (
    <div className="flex h-full min-h-0 flex-col items-center justify-center px-6">
      <EmptyState className="w-full max-w-sm" size="sm">
        <EmptyState.Header>
          <EmptyState.Media variant="icon">
            <SquareTerminal className="size-5 text-accent" />
          </EmptyState.Media>
          <EmptyState.Title>Open a workspace panel</EmptyState.Title>
          <EmptyState.Description>
            Inspect files, use the shell, open previews, or review Git history.
          </EmptyState.Description>
        </EmptyState.Header>
        <EmptyState.Content className="w-full">
          <ListView
            aria-label="Workspace panels"
            selectionMode="none"
            variant="secondary"
            onAction={(key) => onOpen(String(key))}
          >
            {pages.map((page) => {
              const Icon = page.icon;
              return (
                <ListView.Item key={page.id} id={page.id} textValue={page.label}>
                  <ListView.ItemContent className="gap-3">
                    <Icon className="size-5 shrink-0 text-accent" />
                    <ListView.Title>{page.label}</ListView.Title>
                  </ListView.ItemContent>
                  <ListView.ItemAction>
                    <ChevronRight className="size-4 text-muted" />
                  </ListView.ItemAction>
                </ListView.Item>
              );
            })}
          </ListView>
        </EmptyState.Content>
      </EmptyState>
    </div>
  );
}

// -- Sub-page: workspace file browser ----------------------------------------

type WorkspaceEntry = {
  name: string;
  path: string;
  kind: "directory" | "file" | "symlink" | "other";
  size: number;
  modified_at: string;
  previewable: boolean;
  preview_kind?: "markdown" | "code" | "image" | "pdf";
};

type DirectoryResponse = {
  path: string;
  entries: WorkspaceEntry[];
  next_cursor: string;
};

type DirectoryState = {
  entries: WorkspaceEntry[];
  nextCursor: string;
  loading: boolean;
  error: string;
  errorCode: string;
};

const EMPTY_DIRECTORY: DirectoryState = {
  entries: [],
  nextCursor: "",
  loading: false,
  error: "",
  errorCode: "",
};

const DEFAULT_TREE_WIDTH = 240;
const MIN_TREE_WIDTH = 180;
const MAX_TREE_WIDTH = 360;
const MIN_PREVIEW_WIDTH = 220;
const TREE_RESIZE_STEP = 16;

type TreeResizeSession = {
  pointerID: number;
  startX: number;
  startWidth: number;
  maxWidth: number;
  previousCursor: string;
  previousUserSelect: string;
};

function WorkspaceFilesPage({
  sessionID,
  active,
  setHeaderActions,
  onOpenCode,
  workspaceRoot,
}: {
  sessionID: string;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
  onOpenCode: (workspacePath: string) => void;
  workspaceRoot: string;
}) {
  const [directories, setDirectories] = useState<Record<string, DirectoryState>>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<WorkspaceEntry | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [treeWidth, setTreeWidth] = useState(DEFAULT_TREE_WIDTH);
  const [resizingTree, setResizingTree] = useState(false);
  const layoutRef = useRef<HTMLDivElement>(null);
  const treeResizeRef = useRef<TreeResizeSession | null>(null);

  const treeMaxWidth = useCallback(() => {
    const layoutWidth = layoutRef.current?.getBoundingClientRect().width ?? 0;
    if (layoutWidth === 0) return MAX_TREE_WIDTH;
    return Math.max(MIN_TREE_WIDTH, Math.min(MAX_TREE_WIDTH, layoutWidth - MIN_PREVIEW_WIDTH - 1));
  }, []);

  const beginTreeResize = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) return;
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      treeResizeRef.current = {
        pointerID: event.pointerId,
        startX: event.clientX,
        startWidth: treeWidth,
        maxWidth: treeMaxWidth(),
        previousCursor: document.body.style.cursor,
        previousUserSelect: document.body.style.userSelect,
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      setResizingTree(true);
    },
    [treeMaxWidth, treeWidth],
  );

  const moveTreeResize = useCallback((event: PointerEvent<HTMLDivElement>) => {
    const session = treeResizeRef.current;
    if (!session || session.pointerID !== event.pointerId) return;
    const nextWidth = session.startWidth + event.clientX - session.startX;
    setTreeWidth(Math.min(Math.max(nextWidth, MIN_TREE_WIDTH), session.maxWidth));
  }, []);

  const endTreeResize = useCallback((event: PointerEvent<HTMLDivElement>) => {
    const session = treeResizeRef.current;
    if (!session || session.pointerID !== event.pointerId) return;
    treeResizeRef.current = null;
    document.body.style.cursor = session.previousCursor;
    document.body.style.userSelect = session.previousUserSelect;
    setResizingTree(false);
  }, []);

  const resizeTreeWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      const maxWidth = treeMaxWidth();
      let nextWidth: number | null = null;
      if (event.key === "ArrowLeft") nextWidth = treeWidth - TREE_RESIZE_STEP;
      if (event.key === "ArrowRight") nextWidth = treeWidth + TREE_RESIZE_STEP;
      if (event.key === "Home") nextWidth = MIN_TREE_WIDTH;
      if (event.key === "End") nextWidth = maxWidth;
      if (nextWidth === null) return;
      event.preventDefault();
      setTreeWidth(Math.min(Math.max(nextWidth, MIN_TREE_WIDTH), maxWidth));
    },
    [treeMaxWidth, treeWidth],
  );

  useEffect(
    () => () => {
      const session = treeResizeRef.current;
      if (!session) return;
      document.body.style.cursor = session.previousCursor;
      document.body.style.userSelect = session.previousUserSelect;
    },
    [],
  );

  const loadDirectory = useCallback(
    async (path: string, append = false, cursor = "") => {
      setDirectories((current) => ({
        ...current,
        [path]: {
          ...(current[path] ?? EMPTY_DIRECTORY),
          loading: true,
          error: "",
          errorCode: "",
        },
      }));
      const query = new URLSearchParams();
      if (path) query.set("path", path);
      if (cursor) query.set("cursor", cursor);
      try {
        const response = await fetch(
          `/api/conversations/${encodeURIComponent(sessionID)}/workspace/entries?${query}`,
          { cache: "no-store" },
        );
        if (!response.ok) {
          const failure = await workspaceFailure(response);
          throw new WorkspaceRequestError(failure.code, failure.message);
        }
        const result = (await response.json()) as DirectoryResponse;
        setDirectories((current) => ({
          ...current,
          [path]: {
            entries: append
              ? [...(current[path]?.entries ?? []), ...result.entries]
              : result.entries,
            nextCursor: result.next_cursor,
            loading: false,
            error: "",
            errorCode: "",
          },
        }));
      } catch (err) {
        const failure = workspaceErrorMessage(err);
        setDirectories((current) => ({
          ...current,
          [path]: {
            ...(current[path] ?? EMPTY_DIRECTORY),
            loading: false,
            error: failure.message,
            errorCode: failure.code,
          },
        }));
      }
    },
    [sessionID],
  );

  useEffect(() => {
    setDirectories({});
    setExpanded(new Set());
    setSelected(null);
    void loadDirectory(workspaceRoot);
  }, [loadDirectory, workspaceRoot]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    setDirectories({});
    setExpanded(new Set());
    setSelected(null);
    await loadDirectory(workspaceRoot);
    setRefreshing(false);
  }, [loadDirectory, workspaceRoot]);

  const toggleDirectory = useCallback(
    (path: string) => {
      setExpanded((current) => {
        const next = new Set(current);
        if (next.has(path)) {
          next.delete(path);
        } else {
          next.add(path);
          if (!directories[path]) void loadDirectory(path);
        }
        return next;
      });
    },
    [directories, loadDirectory],
  );

  const root = directories[workspaceRoot];
  const rootReady = Boolean(root && !root.loading && !root.error);

  // Publish root-open and refresh controls into the shared dock header while
  // this tab is active; clear them when the page is hidden or unmounts.
  useEffect(() => {
    if (!active) return;
    setHeaderActions(
      <div className="flex items-center gap-1">
        <TooltipIconButton
          type="button"
          tooltip="Open in Code Server"
          disabled={!rootReady}
          onClick={() => onOpenCode(workspaceRoot)}
          className="size-8 rounded-full text-muted"
        >
          <Code2 className="size-4" />
        </TooltipIconButton>
        <button
          type="button"
          title="Refresh workspace"
          aria-label="Refresh workspace"
          disabled={refreshing}
          onClick={() => void refresh()}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus disabled:opacity-50"
        >
          <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
        </button>
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, onOpenCode, refreshing, refresh, rootReady, setHeaderActions, workspaceRoot]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-surface">
      <div
        ref={layoutRef}
        className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[var(--workspace-tree-width)_1px_minmax(0,1fr)]"
        style={{ ["--workspace-tree-width" as string]: `${treeWidth}px` }}
      >
        <section
          aria-label="Workspace files"
          className={cn("min-h-0 flex-col bg-background md:flex", selected ? "hidden" : "flex")}
        >
          <div className="min-h-0 flex-1 overflow-y-auto py-1" role="tree">
            {!root || root.loading ? (
              <WorkspaceLoading />
            ) : root.error ? (
              <WorkspaceError
                code={root.errorCode}
                message={root.error}
                onRetry={() => void loadDirectory(workspaceRoot)}
              />
            ) : root.entries.length === 0 ? (
              <div className="flex flex-col items-center gap-2 px-5 py-12 text-center">
                <Folder className="size-7 text-muted/70" />
                <div className="text-sm font-medium text-foreground">
                  {workspaceRoot ? "Project is empty" : "Workspace is empty"}
                </div>
                <div className="text-xs text-muted">
                  Files created by the agent will appear here after refresh.
                </div>
              </div>
            ) : (
              <WorkspaceTree
                path={workspaceRoot}
                depth={0}
                directories={directories}
                expanded={expanded}
                selectedPath={selected?.path ?? ""}
                onToggle={toggleDirectory}
                onSelect={setSelected}
                onOpenCode={onOpenCode}
                onLoadMore={(path, cursor) => void loadDirectory(path, true, cursor)}
                onReload={(path) => void loadDirectory(path)}
              />
            )}
          </div>
        </section>

        <div
          role="separator"
          aria-label="Resize workspace file tree"
          aria-orientation="vertical"
          aria-valuemin={MIN_TREE_WIDTH}
          aria-valuemax={MAX_TREE_WIDTH}
          aria-valuenow={Math.round(treeWidth)}
          aria-valuetext={`${Math.round(treeWidth)} pixels`}
          tabIndex={0}
          title="Drag to resize file tree"
          onKeyDown={resizeTreeWithKeyboard}
          onPointerDown={beginTreeResize}
          onPointerMove={moveTreeResize}
          onPointerUp={endTreeResize}
          onPointerCancel={endTreeResize}
          onLostPointerCapture={endTreeResize}
          className="group relative z-10 hidden w-px cursor-col-resize touch-none focus-visible:outline-none md:block"
        >
          <span
            className={cn(
              "absolute inset-y-0 left-1/2 w-3 -translate-x-1/2 bg-transparent transition-colors group-hover:bg-accent/10 group-focus-visible:bg-accent/10",
              resizingTree && "bg-accent/10",
            )}
          />
          <span
            className={cn(
              "absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-border transition-colors group-hover:bg-accent/80 group-focus-visible:bg-accent/80",
              resizingTree && "bg-accent",
            )}
          />
        </div>

        <section
          aria-label="Workspace file preview"
          className={cn("min-h-0 flex-col bg-background md:flex", selected ? "flex" : "hidden")}
        >
          {selected ? (
            <WorkspaceFilePreview
              entry={selected}
              sessionID={sessionID}
              onBack={() => setSelected(null)}
            />
          ) : (
            <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted">
              <FileCode2 className="size-9 stroke-[1.4]" />
              <div>
                <p className="text-sm font-medium text-foreground">Select a file to preview</p>
                <p className="mt-1 text-xs">Workspace access is read-only.</p>
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

function WorkspaceTree({
  path,
  depth,
  directories,
  expanded,
  selectedPath,
  onToggle,
  onSelect,
  onOpenCode,
  onLoadMore,
  onReload,
}: {
  path: string;
  depth: number;
  directories: Record<string, DirectoryState>;
  expanded: Set<string>;
  selectedPath: string;
  onToggle: (path: string) => void;
  onSelect: (entry: WorkspaceEntry) => void;
  onOpenCode: (workspacePath: string) => void;
  onLoadMore: (path: string, cursor: string) => void;
  onReload: (path: string) => void;
}) {
  const directory = directories[path];
  if (!directory) return null;
  return (
    <>
      {directory.entries.map((entry) => {
        const isDirectory = entry.kind === "directory";
        const isExpanded = isDirectory && expanded.has(entry.path);
        const child = directories[entry.path];
        return (
          <div key={entry.path}>
            <div className="group/tree-row relative">
              <button
                type="button"
                role="treeitem"
                aria-expanded={isDirectory ? isExpanded : undefined}
                aria-selected={selectedPath === entry.path}
                onClick={() => (isDirectory ? onToggle(entry.path) : onSelect(entry))}
                className={cn(
                  "group flex h-8 w-full items-center gap-1.5 border-l-2 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-focus",
                  isDirectory ? "pr-9" : "pr-2",
                  selectedPath === entry.path
                    ? "border-l-primary bg-accent/10 text-foreground"
                    : "border-l-transparent text-muted hover:bg-surface-secondary/70 hover:text-foreground",
                )}
                style={{ paddingLeft: `${8 + depth * 14}px` }}
              >
                {isDirectory ? (
                  <ChevronRight
                    className={cn(
                      "size-3.5 shrink-0 transition-transform",
                      isExpanded && "rotate-90",
                    )}
                  />
                ) : (
                  <span className="w-3.5 shrink-0" />
                )}
                {isDirectory ? (
                  isExpanded ? (
                    <FolderOpen className="size-4 shrink-0 text-accent/80" />
                  ) : (
                    <Folder className="size-4 shrink-0 text-accent/70" />
                  )
                ) : entry.kind === "file" ? (
                  <MaterialFileIcon
                    name={resolveFileType(entry.name).icon}
                    className="flex size-4 shrink-0 items-center justify-center"
                  />
                ) : (
                  <File className="size-4 shrink-0" />
                )}
                <span className="min-w-0 flex-1 truncate">{entry.name}</span>
              </button>
              {isDirectory ? (
                <TooltipIconButton
                  type="button"
                  tooltip="Open in Code Server"
                  onClick={() => onOpenCode(entry.path)}
                  className="absolute right-1 top-1/2 size-6 -translate-y-1/2 text-muted opacity-100 transition-opacity hover:text-foreground focus-visible:opacity-100 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/tree-row:opacity-100 [@media(hover:hover)]:focus-visible:opacity-100"
                >
                  <Code2 className="size-3.5" />
                </TooltipIconButton>
              ) : null}
            </div>
            {isExpanded ? (
              child?.loading && child.entries.length === 0 ? (
                <div
                  className="flex h-8 items-center gap-2 text-xs text-muted"
                  style={{ paddingLeft: `${32 + depth * 14}px` }}
                >
                  <LoaderCircle className="size-3.5 animate-spin" /> Loading
                </div>
              ) : child?.error ? (
                <button
                  type="button"
                  onClick={() => onReload(entry.path)}
                  className="block w-full py-2 pr-2 text-left text-[11px] text-danger"
                  style={{ paddingLeft: `${32 + depth * 14}px` }}
                >
                  {child.error} · retry
                </button>
              ) : (
                <WorkspaceTree
                  path={entry.path}
                  depth={depth + 1}
                  directories={directories}
                  expanded={expanded}
                  selectedPath={selectedPath}
                  onToggle={onToggle}
                  onSelect={onSelect}
                  onOpenCode={onOpenCode}
                  onLoadMore={onLoadMore}
                  onReload={onReload}
                />
              )
            ) : null}
          </div>
        );
      })}
      {directory.nextCursor ? (
        <button
          type="button"
          disabled={directory.loading}
          onClick={() => onLoadMore(path, directory.nextCursor)}
          className="flex h-8 w-full items-center gap-2 pr-2 text-left text-[11px] font-medium text-accent hover:bg-accent/5 disabled:opacity-50"
          style={{ paddingLeft: `${28 + depth * 14}px` }}
        >
          {directory.loading ? <LoaderCircle className="size-3.5 animate-spin" /> : null}
          Load more
        </button>
      ) : null}
    </>
  );
}

function WorkspaceFilePreview({
  entry,
  sessionID,
  onBack,
}: {
  entry: WorkspaceEntry;
  sessionID: string;
  onBack: () => void;
}) {
  const previewFile = useMemo<PreviewFile>(() => {
    const query = new URLSearchParams({ path: entry.path });
    return {
      filename: entry.name,
      size: entry.size,
      mimeType: workspaceMimeType(entry),
      previewKind: entry.preview_kind,
      url: `/api/conversations/${encodeURIComponent(sessionID)}/workspace/file?${query}`,
    };
  }, [entry, sessionID]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex min-h-12 items-center gap-2 border-b border-border px-3">
        <button
          type="button"
          onClick={onBack}
          aria-label="Back to workspace files"
          title="Back to workspace files"
          className="inline-flex size-8 items-center justify-center rounded-full text-muted hover:bg-surface-secondary hover:text-foreground md:hidden"
        >
          <ArrowLeft className="size-4" />
        </button>
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-medium text-foreground">{entry.name}</div>
        </div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto">
        {entry.previewable ? (
          <ReadonlyFilePreview file={previewFile} renderHtml={false} fetchBinary />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted">
            <FileQuestion className="size-8" />
            <p className="text-sm font-medium text-foreground">Preview unavailable</p>
            <p className="max-w-64 text-xs">
              This file is sensitive, unsupported, too large, or not a regular file.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}

function WorkspaceLoading() {
  return (
    <div className="flex items-center gap-2 px-4 py-5 text-xs text-muted">
      <LoaderCircle className="size-4 animate-spin" /> Loading workspace
    </div>
  );
}

function WorkspaceError({
  code,
  message,
  onRetry,
}: {
  code: string;
  message: string;
  onRetry: () => void;
}) {
  return (
    <div className="flex flex-col items-center gap-3 px-5 py-10 text-center">
      <AlertTriangle className="size-7 text-warning" />
      <div>
        <p className="text-sm font-medium text-foreground">{workspaceErrorTitle(code)}</p>
        <p className="mt-1 text-xs leading-5 text-muted">{message}</p>
      </div>
      <Button size="sm" variant="outline" onPress={onRetry}>
        Retry
      </Button>
    </div>
  );
}

class WorkspaceRequestError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

async function workspaceFailure(response: Response): Promise<{ code: string; message: string }> {
  const body = (await response.json().catch(() => null)) as {
    error?: { code?: string; message?: string } | string;
  } | null;
  if (typeof body?.error === "string") return { code: "", message: body.error };
  return {
    code: body?.error?.code ?? "",
    message: body?.error?.message ?? `Workspace request failed (${response.status})`,
  };
}

function workspaceErrorMessage(err: unknown): { code: string; message: string } {
  if (err instanceof WorkspaceRequestError) {
    return { code: err.code, message: friendlyWorkspaceError(err.code, err.message) };
  }
  return { code: "", message: err instanceof Error ? err.message : String(err) };
}

function friendlyWorkspaceError(code: string, fallback: string): string {
  switch (code) {
    case "WORKSPACE_NODE_UNAVAILABLE":
      return "The node storing this workspace is unavailable. Try again after it recovers.";
    case "WORKSPACE_NOT_FOUND":
      return "This workspace has not been created yet or is no longer available.";
    case "DIRECTORY_TOO_LARGE":
      return "This directory contains too many entries to browse safely.";
    case "NOT_CONFIGURED":
      return "Workspace browsing requires the managed k3s storage mode.";
    case "TOO_MANY_REQUESTS":
      return "The storage node is busy. Wait a moment and retry.";
    default:
      return fallback;
  }
}

function workspaceErrorTitle(code: string): string {
  if (code === "WORKSPACE_NODE_UNAVAILABLE") return "Workspace node unavailable";
  if (code === "WORKSPACE_NOT_FOUND") return "Workspace not ready";
  if (code === "NOT_CONFIGURED") return "Workspace browsing unavailable";
  return "Could not open workspace";
}

function workspaceMimeType(entry: WorkspaceEntry): string {
  if (entry.preview_kind === "pdf") return "application/pdf";
  if (entry.preview_kind === "markdown") return "text/markdown";
  if (entry.preview_kind !== "image") {
    return /\.html?$/i.test(entry.name) ? "text/html" : "text/plain";
  }
  const extension = entry.name.split(".").pop()?.toLowerCase();
  const imageTypes: Record<string, string> = {
    gif: "image/gif",
    jpeg: "image/jpeg",
    jpg: "image/jpeg",
    png: "image/png",
    svg: "image/svg+xml",
    webp: "image/webp",
  };
  return imageTypes[extension ?? ""] ?? "image/*";
}

// -- Sub-page: Preview Proxy --------------------------------------------------
//
// Renders a user-launched in-sandbox dev server (Vite/Next/etc.) inside an
// iframe, reached through the same-origin Preview Proxy:
//
//   /api/preview/{sessionID}/{port}/  ->  gateway /v1/preview/...  ->  sandbox
//
// The user types the port their dev server listens on; the iframe (and the
// "open in new tab" affordance) point at the proxied root. Because the proxy
// serves the app under a subpath, apps that hard-code root-absolute asset URLs
// may need their dev server's base/publicPath set to the preview prefix (same
// caveat as AIO Sandbox's /proxy vs /absproxy).

function previewBasePath(sessionID: string, port: number): string {
  return `/api/preview/${encodeURIComponent(sessionID)}/${port}/`;
}

function PreviewPage({
  sessionID,
  active,
  setHeaderActions,
}: {
  sessionID: string;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
}) {
  // Draft is the text in the input; committed is the port actually being
  // previewed. Committing (Enter / Preview button) mounts the iframe.
  const [draftPort, setDraftPort] = useState("3000");
  const [committedPort, setCommittedPort] = useState<number | null>(null);
  // Bumping this key forces the iframe to remount (a reload that also drops any
  // in-frame history), used by the refresh control.
  const [reloadKey, setReloadKey] = useState(0);
  const [readiness, setReadiness] = useState<"idle" | "checking" | "ready" | "unavailable">("idle");

  const commit = useCallback(() => {
    const port = Number(draftPort.trim());
    if (!Number.isInteger(port) || port <= 0 || port > 65535) {
      setCommittedPort(null);
      setReadiness("idle");
      return;
    }
    setReadiness("checking");
    setCommittedPort(port);
    setReloadKey((k) => k + 1);
  }, [draftPort]);

  const src = committedPort != null ? previewBasePath(sessionID, committedPort) : "";

  useEffect(() => {
    if (!active || committedPort == null || !src) return;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      setReadiness("unavailable");
      controller.abort();
    }, 8_000);
    setReadiness("checking");
    void fetch(src, { cache: "no-store", signal: controller.signal })
      .then((response) => {
        void response.body?.cancel();
        setReadiness(response.status < 500 ? "ready" : "unavailable");
      })
      .catch(() => {
        if (!controller.signal.aborted) setReadiness("unavailable");
      })
      .finally(() => window.clearTimeout(timeout));
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [active, committedPort, reloadKey, src]);

  // Publish refresh + open-in-new-tab into the shared dock header while active.
  useEffect(() => {
    if (!active) return;
    setHeaderActions(
      <div className="flex items-center gap-1">
        <button
          type="button"
          title="Reload preview"
          aria-label="Reload preview"
          disabled={committedPort == null}
          onClick={() => setReloadKey((k) => k + 1)}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus disabled:opacity-50"
        >
          <RefreshCw className="size-4" />
        </button>
        <a
          href={src || "#"}
          target="_blank"
          rel="noreferrer"
          title="Open preview in a new tab"
          aria-label="Open preview in a new tab"
          aria-disabled={readiness !== "ready"}
          onClick={(event) => {
            if (readiness !== "ready") event.preventDefault();
          }}
          className={cn(
            "inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus",
            readiness !== "ready" && "pointer-events-none opacity-50",
          )}
        >
          <ExternalLink className="size-4" />
        </a>
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, committedPort, readiness, src, setHeaderActions]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <div className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md border border-separator bg-background px-2 py-1 text-xs text-muted focus-within:ring-1 focus-within:ring-focus">
          <Globe className="size-3.5 shrink-0 text-accent/70" />
          <span className="shrink-0 select-none">localhost:</span>
          <input
            type="text"
            inputMode="numeric"
            value={draftPort}
            onChange={(event) => setDraftPort(event.target.value.replace(/[^0-9]/g, ""))}
            onKeyDown={(event) => {
              if (event.key === "Enter") commit();
            }}
            placeholder="3000"
            aria-label="Dev server port"
            className="w-full min-w-0 bg-transparent text-foreground outline-none placeholder:text-muted/60"
          />
        </div>
        <button
          type="button"
          onClick={commit}
          className="inline-flex h-7 shrink-0 items-center rounded-md bg-accent px-3 text-xs font-medium text-accent-foreground transition-colors hover:bg-accent/90 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
        >
          Preview
        </button>
      </div>

      <div className="min-h-0 flex-1">
        {committedPort != null && readiness === "ready" ? (
          <iframe
            key={reloadKey}
            src={src}
            title={`Preview of port ${committedPort}`}
            className="h-full w-full border-0 bg-white"
            sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-modals"
          />
        ) : committedPort != null && readiness === "checking" ? (
          <div className="flex h-full min-h-0 flex-col items-center justify-center px-6 text-center">
            <LoaderCircle className="mb-3 size-7 animate-spin text-accent/70" />
            <p className="text-sm font-medium text-foreground">Connecting to preview</p>
            <p className="mt-1 text-xs text-muted">Checking port {committedPort} in the sandbox…</p>
          </div>
        ) : committedPort != null && readiness === "unavailable" ? (
          <div className="flex h-full min-h-0 flex-col items-center justify-center px-6 text-center">
            <AlertTriangle className="mb-3 size-8 text-warning" />
            <p className="text-sm font-medium text-foreground">Preview server unavailable</p>
            <p className="mt-1 max-w-sm text-xs leading-5 text-muted">
              No server is reachable on port {committedPort}. Ask the Agent to start a managed
              preview server, then retry.
            </p>
            <Button
              className="mt-4"
              size="sm"
              variant="outline"
              onPress={() => setReloadKey((key) => key + 1)}
            >
              <RefreshCw className="size-3.5" />
              Retry
            </Button>
          </div>
        ) : (
          <div className="flex h-full min-h-0 flex-col items-center justify-center px-6 text-center">
            <Globe className="mb-3 size-8 text-muted/50" />
            <p className="text-sm font-medium text-foreground">Preview a dev server</p>
            <p className="mt-1 max-w-xs text-xs text-muted">
              Enter the port your in-sandbox dev server listens on (e.g. 3000), then press Preview
              to load it here.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}

// -- Code (resident code-server editor) --------------------------------------
//
// Each dynamic Code tab points the resident editor at one /workspace directory
// through code-server's `folder` query. The editor is WebSocket-driven; those
// upgrades are carried by the custom web server (apps/web/server.mjs), not the
// /api/preview route handler.

const CODE_SERVER_PROBE_TIMEOUT_MS = 8000;

type CodeEditorReadiness = "not-started" | "checking" | "waiting" | "ready" | "reclaimed" | "error";

type CodeEditorProbeResult = {
  kind: CodeEditorReadiness;
  retry: boolean;
};

function CodePage({
  sessionID,
  workspacePath,
  active,
  setHeaderActions,
}: {
  sessionID: string;
  workspacePath: string;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
}) {
  const hasMessages = useThread((thread) => thread.messages.length > 0);
  const isRunning = useThread((thread) => thread.isRunning);
  // Persisted Environment snapshots can be stale after an interrupted run;
  // only the live thread state proves that Acquire may currently be in flight.
  const environmentPreparing = isRunning;
  // Bumping this key remounts the iframe (a hard reload of the editor).
  const [reloadKey, setReloadKey] = useState(0);
  const [probeKey, setProbeKey] = useState(0);
  const [readiness, setReadiness] = useState<CodeEditorReadiness>(() =>
    hasMessages ? "checking" : "not-started",
  );
  // Hidden Code tabs keep their iframe mounted. Avoid probing again when the
  // user returns to one, because replacing "ready" with "checking" would
  // destroy that iframe and discard unsaved Workbench state.
  const editorReadyRef = useRef(false);
  const waitStartedAtRef = useRef<number | null>(null);
  const src = useMemo(
    () => buildCodeEditorURL(sessionID, workspacePath),
    [sessionID, workspacePath],
  );
  const folder = workspacePath ? `/workspace/${workspacePath}` : "/workspace";

  const retryProbe = useCallback(() => {
    waitStartedAtRef.current = environmentPreparing ? Date.now() : null;
    setProbeKey((key) => key + 1);
  }, [environmentPreparing]);

  useEffect(() => {
    if (!active) return;
    if (editorReadyRef.current) return;
    if (environmentPreparing) {
      waitStartedAtRef.current ??= Date.now();
    } else {
      waitStartedAtRef.current = null;
    }
    const initial = classifyCodeEditorProbe({
      hasMessages,
      environmentPreparing,
    }) as CodeEditorProbeResult;
    setReadiness(initial.kind);
    if (!hasMessages) return;

    let disposed = false;
    let retryTimer: number | undefined;
    let requestController: AbortController | undefined;

    const probe = async (attempt: number) => {
      requestController = new AbortController();
      const timeout = window.setTimeout(
        () => requestController?.abort(),
        CODE_SERVER_PROBE_TIMEOUT_MS,
      );
      let result: CodeEditorProbeResult;
      try {
        const responseStatus = await probeCodeEditorStatus(src, requestController.signal);
        result = classifyCodeEditorProbe({
          hasMessages: true,
          environmentPreparing,
          responseStatus,
        }) as CodeEditorProbeResult;
      } catch {
        if (disposed) return;
        result = classifyCodeEditorProbe({
          hasMessages: true,
          environmentPreparing,
          networkFailed: true,
        }) as CodeEditorProbeResult;
      } finally {
        window.clearTimeout(timeout);
      }

      if (disposed) return;
      if (result.retry) {
        editorReadyRef.current = false;
        const startedAt = waitStartedAtRef.current ?? Date.now();
        waitStartedAtRef.current = startedAt;
        if (codeEditorWaitExpired(startedAt)) {
          setReadiness("error");
          return;
        }
        setReadiness(result.kind);
        retryTimer = window.setTimeout(
          () => void probe(attempt + 1),
          codeEditorRetryDelay(attempt),
        );
        return;
      }
      editorReadyRef.current = result.kind === "ready";
      waitStartedAtRef.current = null;
      setReadiness(result.kind);
    };

    void probe(0);
    return () => {
      disposed = true;
      requestController?.abort();
      if (retryTimer != null) window.clearTimeout(retryTimer);
    };
  }, [active, environmentPreparing, hasMessages, probeKey, src]);

  useEffect(() => {
    if (!active) return;
    if (readiness !== "ready") {
      setHeaderActions(null);
      return () => setHeaderActions(null);
    }
    setHeaderActions(
      <div className="flex items-center gap-1">
        <button
          type="button"
          title="Reload editor"
          aria-label="Reload editor"
          onClick={() => setReloadKey((k) => k + 1)}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
        >
          <RefreshCw className="size-4" />
        </button>
        <a
          href={src}
          target="_blank"
          rel="noreferrer"
          title="Open editor in a new tab"
          aria-label="Open editor in a new tab"
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
        >
          <ExternalLink className="size-4" />
        </a>
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, readiness, src, setHeaderActions]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-surface">
      <div className="min-h-0 flex-1">
        {readiness === "ready" ? (
          <iframe
            key={reloadKey}
            src={src}
            title={`Code editor for ${folder}`}
            className="h-full w-full border-0 bg-white"
            sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-modals allow-downloads"
          />
        ) : (
          <CodeEditorPlaceholder
            readiness={readiness}
            onRetry={readiness === "error" ? retryProbe : undefined}
          />
        )}
      </div>
    </div>
  );
}

function CodeEditorPlaceholder({
  readiness,
  onRetry,
}: {
  readiness: Exclude<CodeEditorReadiness, "ready">;
  onRetry?: () => void;
}) {
  const content = {
    "not-started": {
      title: "Environment not started",
      description:
        "Send your first message to prepare the sandbox. Code will be available when the environment is ready.",
    },
    checking: {
      title: "Checking environment",
      description: "Checking whether the Code editor is available for this conversation.",
    },
    waiting: {
      title: "Preparing environment",
      description: "Code will open automatically when the sandbox is ready.",
    },
    reclaimed: {
      title: "Sandbox has been reclaimed",
      description: "Continue this conversation to restore the sandbox and reopen Code.",
    },
    error: {
      title: "Code editor unavailable",
      description: "The sandbox could not be reached. Check the environment and try again.",
    },
  }[readiness];
  const loading = readiness === "checking" || readiness === "waiting";
  const Icon = loading ? LoaderCircle : readiness === "error" ? AlertTriangle : Code2;

  return (
    <div className="flex h-full min-h-0 flex-col items-center justify-center px-6 text-center">
      <div className="flex size-11 items-center justify-center rounded-xl bg-surface-secondary text-muted">
        <Icon className={cn("size-5", loading && "animate-spin")} />
      </div>
      <p className="mt-4 text-sm font-medium text-foreground">{content.title}</p>
      <p className="mt-1 max-w-sm text-xs leading-5 text-muted">{content.description}</p>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="mt-4 inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-3 text-xs font-medium text-foreground transition-colors hover:bg-surface-secondary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
        >
          <RefreshCw className="size-3.5" />
          Try again
        </button>
      ) : null}
    </div>
  );
}
