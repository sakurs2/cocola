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
import { useFormatter, useTranslations } from "next-intl";
import { useCocola, type ConversationSummary } from "@/app/runtime-provider";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
import { ConversationComposer } from "@/components/assistant-ui/thread";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";

type DeleteTarget = { kind: "folder" | "conversation"; id: string; title: string };

export default function FolderPage() {
  const t = useTranslations("workspace.folders.detail");
  const foldersT = useTranslations("workspace.folders");
  const format = useFormatter();
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
  const deleteInFlightRef = useRef(false);
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

  const commitConversationRename = async (conversation: ConversationSummary, draft: string) => {
    const title = draft.trim();
    setEditingConversationID(null);
    if (!title) return;
    try {
      await renameConversation(conversation.id, title);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("renameChatFailed"));
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
      setError(cause instanceof Error ? cause.message : t("renameFolderFailed"));
    }
  };

  const moveChat = async (conversationID: string, destination: string | null) => {
    try {
      await moveConversation(conversationID, destination);
      setError(null);
      showSuccess(
        destination
          ? t("movedFolder", {
              name: folders.find((item) => item.id === destination)?.name || t("folderFallback"),
            })
          : t("movedChats"),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("moveFailed"));
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget || deleteInFlightRef.current) return;
    deleteInFlightRef.current = true;
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
      setError(cause instanceof Error ? cause.message : t("deleteFailed"));
    } finally {
      deleteInFlightRef.current = false;
      setDeleting(false);
    }
  };

  if (!folder && foldersLoaded) {
    return (
      <div className="cocola-web-page mx-auto grid min-h-72 max-w-4xl place-items-center p-8 text-center">
        <div>
          <Folder className="text-muted mx-auto size-9" />
          <h1 className="mt-3 text-lg font-semibold">{t("notFound")}</h1>
          <p className="text-muted mt-1 text-sm">{t("notFoundDescription")}</p>
          <Button className="mt-4" onPress={() => router.push("/folders")}>
            {t("back")}
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
          aria-label={t("back")}
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
              <Label className="sr-only">{t("folderName")}</Label>
              <Input
                ref={folderInputRef}
                aria-label={t("folderName")}
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
                {foldersT("conversationCount", { count: folderConversations.length })}
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
              {t("rename")}
            </Button>
          ) : null}
          <Button
            size="sm"
            variant="danger-soft"
            onPress={() => setDeleteTarget({ kind: "folder", id: folder.id, title: folder.name })}
          >
            <Trash2 className="size-3.5" />
            {t("delete")}
          </Button>
        </div>
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>{t("startTitle", { name: folder.name })}</Card.Title>
          <Card.Description>{t("startDescription")}</Card.Description>
        </Card.Header>
        <Card.Content className="mt-4 p-0">
          <ConversationComposer placeholder={t("startPlaceholder", { name: folder.name })} />
        </Card.Content>
      </Card>

      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-semibold">{t("recent")}</h2>
          <p className="text-muted mt-1 text-sm">{t("recentDescription")}</p>
        </div>
        <Chip size="sm" variant="soft">
          {folderConversations.length}
        </Chip>
      </div>

      {folderConversations.length ? (
        <ListView
          aria-label={t("listAria")}
          dependencies={[editingConversationID]}
          items={folderConversations}
          selectionMode="none"
          variant="primary"
          onAction={(key) => {
            if (String(key) !== editingConversationID) void openConversation(String(key));
          }}
        >
          {(conversation) => (
            <ListView.Item id={conversation.id} textValue={conversation.title || t("untitled")}>
              <ListView.ItemContent>
                <ConversationIcon conversation={conversation} />
                {editingConversationID === conversation.id ? (
                  <ConversationRenameField
                    key={conversation.id}
                    conversation={conversation}
                    onCancel={() => setEditingConversationID(null)}
                    onCommit={(draft) => void commitConversationRename(conversation, draft)}
                  />
                ) : (
                  <div className="flex min-w-0 flex-col">
                    <ListView.Title>{conversation.title || t("untitled")}</ListView.Title>
                    <ListView.Description>
                      {formatUpdatedAt(conversation.updated_at, format, t("recentlyUpdated"))} ·{" "}
                      {runtimes.find((runtime) => runtime.id === conversation.runtime_id)?.label ||
                        conversation.runtime_id}
                    </ListView.Description>
                  </div>
                )}
              </ListView.ItemContent>
              <ListView.ItemAction>
                <Dropdown>
                  <Dropdown.Trigger
                    aria-label={t("actionsFor", { name: conversation.title || t("untitled") })}
                    className="text-muted grid size-8 place-items-center rounded-xl"
                  >
                    <Ellipsis className="size-4" />
                  </Dropdown.Trigger>
                  <Dropdown.Popover placement="bottom end">
                    <Dropdown.Menu
                      aria-label={t("conversationActions")}
                      onAction={(key) => {
                        const action = String(key);
                        if (action === "rename") {
                          setEditingConversationID(conversation.id);
                        } else if (action === "delete")
                          setDeleteTarget({
                            kind: "conversation",
                            id: conversation.id,
                            title: conversation.title || t("untitled"),
                          });
                        else if (action === "move-root") void moveChat(conversation.id, null);
                        else if (action.startsWith("move:"))
                          void moveChat(conversation.id, action.slice(5));
                      }}
                    >
                      <Dropdown.Section>
                        <Dropdown.Item id="rename" textValue={t("rename")}>
                          <Pencil className="text-muted size-4 shrink-0" />
                          <span data-slot="label">{t("rename")}</span>
                        </Dropdown.Item>
                        <Dropdown.Item id="move-root" textValue={t("moveChats")}>
                          <MessagesSquare className="text-muted size-4 shrink-0" />
                          <span data-slot="label">{t("moveChats")}</span>
                        </Dropdown.Item>
                        {folders
                          .filter((item) => item.id !== folder.id)
                          .map((item) => (
                            <Dropdown.Item
                              key={item.id}
                              id={`move:${item.id}`}
                              textValue={t("moveTo", { name: item.name })}
                            >
                              <Folder className="text-muted size-4 shrink-0" />
                              <span className="min-w-0 flex-1 truncate" data-slot="label">
                                {t("moveTo", { name: item.name })}
                              </span>
                            </Dropdown.Item>
                          ))}
                      </Dropdown.Section>
                      <Dropdown.Section className="border-separator mt-1 border-t pt-1">
                        <Dropdown.Item id="delete" textValue={t("delete")} variant="danger">
                          <Trash2 className="size-4 shrink-0" />
                          <span data-slot="label">{t("delete")}</span>
                        </Dropdown.Item>
                      </Dropdown.Section>
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
            <span className="text-sm font-medium">{t("empty")}</span>
            <span className="text-xs">{t("emptyDescription")}</span>
          </Card.Content>
        </Card>
      )}

      <DeleteConfirmDialog
        busy={deleting}
        confirmLabel={t("delete")}
        description={
          deleteTarget?.kind === "folder"
            ? t("deleteFolderDescription", { name: deleteTarget.title })
            : t("deleteConversationDescription", {
                name: deleteTarget?.title || t("thisConversation"),
              })
        }
        error={error}
        open={deleteTarget !== null}
        title={
          deleteTarget?.kind === "folder" ? t("deleteFolderTitle") : t("deleteConversationTitle")
        }
        onOpenChange={(open) => {
          if (!open && !deleteInFlightRef.current) {
            setDeleteTarget(null);
            setError(null);
          }
        }}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}

function ConversationRenameField({
  conversation,
  onCancel,
  onCommit,
}: {
  conversation: ConversationSummary;
  onCancel: () => void;
  onCommit: (draft: string) => void;
}) {
  const t = useTranslations("workspace.folders.detail");
  const inputRef = useRef<HTMLInputElement>(null);
  const composingRef = useRef(false);
  const [draft, setDraft] = useState(conversation.title || t("untitled"));

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  return (
    <TextField className="min-w-0 flex-1" value={draft} onChange={setDraft}>
      <Label className="sr-only">{t("chatName")}</Label>
      <Input
        ref={inputRef}
        aria-label={t("chatName")}
        className="h-9"
        onBlur={(event) => onCommit(event.currentTarget.value)}
        onCompositionEnd={() => {
          composingRef.current = false;
        }}
        onCompositionStart={() => {
          composingRef.current = true;
        }}
        onKeyDown={(event) => {
          event.stopPropagation();
          if (
            composingRef.current ||
            event.nativeEvent.isComposing ||
            event.nativeEvent.keyCode === 229
          )
            return;
          if (event.key === "Enter") event.currentTarget.blur();
          if (event.key === "Escape") onCancel();
        }}
        onPointerDown={(event) => event.stopPropagation()}
      />
    </TextField>
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

function formatUpdatedAt(value: string, format: ReturnType<typeof useFormatter>, fallback: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return fallback;
  return format.dateTime(date, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
