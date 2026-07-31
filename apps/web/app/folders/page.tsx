"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { ArrowRight, Folder, FolderPlus, Loader2, MessagesSquare, Plus, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useCocola } from "@/app/runtime-provider";

export default function FoldersPage() {
  const router = useRouter();
  const { conversations, folders, foldersLoaded, createFolder } = useCocola();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    if (params.get("new") === "1") {
      setOpen(true);
      params.delete("new");
      const query = params.toString();
      window.history.replaceState(null, "", `/folders${query ? `?${query}` : ""}`);
    }
  }, []);

  const folderRows = useMemo(() => {
    const chatCounts = new Map<string, number>();
    for (const conversation of conversations) {
      if (!conversation.folder_id || conversation.chat_type === "scheduled_task") continue;
      chatCounts.set(conversation.folder_id, (chatCounts.get(conversation.folder_id) ?? 0) + 1);
    }
    return [...folders]
      .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
      .map((folder) => ({ ...folder, chatCount: chatCounts.get(folder.id) ?? 0 }));
  }, [conversations, folders]);

  const changeOpen = (next: boolean) => {
    if (busy) return;
    setOpen(next);
    if (!next) {
      setName("");
      setError("");
    }
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const nextName = name.trim();
    if (!nextName || busy) return;
    setBusy(true);
    setError("");
    try {
      const folder = await createFolder(nextName);
      setBusy(false);
      setOpen(false);
      setName("");
      router.push(`/folders/${encodeURIComponent(folder.id)}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not create folder");
      setBusy(false);
    }
  };

  return (
    <main className="user-canvas user-page user-theme-amber h-full min-w-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6 sm:py-7">
      <div className="mx-auto w-full max-w-5xl pb-16">
        {/* Header */}
        <header className="flex flex-wrap items-center gap-3.5">
          <div className="user-page-icon">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M4 20V6a2 2 0 0 1 2-2h5l2 2h5a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2Z" />
            </svg>
          </div>
          <div className="min-w-0 flex-1">
            <div className="user-eyebrow">Workspace</div>
            <h1 className="mt-1 text-[28px] font-semibold tracking-tight">Folders</h1>
          </div>
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="user-accent-btn inline-flex h-10 items-center gap-2 rounded-xl px-[18px] text-[13.5px] font-semibold"
          >
            <Plus className="size-4" />
            New folder
          </button>
        </header>

        {/* Section title */}
        <div className="mb-3 mt-[22px] flex items-center gap-2">
          <span className="user-section-title">All folders</span>
          <span className="user-count-badge">{folderRows.length}</span>
        </div>

        {/* Grid */}
        {!foldersLoaded ? (
          <div className="grid min-h-48 place-items-center">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : folderRows.length === 0 ? (
          <div className="user-empty">
            <h2 className="text-sm font-semibold">No folders yet</h2>
            <p className="text-xs text-muted-foreground">
              Create a folder to organize related chats.
            </p>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {folderRows.map((folder) => (
              <Link
                key={folder.id}
                href={`/folders/${encodeURIComponent(folder.id)}`}
                className="task-card group"
              >
                <div className="task-card-head">
                  <span className="task-card-icon">
                    <Folder className="size-[18px]" />
                  </span>
                  <span className="task-card-title truncate">{folder.name}</span>
                </div>
                <span className="task-card-summary">
                  <MessagesSquare className="size-3.5" />
                  {folder.chatCount} {folder.chatCount === 1 ? "chat" : "chats"}
                </span>
                <div className="task-card-foot">
                  <span className="task-card-cta">
                    Open
                    <ArrowRight className="size-3.5" />
                  </span>
                </div>
              </Link>
            ))}
          </div>
        )}
      </div>

      {/* New folder drawer */}
      <Dialog.Root open={open} onOpenChange={changeOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/20 backdrop-blur-sm data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out data-[state=open]:fade-in" />
          <Dialog.Content className="cocola-user-ui user-theme-amber fixed inset-y-2 right-2 z-50 flex w-[min(28rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-2xl border border-border bg-background text-foreground shadow-2xl outline-none data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right">
            <header className="flex min-h-16 items-center gap-3 border-b border-border/70 px-5">
              <span className="user-panel-glyph">
                <FolderPlus className="size-[18px]" />
              </span>
              <div className="min-w-0 flex-1">
                <Dialog.Title className="truncate text-base font-semibold">
                  Create a folder
                </Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="Close"
                className="grid size-9 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
              >
                <X className="size-4" />
              </Dialog.Close>
            </header>
            <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
              <div className="min-h-0 flex-1 overflow-y-auto p-5">
                <label className="block space-y-1.5">
                  <span className="user-section-title text-sm font-medium">Folder name</span>
                  <input
                    autoFocus
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder="e.g. Product research"
                    className="user-search-input user-field-input mt-2 h-11 w-full rounded-xl px-3 text-sm"
                  />
                </label>
                {error ? (
                  <p role="alert" className="mt-3 text-sm text-destructive">
                    {error}
                  </p>
                ) : null}
              </div>
              <div className="flex items-center justify-end gap-2 border-t border-border/70 p-4">
                <button
                  type="button"
                  onClick={() => changeOpen(false)}
                  className="inline-flex h-10 items-center justify-center rounded-xl border border-black/[0.08] bg-white px-4 text-sm font-semibold text-muted-foreground transition-colors duration-200 hover:text-foreground"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={busy || !name.trim()}
                  className="user-accent-btn inline-flex h-10 items-center justify-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
                  Create folder
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </main>
  );
}
