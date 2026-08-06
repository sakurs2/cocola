"use client";

import { Button, Card, Chip, Dropdown, Input, Label, TextField } from "@heroui/react";
import { ListView } from "@cocola/ui-compat/list-view";
import {
  ArrowLeft,
  Bot,
  CalendarDays,
  Ellipsis,
  Folder,
  FolderOpen,
  MessagesSquare,
  Pencil,
  Trash2,
} from "lucide-react";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useCocola, type ConversationSummary } from "@/app/runtime-provider";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
import { ConversationComposer } from "@/components/assistant-ui/thread";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";

type DeleteTarget = { kind: "folder" | "conversation"; id: string; title: string };

export default function FolderPage() {
  const { id: folderID } = useParams<{ id: string }>();
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
  const folderInputRef = useRef<HTMLInputElement>(null);
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
    )
      router.push("/");
  }, [activeSessionId, folderID, router, runningSessionIds]);

  useEffect(() => {
    if (!editingFolder) return;
    const frame = requestAnimationFrame(() => {
      folderInputRef.current?.focus();
      folderInputRef.current?.select();
    });
    return () => cancelAnimationFrame(frame);
  }, [editingFolder]);

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
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not rename chat");
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
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not rename folder");
    }
  };

  const moveChat = async (conversationID: string, destination: string | null) => {
    try {
      await moveConversation(conversationID, destination);
      setError(null);
      showSuccess(
        `Moved to ${destination ? folders.find((item) => item.id === destination)?.name || "folder" : "Chats"}`,
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not move conversation");
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
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not delete item");
    } finally {
      setDeleting(false);
    }
  };

  if (!folder && foldersLoaded) {
    return (
      <div className="cocola-web-page mx-auto grid min-h-72 max-w-4xl place-items-center p-8 text-center">
        <div>
          <Folder className="text-muted mx-auto size-9" />
          <h1 className="mt-3 text-lg font-semibold">Folder not found</h1>
          <p className="text-muted mt-1 text-sm">
            It may have been deleted or belongs to another account.
          </p>
          <Button className="mt-4" onPress={() => router.push("/folders")}>
            Back to folders
          </Button>
        </div>
      </div>
    );
  }
  if (!folder) return <div className="min-h-64" />;

  return (
    <div className="cocola-web-page mx-auto flex w-full max-w-5xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-center gap-3">
        <Button
          isIconOnly
          aria-label="Back to Folders"
          variant="ghost"
          onPress={() => router.push("/folders")}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span className="bg-amber-500/15 text-amber-600 flex size-11 items-center justify-center rounded-2xl dark:text-amber-300">
          <FolderOpen className="size-5" />
        </span>
        <div className="flex h-11 min-w-0 flex-1 items-center">
          {editingFolder ? (
            <TextField
              className="h-11 w-full max-w-sm"
              value={folderDraft}
              onChange={setFolderDraft}
            >
              <Label className="sr-only">Folder name</Label>
              <Input
                ref={folderInputRef}
                aria-label="Folder name"
                className="h-11 py-0 text-2xl font-semibold leading-11 tracking-[-0.03em]"
                onBlur={() => void commitFolderRename()}
                onKeyDown={(event) => {
                  if (event.key === "Enter") event.currentTarget.blur();
                  if (event.key === "Escape") setEditingFolder(false);
                }}
              />
            </TextField>
          ) : (
            <div className="min-w-0">
              <h1 className="truncate text-2xl font-semibold leading-9 tracking-[-0.03em]">
                {folder.name}
              </h1>
              <p className="text-muted mt-1 text-sm">
                {folderConversations.length} conversation
                {folderConversations.length === 1 ? "" : "s"}
              </p>
            </div>
          )}
        </div>
        <div className="flex shrink-0 gap-2">
          {!editingFolder ? (
            <Button
              size="sm"
              variant="outline"
              onPress={() => {
                setFolderDraft(folder.name);
                setEditingFolder(true);
              }}
            >
              <Pencil className="size-3.5" />
              Rename
            </Button>
          ) : null}
          <Button
            size="sm"
            variant="danger-soft"
            onPress={() => setDeleteTarget({ kind: "folder", id: folder.id, title: folder.name })}
          >
            <Trash2 className="size-3.5" />
            Delete
          </Button>
        </div>
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>Start a chat in {folder.name}</Card.Title>
          <Card.Description>New conversations are automatically filed here.</Card.Description>
        </Card.Header>
        <Card.Content className="mt-4 p-0">
          <ConversationComposer placeholder={`Start a chat in ${folder.name}...`} />
        </Card.Content>
      </Card>

      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-semibold">Recent chats</h2>
          <p className="text-muted mt-1 text-sm">
            Scheduled tasks are excluded from folder chat counts.
          </p>
        </div>
        <Chip size="sm" variant="soft">
          {folderConversations.length}
        </Chip>
      </div>

      {folderConversations.length ? (
        <ListView
          aria-label="Folder conversations"
          items={folderConversations}
          selectionMode="none"
          variant="primary"
          onAction={(key) => {
            if (String(key) !== editingConversationID) void openConversation(String(key));
          }}
        >
          {(conversation) => (
            <ListView.Item id={conversation.id} textValue={conversation.title || "Untitled"}>
              <ListView.ItemContent>
                <ConversationIcon conversation={conversation} />
                {editingConversationID === conversation.id ? (
                  <TextField
                    className="min-w-0 flex-1"
                    value={conversationDraft}
                    onChange={setConversationDraft}
                  >
                    <Label className="sr-only">Chat name</Label>
                    <Input
                      autoFocus
                      aria-label="Chat name"
                      className="h-9"
                      onBlur={() => void commitConversationRename(conversation)}
                      onKeyDown={(event) => {
                        event.stopPropagation();
                        if (event.key === "Enter") event.currentTarget.blur();
                        if (event.key === "Escape") setEditingConversationID(null);
                      }}
                      onPointerDown={(event) => event.stopPropagation()}
                    />
                  </TextField>
                ) : (
                  <div className="flex min-w-0 flex-col">
                    <ListView.Title>{conversation.title || "Untitled"}</ListView.Title>
                    <ListView.Description>
                      {formatUpdatedAt(conversation.updated_at)} ·{" "}
                      {runtimes.find((runtime) => runtime.id === conversation.runtime_id)?.label ||
                        conversation.runtime_id}
                    </ListView.Description>
                  </div>
                )}
              </ListView.ItemContent>
              <ListView.ItemAction>
                <Dropdown>
                  <Dropdown.Trigger
                    aria-label={`Actions for ${conversation.title || "Untitled"}`}
                    className="text-muted grid size-8 place-items-center rounded-xl"
                  >
                    <Ellipsis className="size-4" />
                  </Dropdown.Trigger>
                  <Dropdown.Popover placement="bottom end">
                    <Dropdown.Menu
                      aria-label="Conversation actions"
                      onAction={(key) => {
                        const action = String(key);
                        if (action === "rename") {
                          setEditingConversationID(conversation.id);
                          setConversationDraft(conversation.title || "Untitled");
                        } else if (action === "delete")
                          setDeleteTarget({
                            kind: "conversation",
                            id: conversation.id,
                            title: conversation.title || "Untitled",
                          });
                        else if (action === "move-root") void moveChat(conversation.id, null);
                        else if (action.startsWith("move:"))
                          void moveChat(conversation.id, action.slice(5));
                      }}
                    >
                      <Dropdown.Item id="rename" textValue="Rename">
                        Rename
                      </Dropdown.Item>
                      <Dropdown.Item id="move-root" textValue="Move to Chats">
                        Move to Chats
                      </Dropdown.Item>
                      {folders
                        .filter((item) => item.id !== folder.id)
                        .map((item) => (
                          <Dropdown.Item
                            key={item.id}
                            id={`move:${item.id}`}
                            textValue={`Move to ${item.name}`}
                          >
                            Move to {item.name}
                          </Dropdown.Item>
                        ))}
                      <Dropdown.Item id="delete" textValue="Delete">
                        Delete
                      </Dropdown.Item>
                    </Dropdown.Menu>
                  </Dropdown.Popover>
                </Dropdown>
              </ListView.ItemAction>
            </ListView.Item>
          )}
        </ListView>
      ) : (
        <Card className="border-separator min-h-40 border border-dashed p-6">
          <Card.Content className="text-muted flex flex-col items-center justify-center gap-2 p-0 text-center">
            <MessagesSquare className="size-6" />
            <span className="text-sm font-medium">No chats in this folder yet</span>
            <span className="text-xs">Use the composer above to start the first one.</span>
          </Card.Content>
        </Card>
      )}

      <DeleteConfirmDialog
        busy={deleting}
        confirmLabel="Delete"
        description={
          deleteTarget?.kind === "folder"
            ? `${deleteTarget.title} and every chat inside it will be permanently deleted.`
            : `${deleteTarget?.title || "This conversation"} will be permanently deleted.`
        }
        error={error}
        open={deleteTarget !== null}
        title={
          deleteTarget?.kind === "folder" ? "Delete folder and chats?" : "Delete conversation?"
        }
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

function ConversationIcon({ conversation }: { conversation: ConversationSummary }) {
  if (conversation.agent)
    return (
      <span className="bg-cyan-500/15 text-cyan-600 flex size-9 shrink-0 items-center justify-center rounded-xl dark:text-cyan-300">
        <Bot className="size-4" />
      </span>
    );
  if (conversation.chat_type === "scheduled_task")
    return (
      <span className="bg-amber-500/15 text-amber-600 flex size-9 shrink-0 items-center justify-center rounded-xl dark:text-amber-300">
        <CalendarDays className="size-4" />
      </span>
    );
  return (
    <span className="bg-blue-500/15 text-blue-600 flex size-9 shrink-0 items-center justify-center rounded-xl dark:text-blue-300">
      <MessagesSquare className="size-4" />
    </span>
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
