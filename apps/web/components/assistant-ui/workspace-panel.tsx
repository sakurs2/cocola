"use client";

import { useThread } from "@assistant-ui/react";
import { ReadonlyFilePreview, type PreviewFile } from "@/components/assistant-ui/file-preview";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
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
import { MaterialFileIcon } from "@/lib/material-file-icons";
import {
  AlertTriangle,
  ArrowLeft,
  ChevronRight,
  Code2,
  File,
  FileCode2,
  FileQuestion,
  Folder,
  FolderOpen,
  Globe,
  LoaderCircle,
  Plus,
  RefreshCw,
  ExternalLink,
  X,
  type LucideIcon,
} from "lucide-react";
import {
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
// "+" menu to add and switch to another sub-page. Workspace files and Preview
// are registered base pages. Code pages are created dynamically from directory
// actions, one stable tab per workspace path.

type DockPageContext = {
  sessionID: string;
  active: boolean;
  // Lets a page publish header controls (e.g. refresh) into the shared dock
  // header, so no sub-page needs its own toolbar row.
  setHeaderActions: (node: ReactNode) => void;
  openCodeFolder: (workspacePath: string) => void;
};

type DockPage = {
  id: string;
  label: string;
  title?: string;
  icon: LucideIcon;
  render: (context: DockPageContext) => ReactNode;
};

const BASE_DOCK_PAGES: DockPage[] = [
  {
    id: "files",
    label: "Workspace files",
    icon: FolderOpen,
    render: ({ sessionID, active, setHeaderActions, openCodeFolder }) => (
      <WorkspaceFilesPage
        sessionID={sessionID}
        active={active}
        setHeaderActions={setHeaderActions}
        onOpenCode={openCodeFolder}
      />
    ),
  },
  {
    id: "preview",
    label: "Preview",
    icon: Globe,
    render: ({ sessionID, active, setHeaderActions }) => (
      <PreviewPage sessionID={sessionID} active={active} setHeaderActions={setHeaderActions} />
    ),
  },
];

function createCodePage(workspacePath: string): DockPage {
  const normalizedPath = normalizeCodeEditorWorkspacePath(workspacePath);
  const folder = normalizedPath ? `/workspace/${normalizedPath}` : "/workspace";
  const label = normalizedPath.split("/").pop() || "Workspace";
  return {
    id: codeEditorTabID(normalizedPath),
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

export function WorkspaceDock({ sessionID, onClose }: { sessionID: string; onClose: () => void }) {
  // Opening the workspace dock must not contact code-server. Code tabs only
  // exist after a directory action explicitly creates one.
  const [openPages, setOpenPages] = useState<DockPage[]>([]);
  const [activePageId, setActivePageId] = useState<string>("");
  // The active page publishes its header controls here; keyed by page id so a
  // backgrounded page can never leak its actions into the header.
  const [headerActions, setHeaderActions] = useState<Record<string, ReactNode>>({});

  const addablePages = useMemo(
    () => BASE_DOCK_PAGES.filter((page) => !openPages.some((open) => open.id === page.id)),
    [openPages],
  );

  const openPage = useCallback((id: string) => {
    const page = BASE_DOCK_PAGES.find((candidate) => candidate.id === id);
    if (!page) return;
    setOpenPages((current) =>
      current.some((candidate) => candidate.id === id) ? current : [...current, page],
    );
    setActivePageId(id);
  }, []);

  const openCodeFolder = useCallback((workspacePath: string) => {
    const page = createCodePage(workspacePath);
    setOpenPages((current) =>
      current.some((candidate) => candidate.id === page.id) ? current : [...current, page],
    );
    setActivePageId(page.id);
  }, []);

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

  const closePage = useCallback(
    (id: string) => {
      setOpenPages((current) => {
        const next = current.filter((page) => page.id !== id);
        // Closing the last tab returns to the launcher; the dock stays open (the
        // header close button collapses the whole dock).
        setActivePageId((active) => (active === id ? (next[next.length - 1]?.id ?? "") : active));
        return next;
      });
      publishHeaderActions(id, null);
    },
    [publishHeaderActions],
  );

  const activePage = openPages.find((page) => page.id === activePageId) ?? openPages[0];
  const hasOpenPages = openPages.length > 0;

  return (
    <div className="flex h-full min-h-0 flex-col bg-card">
      <header className="flex min-h-11 items-center gap-1 border-b border-border pl-2 pr-1">
        <div role="tablist" className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {openPages.map((page) => {
            const Icon = page.icon;
            const active = page.id === activePage?.id;
            return (
              <div
                key={page.id}
                title={page.title}
                className={cn(
                  "group flex h-8 shrink-0 items-center gap-1.5 rounded-md pl-2.5 pr-1.5 text-xs transition-colors",
                  active
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                )}
              >
                <button
                  type="button"
                  role="tab"
                  aria-selected={active}
                  onClick={() => setActivePageId(page.id)}
                  className="flex items-center gap-1.5 focus-visible:outline-none"
                >
                  <Icon
                    className={cn("size-4 shrink-0", active ? "text-primary" : "text-primary/70")}
                  />
                  <span className="whitespace-nowrap font-medium">{page.label}</span>
                </button>
                <button
                  type="button"
                  aria-label={`Close ${page.label}`}
                  title={`Close ${page.label}`}
                  onClick={() => closePage(page.id)}
                  className="inline-flex size-4 items-center justify-center rounded-full text-muted-foreground/70 opacity-0 transition hover:bg-background hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
                >
                  <X className="size-3" />
                </button>
              </div>
            );
          })}

          {hasOpenPages ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  title="Add a panel"
                  aria-label="Add a panel"
                  className="inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <Plus className="size-4" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="cocola-user-ui min-w-44 bg-popover">
                {addablePages.length > 0 ? (
                  addablePages.map((page) => {
                    const Icon = page.icon;
                    return (
                      <DropdownMenuItem key={page.id} onSelect={() => openPage(page.id)}>
                        <Icon className="size-4 text-primary/80" />
                        <span>{page.label}</span>
                      </DropdownMenuItem>
                    );
                  })
                ) : (
                  <div className="px-2 py-1.5 text-xs text-muted-foreground">empty</div>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>

        {activePage ? (headerActions[activePage.id] ?? null) : null}

        <button
          type="button"
          title="Close workspace"
          aria-label="Close workspace"
          onClick={onClose}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <X className="size-4" />
        </button>
      </header>

      <div className="min-h-0 flex-1">
        {hasOpenPages ? null : <WorkspaceLauncher onOpen={openPage} />}
        {openPages.map((page) => {
          const isActive = page.id === activePage?.id;
          return (
            <DockPagePanel
              key={page.id}
              page={page}
              sessionID={sessionID}
              active={isActive}
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
  openCodeFolder,
  publishHeaderActions,
}: {
  page: DockPage;
  sessionID: string;
  active: boolean;
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
        setHeaderActions,
        openCodeFolder,
      })}
    </div>
  );
}

// Empty-state launcher: lists the available panels centered in the dock so the
// user can pick one to open (mirrors a command-menu style row list).
function WorkspaceLauncher({ onOpen }: { onOpen: (id: string) => void }) {
  return (
    <div className="flex h-full min-h-0 flex-col items-center justify-center px-6">
      <div className="w-full max-w-sm">
        <p className="mb-3 px-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Panels
        </p>
        <div className="flex flex-col">
          {BASE_DOCK_PAGES.map((page) => {
            const Icon = page.icon;
            return (
              <button
                key={page.id}
                type="button"
                onClick={() => onOpen(page.id)}
                className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              >
                <Icon className="size-5 shrink-0 text-primary/80" />
                <span className="font-medium">{page.label}</span>
              </button>
            );
          })}
        </div>
      </div>
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
}: {
  sessionID: string;
  active: boolean;
  setHeaderActions: (node: ReactNode) => void;
  onOpenCode: (workspacePath: string) => void;
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
    void loadDirectory("");
  }, [loadDirectory]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    setDirectories({});
    setExpanded(new Set());
    setSelected(null);
    await loadDirectory("");
    setRefreshing(false);
  }, [loadDirectory]);

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

  const root = directories[""];
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
          onClick={() => onOpenCode("")}
          className="size-8 rounded-full text-muted-foreground"
        >
          <Code2 className="size-4" />
        </TooltipIconButton>
        <button
          type="button"
          title="Refresh workspace"
          aria-label="Refresh workspace"
          disabled={refreshing}
          onClick={() => void refresh()}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
        >
          <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
        </button>
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, onOpenCode, refreshing, refresh, rootReady, setHeaderActions]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-card">
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
                onRetry={() => void loadDirectory("")}
              />
            ) : root.entries.length === 0 ? (
              <div className="flex flex-col items-center gap-2 px-5 py-12 text-center">
                <Folder className="size-7 text-muted-foreground/70" />
                <div className="text-sm font-medium text-foreground">Workspace is empty</div>
                <div className="text-xs text-muted-foreground">
                  Files created by the agent will appear here after refresh.
                </div>
              </div>
            ) : (
              <WorkspaceTree
                path=""
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
              "absolute inset-y-0 left-1/2 w-3 -translate-x-1/2 bg-transparent transition-colors group-hover:bg-primary/10 group-focus-visible:bg-primary/10",
              resizingTree && "bg-primary/10",
            )}
          />
          <span
            className={cn(
              "absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-border transition-colors group-hover:bg-primary/80 group-focus-visible:bg-primary/80",
              resizingTree && "bg-primary",
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
            <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
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
                  "group flex h-8 w-full items-center gap-1.5 border-l-2 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring",
                  isDirectory ? "pr-9" : "pr-2",
                  selectedPath === entry.path
                    ? "border-l-primary bg-primary/10 text-foreground"
                    : "border-l-transparent text-muted-foreground hover:bg-muted/70 hover:text-foreground",
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
                    <FolderOpen className="size-4 shrink-0 text-primary/80" />
                  ) : (
                    <Folder className="size-4 shrink-0 text-primary/70" />
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
                  className="absolute right-1 top-1/2 size-6 -translate-y-1/2 text-muted-foreground opacity-100 transition-opacity hover:text-foreground focus-visible:opacity-100 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/tree-row:opacity-100 [@media(hover:hover)]:focus-visible:opacity-100"
                >
                  <Code2 className="size-3.5" />
                </TooltipIconButton>
              ) : null}
            </div>
            {isExpanded ? (
              child?.loading && child.entries.length === 0 ? (
                <div
                  className="flex h-8 items-center gap-2 text-xs text-muted-foreground"
                  style={{ paddingLeft: `${32 + depth * 14}px` }}
                >
                  <LoaderCircle className="size-3.5 animate-spin" /> Loading
                </div>
              ) : child?.error ? (
                <button
                  type="button"
                  onClick={() => onReload(entry.path)}
                  className="block w-full py-2 pr-2 text-left text-[11px] text-destructive"
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
          className="flex h-8 w-full items-center gap-2 pr-2 text-left text-[11px] font-medium text-primary hover:bg-primary/5 disabled:opacity-50"
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
          className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground md:hidden"
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
          <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
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
    <div className="flex items-center gap-2 px-4 py-5 text-xs text-muted-foreground">
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
      <AlertTriangle className="size-7 text-amber-500" />
      <div>
        <p className="text-sm font-medium text-foreground">{workspaceErrorTitle(code)}</p>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">{message}</p>
      </div>
      <button
        type="button"
        onClick={onRetry}
        className="rounded-lg border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      >
        Retry
      </button>
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

  const commit = useCallback(() => {
    const port = Number(draftPort.trim());
    if (!Number.isInteger(port) || port <= 0 || port > 65535) {
      setCommittedPort(null);
      return;
    }
    setCommittedPort(port);
    setReloadKey((k) => k + 1);
  }, [draftPort]);

  const src = committedPort != null ? previewBasePath(sessionID, committedPort) : "";

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
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
        >
          <RefreshCw className="size-4" />
        </button>
        <a
          href={src || "#"}
          target="_blank"
          rel="noreferrer"
          title="Open preview in a new tab"
          aria-label="Open preview in a new tab"
          aria-disabled={committedPort == null}
          onClick={(event) => {
            if (committedPort == null) event.preventDefault();
          }}
          className={cn(
            "inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
            committedPort == null && "pointer-events-none opacity-50",
          )}
        >
          <ExternalLink className="size-4" />
        </a>
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, committedPort, src, setHeaderActions]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-card">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <div className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1 text-xs text-muted-foreground focus-within:ring-1 focus-within:ring-ring">
          <Globe className="size-3.5 shrink-0 text-primary/70" />
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
            className="w-full min-w-0 bg-transparent text-foreground outline-none placeholder:text-muted-foreground/60"
          />
        </div>
        <button
          type="button"
          onClick={commit}
          className="inline-flex h-7 shrink-0 items-center rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          Preview
        </button>
      </div>

      <div className="min-h-0 flex-1">
        {committedPort != null ? (
          <iframe
            key={reloadKey}
            src={src}
            title={`Preview of port ${committedPort}`}
            className="h-full w-full border-0 bg-white"
            sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-modals"
          />
        ) : (
          <div className="flex h-full min-h-0 flex-col items-center justify-center px-6 text-center">
            <Globe className="mb-3 size-8 text-muted-foreground/50" />
            <p className="text-sm font-medium text-foreground">Preview a dev server</p>
            <p className="mt-1 max-w-xs text-xs text-muted-foreground">
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
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <RefreshCw className="size-4" />
        </button>
        <a
          href={src}
          target="_blank"
          rel="noreferrer"
          title="Open editor in a new tab"
          aria-label="Open editor in a new tab"
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <ExternalLink className="size-4" />
        </a>
      </div>,
    );
    return () => setHeaderActions(null);
  }, [active, readiness, src, setHeaderActions]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-card">
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
      <div className="flex size-11 items-center justify-center rounded-xl bg-muted text-muted-foreground">
        <Icon className={cn("size-5", loading && "animate-spin")} />
      </div>
      <p className="mt-4 text-sm font-medium text-foreground">{content.title}</p>
      <p className="mt-1 max-w-sm text-xs leading-5 text-muted-foreground">{content.description}</p>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="mt-4 inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-3 text-xs font-medium text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <RefreshCw className="size-3.5" />
          Try again
        </button>
      ) : null}
    </div>
  );
}
