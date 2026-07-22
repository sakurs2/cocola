"use client";

import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { ChevronRight, Folder, MessagesSquare, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useCocola, type ConversationSummary } from "@/app/runtime-provider";
import { ConversationActionsMenu } from "@/components/assistant-ui/conversation-actions-menu";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
import { ConversationComposer } from "@/components/assistant-ui/thread";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";

type DeleteTarget = { kind: "folder" | "conversation"; id: string; title: string };

export default function FolderPage() {
  const params = useParams<{ id: string }>();
  const folderID = params.id;
  const router = useRouter();
  const { showSuccess } = useWorkspaceToast();
  const {
    folders,
    foldersLoaded,
    conversations,
    runtimes,
    activeSessionId,
    runningSessionIds,
    newConversation,
    loadConversation,
    renameConversation,
    deleteConversation,
    renameFolder,
    deleteFolder,
    moveConversation,
  } = useCocola();
  const preparedFolder = useRef<string | null>(null);
  const preparedSession = useRef<string | null>(null);
  const folder = folders.find((item) => item.id === folderID);
  const folderConversations = useMemo(
    () =>
      conversations
        .filter(
          (conversation) =>
            conversation.chat_type !== "scheduled_task" && conversation.folder_id === folderID,
        )
        .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)),
    [conversations, folderID],
  );
  const [editingConversationID, setEditingConversationID] = useState<string | null>(null);
  const [conversationDraft, setConversationDraft] = useState("");
  const [editingFolder, setEditingFolder] = useState(false);
  const [folderDraft, setFolderDraft] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!folder || preparedFolder.current === folder.id) return;
    preparedFolder.current = folder.id;
    preparedSession.current = newConversation(folder.id);
  }, [folder, newConversation]);

  useEffect(() => {
    if (
      preparedFolder.current === folderID &&
      preparedSession.current === activeSessionId &&
      runningSessionIds.has(activeSessionId)
    ) {
      router.push("/");
    }
  }, [activeSessionId, folderID, router, runningSessionIds]);

  const openConversation = async (id: string) => {
    await loadConversation(id);
    router.push("/");
  };

  const commitConversationRename = async (conversation: ConversationSummary) => {
    const title = conversationDraft.trim();
    setEditingConversationID(null);
    if (!title) return;
    try {
      await renameConversation(conversation.id, title);
      setError(null);
    } catch (renameError) {
      setError(renameError instanceof Error ? renameError.message : "Could not rename chat");
    }
  };

  const commitFolderRename = async () => {
    if (!folder) return;
    const name = folderDraft.trim();
    if (!name) {
      setEditingFolder(false);
      return;
    }
    try {
      await renameFolder(folder.id, name);
      setEditingFolder(false);
      setError(null);
    } catch (renameError) {
      setError(renameError instanceof Error ? renameError.message : "Could not rename folder");
    }
  };

  const moveChat = async (conversationID: string, destination: string | null) => {
    try {
      await moveConversation(conversationID, destination);
      setError(null);
      const destinationName = destination
        ? folders.find((item) => item.id === destination)?.name || "folder"
        : "Chats";
      showSuccess(`Moved to ${destinationName}`);
    } catch (moveError) {
      setError(moveError instanceof Error ? moveError.message : "Could not move conversation");
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    setError(null);
    try {
      if (deleteTarget.kind === "folder") {
        await deleteFolder(deleteTarget.id);
        router.push("/folders");
      } else {
        await deleteConversation(deleteTarget.id);
      }
      setDeleteTarget(null);
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "Could not delete item");
    } finally {
      setDeleting(false);
    }
  };

  if (!folder && foldersLoaded) {
    return (
      <div className="grid h-full place-items-center px-6 text-center">
        <div>
          <Folder className="mx-auto size-9 text-muted-foreground" />
          <h1 className="mt-3 text-lg font-semibold">Folder not found</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            It may have been deleted or belongs to another account.
          </p>
          <button
            type="button"
            onClick={() => router.push("/folders")}
            className="mt-4 rounded-xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground focus:outline-none"
          >
            Back to folders
          </button>
        </div>
      </div>
    );
  }

  if (!folder) return <div className="h-full" />;

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8 lg:px-12">
      <div className="mx-auto w-full max-w-4xl pb-16">
        <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <Link href="/folders" className="hover:text-foreground">
            Folders
          </Link>
          <ChevronRight className="size-3.5" />
          <span className="truncate text-foreground/75">{folder.name}</span>
        </div>

        <section className="relative mt-8 pl-8 sm:pl-11">
          <div className="absolute bottom-[-2.5rem] left-[0.9rem] top-10 w-px bg-gradient-to-b from-primary/55 via-primary/20 to-transparent sm:left-[1.15rem]" />
          <div className="absolute left-0 top-0 grid size-8 place-items-center rounded-xl border border-primary/15 bg-primary/10 text-primary shadow-sm sm:size-9">
            <Folder className="size-4 sm:size-[18px]" />
          </div>
          <div className="flex min-w-0 items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              {editingFolder ? (
                <input
                  autoFocus
                  value={folderDraft}
                  onChange={(event) => setFolderDraft(event.target.value)}
                  onBlur={() => void commitFolderRename()}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") event.currentTarget.blur();
                    if (event.key === "Escape") setEditingFolder(false);
                  }}
                  className="w-full bg-transparent text-3xl font-semibold tracking-tight outline-none"
                />
              ) : (
                <h1 className="truncate text-3xl font-semibold tracking-tight text-foreground">
                  {folder.name}
                </h1>
              )}
              <p className="mt-2 text-sm text-muted-foreground">
                Start something new or continue a conversation in this folder.
              </p>
            </div>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button
                  type="button"
                  aria-label="Folder actions"
                  className="grid size-9 shrink-0 place-items-center rounded-xl text-muted-foreground transition hover:bg-muted hover:text-foreground focus:outline-none"
                >
                  <MoreHorizontal className="size-5" />
                </button>
              </DropdownMenu.Trigger>
              <DropdownMenu.Portal>
                <DropdownMenu.Content
                  align="end"
                  sideOffset={5}
                  className="cocola-user-ui z-50 min-w-40 rounded-xl border border-border bg-popover p-1 text-foreground shadow-xl outline-none"
                >
                  <DropdownMenu.Item
                    onSelect={() => {
                      setFolderDraft(folder.name);
                      setEditingFolder(true);
                    }}
                    className="flex cursor-default items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-foreground outline-none focus:bg-accent focus:text-foreground data-[highlighted]:bg-accent data-[highlighted]:text-foreground"
                  >
                    <Pencil className="size-4" />
                    Rename
                  </DropdownMenu.Item>
                  <DropdownMenu.Item
                    onSelect={() =>
                      setDeleteTarget({ kind: "folder", id: folder.id, title: folder.name })
                    }
                    className="flex cursor-default items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-red-500 outline-none focus:bg-red-500/10 focus:text-red-600"
                  >
                    <Trash2 className="size-4" />
                    Delete
                  </DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
          </div>

          <div className="mt-8">
            <ConversationComposer placeholder={`Start a chat in ${folder.name}...`} />
          </div>
        </section>

        <section className="mt-14">
          <div className="flex items-end justify-between border-b border-border/80 pb-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">Recent chats</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {folderConversations.length} {folderConversations.length === 1 ? "chat" : "chats"}
              </p>
            </div>
          </div>
          {error ? (
            <div className="mt-4 rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-600">
              {error}
            </div>
          ) : null}
          {folderConversations.length === 0 ? (
            <div className="mt-6 rounded-2xl border border-dashed border-border px-5 py-10 text-center">
              <MessagesSquare className="mx-auto size-7 text-muted-foreground" />
              <p className="mt-2 text-sm font-medium">No chats in this folder yet</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Use the composer above to start the first one.
              </p>
            </div>
          ) : (
            <div className="mt-2 divide-y divide-border/70">
              {folderConversations.map((conversation) => (
                <div
                  key={conversation.id}
                  className="group flex min-h-16 items-center gap-3 rounded-xl px-3 py-3 transition hover:bg-muted"
                >
                  <div className="min-w-0 flex-1">
                    {editingConversationID === conversation.id ? (
                      <input
                        autoFocus
                        value={conversationDraft}
                        onChange={(event) => setConversationDraft(event.target.value)}
                        onBlur={() => void commitConversationRename(conversation)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") event.currentTarget.blur();
                          if (event.key === "Escape") setEditingConversationID(null);
                        }}
                        className="w-full bg-transparent text-sm font-medium outline-none"
                      />
                    ) : (
                      <button
                        type="button"
                        onClick={() => void openConversation(conversation.id)}
                        className="block w-full truncate text-left text-sm font-medium text-foreground focus:outline-none"
                      >
                        {conversation.title || "Untitled"}
                      </button>
                    )}
                    <span className="mt-1 block text-xs text-muted-foreground">
                      {formatUpdatedAt(conversation.updated_at)} ·{" "}
                      {runtimes.find((runtime) => runtime.id === conversation.runtime_id)?.label ||
                        conversation.runtime_id}
                    </span>
                  </div>
                  <ConversationActionsMenu
                    conversation={conversation}
                    folders={folders}
                    onRename={() => {
                      setEditingConversationID(conversation.id);
                      setConversationDraft(conversation.title || "Untitled");
                    }}
                    onDelete={() =>
                      setDeleteTarget({
                        kind: "conversation",
                        id: conversation.id,
                        title: conversation.title || "Untitled",
                      })
                    }
                    onMove={(destination) => void moveChat(conversation.id, destination)}
                    triggerClassName="opacity-60 sm:opacity-0"
                  />
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        title={
          deleteTarget?.kind === "folder" ? "Delete folder and chats?" : "Delete conversation?"
        }
        description={
          deleteTarget?.kind === "folder" ? (
            <>
              <span className="font-medium text-foreground">{deleteTarget.title}</span> and all
              chats inside it will be permanently deleted. Stop any running answers first.
            </>
          ) : (
            <>
              <span className="font-medium text-foreground">{deleteTarget?.title}</span> will be
              permanently deleted.
            </>
          )
        }
        busy={deleting}
        error={error}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
            setError(null);
          }
        }}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}

function formatUpdatedAt(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Recently updated";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
