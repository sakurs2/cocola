"use client";

import { Folder, Loader2, MessagesSquare, Plus } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";
import { useCocola } from "@/app/runtime-provider";

export default function FoldersPage() {
  const { conversations, folders, foldersLoaded } = useCocola();
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

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8 lg:px-12">
      <main className="mx-auto w-full max-w-4xl pb-16">
        <header className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-4">
            <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-amber-500/10 text-amber-600">
              <Folder className="size-5" />
            </div>
            <div>
              <h1 className="text-2xl font-semibold tracking-tight">Folders</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                Group related conversations in one place.
              </p>
            </div>
          </div>
          <Link
            href="/folders/new"
            className="inline-flex h-10 items-center justify-center gap-2 self-start rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 sm:self-auto"
          >
            <Plus className="size-4" />
            New folder
          </Link>
        </header>

        <section className="mt-8">
          {!foldersLoaded ? (
            <div className="grid min-h-48 place-items-center">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          ) : folderRows.length === 0 ? (
            <div className="rounded-3xl border border-dashed border-border px-6 py-14 text-center">
              <Folder className="mx-auto size-8 text-muted-foreground/70" />
              <h2 className="mt-3 text-sm font-semibold">No folders yet</h2>
              <p className="mt-1 text-xs text-muted-foreground">
                Create a folder to organize related chats.
              </p>
            </div>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              {folderRows.map((folder) => (
                <Link
                  key={folder.id}
                  href={`/folders/${encodeURIComponent(folder.id)}`}
                  className="group flex items-center gap-3.5 rounded-2xl border border-border bg-card p-5 transition-colors hover:border-amber-500/30 hover:bg-muted/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                >
                  <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-amber-500/10 text-amber-600">
                    <Folder className="size-5" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <h2 className="truncate font-semibold group-hover:text-amber-600">
                      {folder.name}
                    </h2>
                    <p className="mt-1 inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                      <MessagesSquare className="size-3.5" />
                      {folder.chatCount} {folder.chatCount === 1 ? "chat" : "chats"}
                    </p>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </section>
      </main>
    </div>
  );
}
