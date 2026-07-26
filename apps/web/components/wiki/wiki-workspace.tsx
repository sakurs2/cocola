"use client";

import {
  ArrowLeft,
  BookOpenText,
  ChevronRight,
  Download,
  File,
  FileCode2,
  FileSpreadsheet,
  FileText,
  Folder,
  FolderOpen,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  Upload,
} from "lucide-react";
import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { ReadonlyFilePreview } from "@/components/assistant-ui/file-preview";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
import { useWorkspaceUnsavedChanges } from "@/components/assistant-ui/workspace-unsaved-changes";
import { ActionConfirmDialog, TextInputDialog } from "@/components/ui/action-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { WikiMarkdownEditor } from "@/components/wiki/wiki-markdown-editor";

type WikiNode = {
  id: string;
  parent_id?: string;
  kind: "folder" | "file";
  name: string;
  extension?: string;
  mime_type?: string;
  current_version_id?: string;
  revision?: number;
  size_bytes?: number;
  logical_path?: string;
  created_at: string;
  updated_at: string;
};

type SaveState = "loading" | "load-error" | "saved" | "dirty" | "saving" | "conflict" | "error";
type WikiNameDialogState = {
  kind: "folder" | "markdown" | "rename";
  node?: WikiNode;
};

const DEFAULT_MAX_FILE_BYTES = 20 * 1024 * 1024;
const DEFAULT_SIDEBAR_WIDTH = 304;
const MIN_SIDEBAR_WIDTH = 240;
const MAX_SIDEBAR_WIDTH = 520;
const OFFICE_EXTENSIONS = new Set([".docx", ".xlsx", ".pptx"]);
const ACCEPTED_FILES = ".md,.txt,.csv,.json,.yaml,.yml,.pdf,.docx,.xlsx,.pptx,application/pdf";

async function responseError(response: Response) {
  const body = (await response.json().catch(() => null)) as {
    error?: { message?: string };
  } | null;
  return body?.error?.message || `Request failed (${response.status})`;
}

function sortWikiNodes(nodes: WikiNode[]): WikiNode[] {
  return [...nodes].sort(
    (a, b) =>
      Number(b.kind === "folder") - Number(a.kind === "folder") ||
      a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
  );
}

function clampSidebarWidth(width: number, viewportWidth?: number): number {
  const responsiveMax =
    typeof viewportWidth === "number"
      ? Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, viewportWidth - 360))
      : MAX_SIDEBAR_WIDTH;
  return Math.min(responsiveMax, Math.max(MIN_SIDEBAR_WIDTH, width));
}

function formatBytes(bytes = 0) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function fileIcon(node: WikiNode) {
  if (node.kind === "folder") return Folder;
  if (node.extension === ".md") return FileText;
  if (node.extension === ".xlsx" || node.extension === ".csv") return FileSpreadsheet;
  if ([".json", ".yaml", ".yml"].includes(node.extension ?? "")) return FileCode2;
  return File;
}

function previewKind(node: WikiNode): "markdown" | "code" | "pdf" | undefined {
  if (node.extension === ".md") return "markdown";
  if (node.extension === ".pdf") return "pdf";
  if ([".json", ".yaml", ".yml", ".csv"].includes(node.extension ?? "")) return "code";
  return undefined;
}

