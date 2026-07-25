"use client";

import dynamic from "next/dynamic";
import {
  BookOpenText,
  ChevronDown,
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
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
} from "react";
import { MarkdownContent } from "@/components/assistant-ui/markdown-text";
import { ReadonlyFilePreview } from "@/components/assistant-ui/file-preview";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), { ssr: false });

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

type WikiTreeNode = WikiNode & { children: WikiTreeNode[] };
type SaveState = "loading" | "saved" | "dirty" | "saving" | "conflict" | "error";

const DEFAULT_MAX_FILE_BYTES = 20 * 1024 * 1024;
const OFFICE_EXTENSIONS = new Set([".docx", ".xlsx", ".pptx"]);
const ACCEPTED_FILES = ".md,.txt,.csv,.json,.yaml,.yml,.pdf,.docx,.xlsx,.pptx,application/pdf";

async function responseError(response: Response) {
  const body = (await response.json().catch(() => null)) as {
    error?: { message?: string };
  } | null;
  return body?.error?.message || `Request failed (${response.status})`;
}

function buildTree(nodes: WikiNode[]): WikiTreeNode[] {
  const byID = new Map<string, WikiTreeNode>();
  for (const node of nodes) byID.set(node.id, { ...node, children: [] });
  const roots: WikiTreeNode[] = [];
  for (const node of byID.values()) {
    const parent = node.parent_id ? byID.get(node.parent_id) : undefined;
    if (parent?.kind === "folder") parent.children.push(node);
    else roots.push(node);
  }
  const sort = (items: WikiTreeNode[]) => {
    items.sort(
      (a, b) =>
        Number(b.kind === "folder") - Number(a.kind === "folder") ||
        a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
    );
    for (const item of items) sort(item.children);
  };
  sort(roots);
  return roots;
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
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [maxFileBytes, setMaxFileBytes] = useState(DEFAULT_MAX_FILE_BYTES);
  const [unsavedFileID, setUnsavedFileID] = useState("");
  const uploadRef = useRef<HTMLInputElement>(null);

  const tree = useMemo(() => buildTree(nodes), [nodes]);
  const selected = nodes.find((node) => node.id === selectedID) ?? null;
  const activeFolderID = selected?.kind === "folder" ? selected.id : (selected?.parent_id ?? "");
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

  const confirmDiscardChanges = useCallback(
    (nextNodeID = "") =>
      !unsavedFileID ||
      unsavedFileID === nextNodeID ||
      window.confirm("This page has unsaved changes. Discard them and continue?"),
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

  const createFolder = async () => {
    if (!confirmDiscardChanges()) return;
    const name = window.prompt("Folder name");
    if (!name?.trim()) return;
    setBusy(true);
    try {
      const response = await fetch("/api/wiki/folders", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ parent_id: activeFolderID, name: name.trim() }),
      });
      if (!response.ok) throw new Error(await responseError(response));
      const node = (await response.json()) as WikiNode;
      setExpanded((current) => new Set(current).add(activeFolderID));
      await loadTree(node.id);
      changed();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const createMarkdown = async () => {
    if (!confirmDiscardChanges()) return;
    const raw = window.prompt("Markdown filename", "Untitled.md");
    if (!raw?.trim()) return;
    setBusy(true);
    try {
      const response = await fetch("/api/wiki/markdown", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          parent_id: activeFolderID,
          name: raw.trim(),
          content: `# ${raw.trim().replace(/\.md$/i, "")}\n\n`,
        }),
      });
      if (!response.ok) throw new Error(await responseError(response));
      const node = (await response.json()) as WikiNode;
      await loadTree(node.id);
      changed();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (files.length === 0) return;
    if (!confirmDiscardChanges()) return;
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

  const rename = async (node: WikiNode) => {
    const name = window.prompt("Rename", node.name);
    if (!name?.trim() || name.trim() === node.name) return;
    setBusy(true);
    try {
      const response = await fetch(`/api/wiki/nodes/${encodeURIComponent(node.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ name: name.trim() }),
      });
      if (!response.ok) throw new Error(await responseError(response));
      await loadTree(node.id);
      changed();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (node: WikiNode) => {
    const detail =
      node.kind === "folder"
        ? `Delete “${node.name}” and everything inside it?`
        : `Delete “${node.name}”?`;
    if (!window.confirm(detail)) return;
    setBusy(true);
    try {
      const response = await fetch(`/api/wiki/nodes/${encodeURIComponent(node.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await responseError(response));
      await loadTree();
      changed();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const move = async (nodeID: string, parentID: string) => {
    if (!nodeID || nodeID === parentID) return;
    if (!confirmDiscardChanges(nodeID)) return;
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

  const selectNode = (node: WikiNode) => {
    if (!confirmDiscardChanges(node.id)) return;
    setSelectedID(node.id);
    setQuery("");
    if (node.kind === "folder") {
      setExpanded((current) => new Set(current).add(node.id));
    }
  };

  return (
    <div className="relative flex h-full min-h-0 bg-[#fff] text-[#142033]">
      <div
        className="pointer-events-none absolute inset-y-0 left-0 w-1 bg-gradient-to-b from-[#2563EB] via-[#7C3AED] to-[#10B981]"
        aria-hidden="true"
      />
      <aside className="flex w-[19rem] shrink-0 flex-col border-r border-slate-200 bg-[#F8FAFD]">
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
        </header>
        <div
          className="flex-1 overflow-y-auto px-2 py-3"
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault();
            void move(event.dataTransfer.getData("application/x-cocola-wiki-node"), "");
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
          ) : tree.length ? (
            tree.map((node) => (
              <WikiTreeRow
                key={node.id}
                node={node}
                depth={0}
                selectedID={selectedID}
                expanded={expanded}
                onToggle={(id) =>
                  setExpanded((current) => {
                    const next = new Set(current);
                    if (next.has(id)) next.delete(id);
                    else next.add(id);
                    return next;
                  })
                }
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
            <QuickAction
              icon={Upload}
              label="Upload"
              disabled={busy}
              onClick={() => uploadRef.current?.click()}
            />
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
          <WikiWelcome onCreate={createMarkdown} onUpload={() => uploadRef.current?.click()} />
        )}
      </main>
    </div>
  );
}

function WikiTreeRow({
  node,
  depth,
  selectedID,
  expanded,
  onToggle,
  onSelect,
  onMove,
}: {
  node: WikiTreeNode;
  depth: number;
  selectedID: string;
  expanded: Set<string>;
  onToggle: (id: string) => void;
  onSelect: (node: WikiNode) => void;
  onMove: (nodeID: string, parentID: string) => Promise<void>;
}) {
  const Icon = fileIcon(node);
  const open = node.kind === "folder" && expanded.has(node.id);
  return (
    <div>
      <div
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
        className={cn(
          "group flex h-8 cursor-default items-center rounded-lg pr-2 text-[13px] transition",
          selectedID === node.id
            ? "bg-white text-slate-950 shadow-sm ring-1 ring-slate-200"
            : "text-slate-600 hover:bg-white/80 hover:text-slate-950",
        )}
        style={{ paddingLeft: `${6 + depth * 14}px` }}
        onClick={() => onSelect(node)}
      >
        <button
          type="button"
          className="grid size-5 shrink-0 place-items-center rounded"
          onClick={(event) => {
            event.stopPropagation();
            if (node.kind === "folder") onToggle(node.id);
          }}
          aria-label={open ? "Collapse folder" : "Expand folder"}
        >
          {node.kind === "folder" ? (
            open ? (
              <ChevronDown className="size-3.5" />
            ) : (
              <ChevronRight className="size-3.5" />
            )
          ) : null}
        </button>
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
      </div>
      {open
        ? node.children.map((child) => (
            <WikiTreeRow
              key={child.id}
              node={child}
              depth={depth + 1}
              selectedID={selectedID}
              expanded={expanded}
              onToggle={onToggle}
              onSelect={onSelect}
              onMove={onMove}
            />
          ))
        : null}
    </div>
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
  const [tab, setTab] = useState<"source" | "preview">("source");
  const loadedContent = useRef("");
  const loaded = useRef(false);
  const initialRevision = useRef(node.revision ?? 1);

  useEffect(() => {
    if (!markdown) return;
    const controller = new AbortController();
    loaded.current = false;
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
        loaded.current = true;
        setContent(text);
        setState("saved");
      })
      .catch((cause) => {
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        setState("error");
      });
    return () => controller.abort();
  }, [markdown, node.id]);

  useEffect(() => {
    const unsaved =
      markdown &&
      loaded.current &&
      (state === "dirty" || state === "saving" || state === "conflict" || state === "error");
    onUnsavedChange(node.id, unsaved);
    return () => onUnsavedChange(node.id, false);
  }, [markdown, node.id, onUnsavedChange, state]);

  const save = async () => {
    if (!markdown || !loaded.current || (state !== "dirty" && state !== "error")) return;
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
        <div className="flex rounded-xl bg-slate-100 p-1">
          {(["source", "preview"] as const).map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setTab(value)}
              className={cn(
                "rounded-lg px-3 py-1.5 text-xs font-medium capitalize transition",
                tab === value ? "bg-white text-slate-950 shadow-sm" : "text-slate-500",
              )}
            >
              {value}
            </button>
          ))}
        </div>
        <Button
          size="sm"
          disabled={
            !loaded.current ||
            state === "loading" ||
            state === "saved" ||
            state === "saving" ||
            state === "conflict"
          }
          onClick={() => void save()}
        >
          {state === "saving" ? "Saving…" : "Save"}
        </Button>
      </FileHeader>
      <div className="min-h-0 flex-1">
        {tab === "source" ? (
          <MonacoEditor
            language="markdown"
            value={content}
            onChange={(value) => {
              const next = value ?? "";
              setContent(next);
              setState(next === loadedContent.current ? "saved" : "dirty");
            }}
            theme="vs"
            options={{
              minimap: { enabled: false },
              wordWrap: "on",
              fontSize: 14,
              lineHeight: 23,
              padding: { top: 28, bottom: 28 },
              scrollBeyondLastLine: false,
              automaticLayout: true,
              lineNumbersMinChars: 3,
              renderLineHighlight: "none",
              readOnly:
                !loaded.current ||
                state === "loading" ||
                state === "saving" ||
                state === "conflict",
            }}
          />
        ) : (
          <div className="h-full overflow-y-auto">
            <MarkdownContent value={content} className="mx-auto max-w-3xl px-10 py-10" />
          </div>
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
              state === "error" || state === "conflict" ? "text-red-600" : "text-slate-400",
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