export function WikiWorkspace() {
  const [nodes, setNodes] = useState<WikiNode[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [currentFolderID, setCurrentFolderID] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [sidebarWidth, setSidebarWidth] = useState(DEFAULT_SIDEBAR_WIDTH);
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [maxFileBytes, setMaxFileBytes] = useState(DEFAULT_MAX_FILE_BYTES);
  const [unsavedFileID, setUnsavedFileID] = useState("");
  const [nameDialog, setNameDialog] = useState<WikiNameDialogState | null>(null);
  const [nameDialogError, setNameDialogError] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<WikiNode | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [discardDialogOpen, setDiscardDialogOpen] = useState(false);
  const pendingDiscardAction = useRef<(() => void) | null>(null);
  const uploadRef = useRef<HTMLInputElement>(null);
  const sidebarResize = useRef<{
    pointerID: number;
    startX: number;
    startWidth: number;
  } | null>(null);
  const previousBodyStyle = useRef<{ cursor: string; userSelect: string } | null>(null);
  const { setDirty } = useWorkspaceUnsavedChanges();

  const selected = nodes.find((node) => node.id === selectedID) ?? null;
  const currentFolder =
    nodes.find((node) => node.id === currentFolderID && node.kind === "folder") ?? null;
  const activeFolderID = currentFolderID;
  const visibleNodes = useMemo(
    () => sortWikiNodes(nodes.filter((node) => (node.parent_id ?? "") === currentFolderID)),
    [currentFolderID, nodes],
  );
  const folderTrail = useMemo(() => {
    const byID = new Map(nodes.map((node) => [node.id, node]));
    const trail: WikiNode[] = [];
    const visited = new Set<string>();
    let folderID = currentFolderID;
    while (folderID && !visited.has(folderID)) {
      visited.add(folderID);
      const folder = byID.get(folderID);
      if (!folder || folder.kind !== "folder") break;
      trail.push(folder);
      folderID = folder.parent_id ?? "";
    }
    return trail.reverse();
  }, [currentFolderID, nodes]);
  const filteredNodes = useMemo(() => {
    const value = query.trim().toLowerCase();
    if (!value) return [];
    return nodes
      .filter(
        (node) =>
          node.name.toLowerCase().includes(value) ||
          (node.logical_path ?? "").toLowerCase().includes(value),
      )
      .slice(0, 30);
  }, [nodes, query]);

  const loadTree = useCallback(async (preferredID?: string) => {
    setError("");
    try {
      const response = await fetch("/api/wiki/tree", { cache: "no-store" });
      if (!response.ok) throw new Error(await responseError(response));
      const body = (await response.json()) as { nodes?: WikiNode[] };
      const next = Array.isArray(body.nodes) ? body.nodes : [];
      setNodes(next);
      setCurrentFolderID((current) =>
        next.some((node) => node.id === current && node.kind === "folder") ? current : "",
      );
      setSelectedID((current) => {
        const target = preferredID ?? current;
        return next.some((node) => node.id === target) ? target : "";
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadTree();
    void fetch("/api/product-config", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) return;
        const config = (await response.json()) as { wiki?: { max_file_bytes?: number } };
        if (typeof config.wiki?.max_file_bytes === "number" && config.wiki.max_file_bytes > 0) {
          setMaxFileBytes(config.wiki.max_file_bytes);
        }
      })
      .catch(() => {});
  }, [loadTree]);

  const changed = useCallback(() => {
    window.dispatchEvent(new Event("cocola:wiki-changed"));
  }, []);

  const runAfterDiscardCheck = useCallback(
    (action: () => void, nextNodeID = "") => {
      if (!unsavedFileID || unsavedFileID === nextNodeID) {
        action();
        return;
      }
      pendingDiscardAction.current = action;
      setDiscardDialogOpen(true);
    },
    [unsavedFileID],
  );

  const handleUnsavedChange = useCallback((nodeID: string, unsaved: boolean) => {
    setUnsavedFileID((current) => {
      if (unsaved) return nodeID;
      return current === nodeID ? "" : current;
    });
  }, []);

  useEffect(() => {
    if (!unsavedFileID) return;
    const warnBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [unsavedFileID]);

  useEffect(() => {
    setDirty(Boolean(unsavedFileID));
  }, [setDirty, unsavedFileID]);

  useEffect(() => () => setDirty(false), [setDirty]);

  const createFolder = () => {
    runAfterDiscardCheck(() => {
      setNameDialogError("");
      setNameDialog({ kind: "folder" });
    });
  };

  const createMarkdown = () => {
    runAfterDiscardCheck(() => {
      setNameDialogError("");
      setNameDialog({ kind: "markdown" });
    });
  };

  const submitNameDialog = async (name: string) => {
    if (!nameDialog) return;
    setBusy(true);
    setNameDialogError("");
    try {
      let response: Response;
      let preferredID = nameDialog.node?.id ?? "";
      if (nameDialog.kind === "folder") {
        response = await fetch("/api/wiki/folders", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ parent_id: activeFolderID, name }),
        });
      } else if (nameDialog.kind === "markdown") {
        response = await fetch("/api/wiki/markdown", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            parent_id: activeFolderID,
            name,
            content: `# ${name.replace(/\.md$/i, "")}\n\n`,
          }),
        });
      } else {
        if (!nameDialog.node || name === nameDialog.node.name) {
          setNameDialog(null);
          return;
        }
        response = await fetch(`/api/wiki/nodes/${encodeURIComponent(nameDialog.node.id)}`, {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ name }),
        });
      }
      if (!response.ok) throw new Error(await responseError(response));
      const updated = (await response.json()) as WikiNode;
      preferredID = updated.id || preferredID;
      if (nameDialog.kind === "folder") {
        setCurrentFolderID(updated.id);
      }
      await loadTree(preferredID);
      changed();
      setNameDialog(null);
    } catch (cause) {
      setNameDialogError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (files.length === 0) return;
    const oversized = files.find((file) => file.size > maxFileBytes);
    if (oversized) {
      setError(`${oversized.name} exceeds the ${formatBytes(maxFileBytes)} file limit.`);
      return;
    }
    setBusy(true);
    setError("");
    try {
      let lastID = "";
      for (const file of files) {
        const form = new FormData();
        form.set("parent_id", activeFolderID);
        form.set("file", file);
        const response = await fetch("/api/wiki/uploads", { method: "POST", body: form });
        if (!response.ok) throw new Error(`${file.name}: ${await responseError(response)}`);
        lastID = ((await response.json()) as WikiNode).id;
      }
      await loadTree(lastID);
      changed();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const requestUpload = () => {
    runAfterDiscardCheck(() => uploadRef.current?.click());
  };

  const rename = (node: WikiNode) => {
    setNameDialogError("");
    setNameDialog({ kind: "rename", node });
  };

  const remove = (node: WikiNode) => {
    setDeleteError(null);
    setDeleteTarget(node);
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setBusy(true);
    setDeleteError(null);
    try {
      const response = await fetch(`/api/wiki/nodes/${encodeURIComponent(deleteTarget.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await responseError(response));
      if (deleteTarget.id === currentFolderID) {
        setCurrentFolderID(deleteTarget.parent_id ?? "");
      }
      await loadTree();
      changed();
      setDeleteTarget(null);
    } catch (cause) {
      setDeleteError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const performMove = async (nodeID: string, parentID: string) => {
    setBusy(true);
    try {
      const response = await fetch(`/api/wiki/nodes/${encodeURIComponent(nodeID)}/move`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ parent_id: parentID }),
      });
      if (!response.ok) throw new Error(await responseError(response));
      await loadTree(nodeID);
      changed();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const move = async (nodeID: string, parentID: string) => {
    if (!nodeID || nodeID === parentID) return;
    runAfterDiscardCheck(() => void performMove(nodeID, parentID), nodeID);
  };

  const navigateToFolder = (folderID: string) => {
    const folder = folderID
      ? nodes.find((node) => node.id === folderID && node.kind === "folder")
      : null;
    if (folderID && !folder) return;
    runAfterDiscardCheck(() => {
      setCurrentFolderID(folderID);
      setSelectedID(folderID);
      setQuery("");
    }, folderID);
  };

  const selectNode = (node: WikiNode) => {
    runAfterDiscardCheck(() => {
      setSelectedID(node.id);
      setQuery("");
      if (node.kind === "folder") {
        setCurrentFolderID(node.id);
      } else {
        setCurrentFolderID(node.parent_id ?? "");
      }
    }, node.id);
  };

  const finishSidebarResize = useCallback(() => {
    sidebarResize.current = null;
    setResizingSidebar(false);
    if (previousBodyStyle.current) {
      document.body.style.cursor = previousBodyStyle.current.cursor;
      document.body.style.userSelect = previousBodyStyle.current.userSelect;
      previousBodyStyle.current = null;
    }
  }, []);

  const startSidebarResize = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    sidebarResize.current = {
      pointerID: event.pointerId,
      startX: event.clientX,
      startWidth: sidebarWidth,
    };
    previousBodyStyle.current = {
      cursor: document.body.style.cursor,
      userSelect: document.body.style.userSelect,
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    setResizingSidebar(true);
  };

  const resizeSidebar = (event: PointerEvent<HTMLDivElement>) => {
    const resize = sidebarResize.current;
    if (!resize || resize.pointerID !== event.pointerId) return;
    setSidebarWidth(
      clampSidebarWidth(resize.startWidth + event.clientX - resize.startX, window.innerWidth),
    );
  };

  const resizeSidebarWithKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const delta = event.key === "ArrowLeft" ? -16 : 16;
    setSidebarWidth((width) => clampSidebarWidth(width + delta, window.innerWidth));
  };

  useEffect(() => finishSidebarResize, [finishSidebarResize]);

  return (
    <div
      className={cn(
        "relative flex h-full min-h-0 bg-[#fff] text-[#142033]",
        resizingSidebar && "cursor-col-resize select-none",
      )}
    >
      <div
        className="pointer-events-none absolute inset-y-0 left-0 w-1 bg-gradient-to-b from-[#2563EB] via-[#7C3AED] to-[#10B981]"
        aria-hidden="true"
      />
      <aside
        className="flex shrink-0 flex-col border-r border-slate-200 bg-[#F8FAFD]"
        style={{ width: sidebarWidth }}
      >
        <header className="border-b border-slate-200 px-4 pb-3 pt-5">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <span className="grid size-8 place-items-center rounded-xl bg-gradient-to-br from-blue-600 to-violet-600 text-white shadow-sm">
                  <BookOpenText className="size-4" />
                </span>
                <h1 className="text-lg font-semibold tracking-tight">Wiki</h1>
              </div>
              <p className="mt-2 text-xs text-slate-500">
                Your working knowledge, ready for Agents.
              </p>
            </div>
            <button
              type="button"
              onClick={() => void loadTree()}
              aria-label="Refresh Wiki"
              className="grid size-8 place-items-center rounded-lg text-slate-500 hover:bg-white hover:text-slate-900"
            >
              <RefreshCw className={cn("size-4", loading && "animate-spin")} />
            </button>
          </div>
          <div className="relative mt-4">
            <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-slate-400" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search this Wiki"
              className="h-9 w-full rounded-xl border border-slate-200 bg-white pl-9 pr-3 text-sm outline-none transition focus:border-blue-400 focus:ring-2 focus:ring-blue-100"
            />
          </div>
          <div className="mt-3 flex min-w-0 items-center gap-1.5">
            <button
              type="button"
              disabled={!currentFolder}
              onClick={() => navigateToFolder(currentFolder?.parent_id ?? "")}
              aria-label="Go to parent folder"
              className="grid size-7 shrink-0 place-items-center rounded-lg text-slate-500 transition hover:bg-white hover:text-slate-900 disabled:cursor-default disabled:opacity-35"
            >
              <ArrowLeft className="size-3.5" />
            </button>
            <nav
              aria-label="Wiki folder path"
              className="flex min-w-0 flex-1 items-center overflow-hidden text-xs"
            >
              <button
                type="button"
                onClick={() => navigateToFolder("")}
                className={cn(
                  "shrink-0 rounded-md px-1.5 py-1 font-medium transition hover:bg-white hover:text-slate-950",
                  currentFolderID ? "text-slate-500" : "text-slate-950",
                )}
              >
                All files
              </button>
              {folderTrail.map((folder, index) => (
                <Fragment key={folder.id}>
                  <ChevronRight className="size-3 shrink-0 text-slate-300" />
                  <button
                    type="button"
                    onClick={() => navigateToFolder(folder.id)}
                    title={folder.logical_path || folder.name}
                    className={cn(
                      "min-w-0 truncate rounded-md px-1.5 py-1 transition hover:bg-white hover:text-slate-950",
                      index === folderTrail.length - 1
                        ? "font-semibold text-slate-950"
                        : "text-slate-500",
                    )}
                  >
                    {folder.name}
                  </button>
                </Fragment>
              ))}
            </nav>
          </div>
        </header>
        <div
          className="flex-1 overflow-y-auto px-2 py-3"
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault();
            void move(
              event.dataTransfer.getData("application/x-cocola-wiki-node"),
              currentFolderID,
            );
          }}
        >
          {query ? (
            filteredNodes.length ? (
              filteredNodes.map((node) => (
                <SearchResult key={node.id} node={node} onSelect={() => selectNode(node)} />
              ))
            ) : (
              <EmptyTree label="No matching pages" />
            )
          ) : visibleNodes.length ? (
            visibleNodes.map((node) => (
              <WikiNavigationRow
                key={node.id}
                node={node}
                selectedID={selectedID}
                onSelect={selectNode}
                onMove={move}
              />
            ))
          ) : loading ? (
            <EmptyTree label="Loading your Wiki…" />
          ) : (
            <EmptyTree label="Create your first page or upload a file." />
          )}
        </div>
        <footer className="border-t border-slate-200 p-3">
          <div className="grid grid-cols-3 gap-1.5">
            <QuickAction icon={Folder} label="Folder" disabled={busy} onClick={createFolder} />
            <QuickAction icon={Plus} label="Page" disabled={busy} onClick={createMarkdown} />
            <QuickAction icon={Upload} label="Upload" disabled={busy} onClick={requestUpload} />
          </div>
          <input
            ref={uploadRef}
            type="file"
            multiple
            accept={ACCEPTED_FILES}
            className="hidden"
            onChange={upload}
          />
          <p className="mt-2 text-center text-[10px] text-slate-400">
            {formatBytes(maxFileBytes)} max per file
          </p>
        </footer>
      </aside>
      <div
        role="separator"
        aria-label="Resize Wiki sidebar"
        aria-orientation="vertical"
        aria-valuemin={MIN_SIDEBAR_WIDTH}
        aria-valuemax={MAX_SIDEBAR_WIDTH}
        aria-valuenow={Math.round(sidebarWidth)}
        tabIndex={0}
        onPointerDown={startSidebarResize}
        onPointerMove={resizeSidebar}
        onPointerUp={(event) => {
          if (event.currentTarget.hasPointerCapture(event.pointerId)) {
            event.currentTarget.releasePointerCapture(event.pointerId);
          }
          finishSidebarResize();
        }}
        onPointerCancel={finishSidebarResize}
        onLostPointerCapture={finishSidebarResize}
        onKeyDown={resizeSidebarWithKeyboard}
        className="group relative z-20 w-0 shrink-0 cursor-col-resize touch-none outline-none"
      >
        <span className="absolute inset-y-0 -left-1.5 w-3">
          <span
            className={cn(
              "absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-transparent transition-colors group-hover:bg-blue-400 group-focus-visible:bg-blue-500",
              resizingSidebar && "bg-blue-500",
            )}
          />
        </span>
      </div>

      <main className="min-w-0 flex-1">
        {error ? (
          <div className="absolute right-5 top-4 z-30 max-w-lg rounded-xl border border-red-200 bg-red-50 px-4 py-2 text-sm text-red-700 shadow-lg">
            {error}
            <button
              type="button"
              onClick={() => setError("")}
              className="ml-3 font-semibold text-red-900"
            >
              Dismiss
            </button>
          </div>
        ) : null}
        {selected ? (
          selected.kind === "folder" ? (
            <FolderView
              folder={selected}
              nodes={nodes.filter((node) => (node.parent_id ?? "") === selected.id)}
              onSelect={selectNode}
              onRename={() => rename(selected)}
              onDelete={() => remove(selected)}
            />
          ) : (
            <FileView
              key={selected.id}
              node={selected}
              onRename={() => rename(selected)}
              onDelete={() => remove(selected)}
              onSaved={(updated) => {
                setNodes((current) =>
                  current.map((item) => (item.id === updated.id ? { ...item, ...updated } : item)),
                );
                changed();
              }}
              onUnsavedChange={handleUnsavedChange}
            />
          )
        ) : (
          <WikiWelcome onCreate={createMarkdown} onUpload={requestUpload} />
        )}
      </main>

      <TextInputDialog
        open={nameDialog !== null}
        title={
          nameDialog?.kind === "folder"
            ? "Create folder"
            : nameDialog?.kind === "markdown"
              ? "Create Markdown page"
              : "Rename item"
        }
        description={
          nameDialog?.kind === "folder"
            ? "Add a folder to the current Wiki location."
            : nameDialog?.kind === "markdown"
              ? "Create an editable Markdown page in the current Wiki location."
              : "Choose a new name. The file type must stay supported by Wiki."
        }
        label={
          nameDialog?.kind === "folder"
            ? "Folder name"
            : nameDialog?.kind === "markdown"
              ? "Filename"
              : "New name"
        }
        initialValue={
          nameDialog?.kind === "markdown"
            ? "Untitled.md"
            : nameDialog?.kind === "rename"
              ? (nameDialog.node?.name ?? "")
              : ""
        }
        placeholder={nameDialog?.kind === "markdown" ? "Notes.md" : "Name"}
        submitLabel={nameDialog?.kind === "rename" ? "Rename" : "Create"}
        busy={busy}
        error={nameDialogError}
        icon={
          nameDialog?.kind === "folder"
            ? Folder
            : nameDialog?.kind === "markdown"
              ? FileText
              : Pencil
        }
        onOpenChange={(open) => {
          if (open) return;
          setNameDialog(null);
          setNameDialogError("");
        }}
        onSubmit={(name) => void submitNameDialog(name)}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        title={deleteTarget?.kind === "folder" ? "Delete folder?" : "Delete file?"}
        description={
          deleteTarget?.kind === "folder" ? (
            <>
              <span className="font-medium text-foreground">{deleteTarget.name}</span> and
              everything inside it will be permanently deleted.
            </>
          ) : (
            <>
              <span className="font-medium text-foreground">{deleteTarget?.name}</span> will be
              permanently deleted.
            </>
          )
        }
        busy={busy}
        error={deleteError}
        onOpenChange={(open) => {
          if (open) return;
          setDeleteTarget(null);
          setDeleteError(null);
        }}
        onConfirm={() => void confirmDelete()}
      />

      <ActionConfirmDialog
        open={discardDialogOpen}
        title="Discard unsaved changes?"
        description="This page has changes that have not been saved. Continue only if you do not need them."
        confirmLabel="Discard and continue"
        cancelLabel="Keep editing"
        tone="warning"
        onOpenChange={(open) => {
          setDiscardDialogOpen(open);
          if (!open) pendingDiscardAction.current = null;
        }}
        onConfirm={() => {
          const action = pendingDiscardAction.current;
          pendingDiscardAction.current = null;
          setDiscardDialogOpen(false);
          action?.();
        }}
      />
    </div>
  );
}

function WikiNavigationRow({
  node,
  selectedID,
  onSelect,
  onMove,
}: {
  node: WikiNode;
  selectedID: string;
  onSelect: (node: WikiNode) => void;
  onMove: (nodeID: string, parentID: string) => Promise<void>;
}) {
  const Icon = fileIcon(node);
  return (
    <button
      type="button"
      draggable
      onDragStart={(event) => {
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData("application/x-cocola-wiki-node", node.id);
      }}
      onDragOver={(event) => {
        if (node.kind === "folder") event.preventDefault();
      }}
      onDrop={(event) => {
        if (node.kind !== "folder") return;
        event.preventDefault();
        event.stopPropagation();
        void onMove(event.dataTransfer.getData("application/x-cocola-wiki-node"), node.id);
      }}
      onClick={() => onSelect(node)}
      className={cn(
        "group mb-0.5 flex h-9 w-full items-center rounded-lg px-2 text-left text-[13px] transition",
        selectedID === node.id
          ? "bg-white text-slate-950 shadow-sm ring-1 ring-slate-200"
          : "text-slate-600 hover:bg-white/80 hover:text-slate-950",
      )}
    >
      <Icon
        className={cn(
          "mr-2 size-4 shrink-0",
          node.kind === "folder"
            ? "fill-blue-100 text-blue-600"
            : node.extension === ".md"
              ? "text-violet-600"
              : "text-slate-400",
        )}
      />
      <span className="min-w-0 flex-1 truncate">{node.name}</span>
      {node.kind === "folder" ? (
        <ChevronRight className="size-3.5 shrink-0 text-slate-300 transition group-hover:translate-x-0.5 group-hover:text-slate-500" />
      ) : null}
    </button>
  );
}

function SearchResult({ node, onSelect }: { node: WikiNode; onSelect: () => void }) {
  const Icon = fileIcon(node);
  return (
    <button
      type="button"
      onClick={onSelect}
      className="mb-1 flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left hover:bg-white"
    >
      <Icon className="size-4 shrink-0 text-blue-600" />
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium">{node.name}</span>
        <span className="block truncate text-[11px] text-slate-400">{node.logical_path}</span>
      </span>
    </button>
  );
}

function EmptyTree({ label }: { label: string }) {
  return <p className="px-3 py-8 text-center text-xs leading-5 text-slate-400">{label}</p>;
}

function QuickAction({
  icon: Icon,
  label,
  disabled,
  onClick,
}: {
  icon: typeof Folder;
  label: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="flex h-9 items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white text-xs font-medium text-slate-600 transition hover:border-blue-200 hover:text-blue-700 disabled:opacity-50"
    >
      <Icon className="size-3.5" />
      {label}
    </button>
  );
}

function PageActions({
  node,
  onRename,
  onDelete,
}: {
  node: WikiNode;
  onRename: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="flex items-center gap-1">
      {node.kind === "file" ? (
        <a
          href={`/api/wiki/files/${encodeURIComponent(node.id)}/download`}
          className="grid size-9 place-items-center rounded-xl text-slate-500 hover:bg-slate-100 hover:text-slate-900"
          title="Download"
        >
          <Download className="size-4" />
        </a>
      ) : null}
      <button
        type="button"
        onClick={onRename}
        className="grid size-9 place-items-center rounded-xl text-slate-500 hover:bg-slate-100 hover:text-slate-900"
        title="Rename"
      >
        <Pencil className="size-4" />
      </button>
      <button
        type="button"
        onClick={onDelete}
        className="grid size-9 place-items-center rounded-xl text-slate-500 hover:bg-red-50 hover:text-red-600"
        title="Delete"
      >
        <Trash2 className="size-4" />
      </button>
    </div>
  );
}

function FolderView({
  folder,
  nodes,
  onSelect,
  onRename,
  onDelete,
}: {
  folder: WikiNode;
  nodes: WikiNode[];
  onSelect: (node: WikiNode) => void;
  onRename: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-5xl px-8 py-9">
        <header className="flex items-start justify-between gap-5 border-b border-slate-200 pb-6">
          <div>
            <div className="mb-4 grid size-12 place-items-center rounded-2xl bg-blue-50 text-blue-600">
              <FolderOpen className="size-6" />
            </div>
            <h2 className="text-3xl font-semibold tracking-tight">{folder.name}</h2>
            <p className="mt-2 text-sm text-slate-500">
              {nodes.length} {nodes.length === 1 ? "item" : "items"} · {folder.logical_path}
            </p>
          </div>
          <PageActions node={folder} onRename={onRename} onDelete={onDelete} />
        </header>
        {nodes.length ? (
          <div className="mt-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {nodes.map((node) => {
              const Icon = fileIcon(node);
              return (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => onSelect(node)}
                  className="group rounded-2xl border border-slate-200 bg-white p-4 text-left shadow-sm transition hover:-translate-y-0.5 hover:border-blue-200 hover:shadow-md"
                >
                  <div className="flex items-start justify-between">
                    <span className="grid size-10 place-items-center rounded-xl bg-slate-50 text-blue-600 group-hover:bg-blue-50">
                      <Icon className="size-5" />
                    </span>
                    <MoreHorizontal className="size-4 text-slate-300" />
                  </div>
                  <p className="mt-4 truncate text-sm font-semibold">{node.name}</p>
                  <p className="mt-1 text-xs text-slate-400">
                    {node.kind === "folder" ? "Folder" : formatBytes(node.size_bytes)}
                  </p>
                </button>
              );
            })}
          </div>
        ) : (
          <div className="mt-20 text-center text-sm text-slate-400">This folder is empty.</div>
        )}
      </div>
    </div>
  );
}

function FileView({
  node,
  onRename,
  onDelete,
  onSaved,
  onUnsavedChange,
}: {
  node: WikiNode;
  onRename: () => void;
  onDelete: () => void;
  onSaved: (node: WikiNode) => void;
  onUnsavedChange: (nodeID: string, unsaved: boolean) => void;
}) {
  const markdown = node.extension === ".md";
  const [content, setContent] = useState("");
  const [revision, setRevision] = useState(node.revision ?? 1);
  const [state, setState] = useState<SaveState>(markdown ? "loading" : "saved");
  const [contentLoaded, setContentLoaded] = useState(!markdown);
  const [loadError, setLoadError] = useState("");
  const loadedContent = useRef("");
  const loadAbort = useRef<AbortController | null>(null);
  const initialRevision = useRef(node.revision ?? 1);

  const loadMarkdown = useCallback(() => {
    if (!markdown) return;
    loadAbort.current?.abort();
    const controller = new AbortController();
    loadAbort.current = controller;
    setContentLoaded(false);
    setLoadError("");
    setState("loading");
    void fetch(`/api/wiki/files/${encodeURIComponent(node.id)}/content`, {
      cache: "no-store",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(await responseError(response));
        const text = await response.text();
        const etag = response.headers.get("etag") ?? "";
        const match = etag.match(/wiki-rev-(\d+)/);
        setRevision(match ? Number(match[1]) : initialRevision.current);
        loadedContent.current = text;
        setContent(text);
        setContentLoaded(true);
        setState("saved");
      })
      .catch((cause) => {
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        setLoadError(cause instanceof Error ? cause.message : String(cause));
        setContentLoaded(false);
        setState("load-error");
      })
      .finally(() => {
        if (loadAbort.current === controller) loadAbort.current = null;
      });
  }, [markdown, node.id]);

  useEffect(() => {
    loadMarkdown();
    return () => loadAbort.current?.abort();
  }, [loadMarkdown]);

  useEffect(() => {
    const unsaved =
      markdown &&
      contentLoaded &&
      (state === "dirty" || state === "saving" || state === "conflict" || state === "error");
    onUnsavedChange(node.id, unsaved);
    return () => onUnsavedChange(node.id, false);
  }, [contentLoaded, markdown, node.id, onUnsavedChange, state]);

  const save = async () => {
    if (!markdown || !contentLoaded || (state !== "dirty" && state !== "error")) return;
    setState("saving");
    try {
      const response = await fetch(`/api/wiki/files/${encodeURIComponent(node.id)}/content`, {
        method: "PUT",
        headers: {
          "content-type": "text/markdown; charset=utf-8",
          "if-match": `"wiki-rev-${revision}"`,
        },
        body: content,
      });
      if (response.status === 412) {
        setState("conflict");
        return;
      }
      if (!response.ok) throw new Error(await responseError(response));
      const updated = (await response.json()) as WikiNode;
      loadedContent.current = content;
      setRevision(updated.revision ?? revision + 1);
      setState("saved");
      onSaved(updated);
    } catch {
      setState("error");
    }
  };

  if (!markdown) {
    const office = OFFICE_EXTENSIONS.has(node.extension ?? "");
    return (
      <div className="flex h-full min-h-0 flex-col">
        <FileHeader node={node} state="saved" onRename={onRename} onDelete={onDelete} />
        <div className="min-h-0 flex-1 p-6">
          {office ? (
            <div className="flex h-full items-center justify-center rounded-3xl border border-dashed border-slate-300 bg-slate-50">
              <div className="max-w-md px-8 text-center">
                <span className="mx-auto grid size-16 place-items-center rounded-2xl bg-gradient-to-br from-blue-50 to-violet-50 text-violet-600">
                  {node.extension === ".xlsx" ? (
                    <FileSpreadsheet className="size-8" />
                  ) : (
                    <FileText className="size-8" />
                  )}
                </span>
                <h3 className="mt-5 text-lg font-semibold">Ready for Agent reading</h3>
                <p className="mt-2 text-sm leading-6 text-slate-500">
                  Cocola preserves the original Office file. Reference it with @ in a chat and the
                  Agent can extract its document, slide, or spreadsheet content. Web preview and
                  online Office editing are not included.
                </p>
                <a
                  href={`/api/wiki/files/${encodeURIComponent(node.id)}/download`}
                  className="mt-5 inline-flex h-10 items-center justify-center rounded-full bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition hover:bg-primary/90"
                >
                  <Download className="mr-2 size-4" />
                  Download original
                </a>
              </div>
            </div>
          ) : (
            <div className="h-full overflow-hidden rounded-2xl border border-slate-200 bg-white">
              <ReadonlyFilePreview
                file={{
                  filename: node.name,
                  size: node.size_bytes ?? 0,
                  mimeType: node.mime_type ?? "application/octet-stream",
                  url: `/api/wiki/files/${encodeURIComponent(node.id)}/content`,
                  previewKind: previewKind(node),
                }}
                fetchBinary
              />
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <FileHeader node={node} state={state} onRename={onRename} onDelete={onDelete}>
        <Button
          size="sm"
          disabled={
            !contentLoaded ||
            state === "loading" ||
            state === "load-error" ||
            state === "saved" ||
            state === "saving" ||
            state === "conflict"
          }
          onClick={() => void save()}
        >
          {state === "saving" ? "Saving…" : "Save"}
        </Button>
      </FileHeader>
      <div className="min-h-0 flex-1 overflow-y-auto bg-slate-50/80 p-4 sm:p-5">
        {state === "loading" && !contentLoaded ? (
          <div className="grid h-full min-h-72 place-items-center rounded-3xl border border-slate-200 bg-white">
            <div className="text-center text-sm text-slate-500">
              <RefreshCw className="mx-auto mb-3 size-5 animate-spin text-blue-600" />
              Loading page…
            </div>
          </div>
        ) : loadError ? (
          <div
            role="alert"
            className="grid h-full min-h-72 place-items-center rounded-3xl border border-red-200 bg-white"
          >
            <div className="max-w-md px-8 text-center">
              <span className="mx-auto grid size-11 place-items-center rounded-2xl bg-red-50 text-red-600">
                <FileText className="size-5" />
              </span>
              <h3 className="mt-4 text-base font-semibold text-slate-950">
                Couldn&apos;t open this page
              </h3>
              <p className="mt-2 text-sm leading-6 text-slate-500">{loadError}</p>
              <Button size="sm" variant="outline" className="mt-5" onClick={loadMarkdown}>
                <RefreshCw className="mr-2 size-3.5" />
                Try again
              </Button>
            </div>
          </div>
        ) : (
          <WikiMarkdownEditor
            value={content}
            readOnly={state === "saving" || state === "conflict"}
            onChange={(next) => {
              setContent(next);
              setState(next === loadedContent.current ? "saved" : "dirty");
            }}
          />
        )}
      </div>
      {state === "conflict" ? (
        <div className="flex items-center justify-between border-t border-amber-200 bg-amber-50 px-5 py-3 text-sm text-amber-900">
          <span>
            This page changed in another tab. Reload before continuing to avoid overwriting it.
          </span>
          <Button size="sm" variant="outline" onClick={() => window.location.reload()}>
            Reload
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function FileHeader({
  node,
  state,
  onRename,
  onDelete,
  children,
}: {
  node: WikiNode;
  state: SaveState;
  onRename: () => void;
  onDelete: () => void;
  children?: ReactNode;
}) {
  const status = {
    loading: "Loading…",
    "load-error": "Load failed",
    saved: "Saved",
    dirty: "Unsaved",
    saving: "Saving…",
    conflict: "Edit conflict",
    error: "Save failed",
  }[state];
  return (
    <header className="flex h-[4.75rem] shrink-0 items-center justify-between gap-4 border-b border-slate-200 px-6">
      <div className="min-w-0">
        <p className="truncate text-[11px] text-slate-400">{node.logical_path}</p>
        <div className="mt-1 flex items-center gap-2">
          <h2 className="truncate text-lg font-semibold">{node.name}</h2>
          <span
            className={cn(
              "text-[11px]",
              state === "load-error" || state === "error" || state === "conflict"
                ? "text-red-600"
                : "text-slate-400",
            )}
          >
            {status}
          </span>
        </div>
      </div>
      <div className="flex items-center gap-3">
        {children}
        <PageActions node={node} onRename={onRename} onDelete={onDelete} />
      </div>
    </header>
  );
}

function WikiWelcome({ onCreate, onUpload }: { onCreate: () => void; onUpload: () => void }) {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="max-w-xl text-center">
        <div className="relative mx-auto grid size-24 place-items-center">
          <span className="absolute inset-0 rounded-[2rem] bg-gradient-to-br from-blue-100 via-violet-100 to-emerald-100 blur-lg" />
          <span className="relative grid size-20 place-items-center rounded-[1.75rem] border border-white bg-white text-violet-600 shadow-xl shadow-blue-100">
            <BookOpenText className="size-9" />
          </span>
        </div>
        <h2 className="mt-7 text-3xl font-semibold tracking-tight">Build your working Wiki</h2>
        <p className="mx-auto mt-3 max-w-lg text-sm leading-6 text-slate-500">
          Organize durable context in folders, write Markdown with source fidelity, and reference
          exact files in any Agent conversation with @.
        </p>
        <div className="mt-7 flex justify-center gap-3">
          <Button onClick={onCreate} className="rounded-full px-5">
            <Plus className="mr-2 size-4" />
            New Markdown
          </Button>
          <Button variant="outline" onClick={onUpload} className="rounded-full px-5">
            <Upload className="mr-2 size-4" />
            Upload files
          </Button>
        </div>
      </div>
    </div>
  );
}
