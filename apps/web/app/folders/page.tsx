"use client";

import { Button, Card, Chip, Input, Label, TextField } from "@heroui/react";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { FolderOpen, Folders, Loader2, MessagesSquare, Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useCocola } from "@/app/runtime-provider";
import {
  WorkspaceCatalogCard,
  WorkspaceCatalogGrid,
  WorkspaceEntitySheet,
  WorkspacePageAction,
  WorkspacePageFrame,
  WorkspacePageHeader,
  WorkspaceSectionHeader,
} from "@/components/heroui-workspace/workspace-ui";

function relativeDate(iso: string) {
  const timestamp = Date.parse(iso);
  return Number.isNaN(timestamp) ? "recently" : new Date(timestamp).toLocaleDateString();
}

export default function FoldersPage() {
  const router = useRouter();
  const { conversations, folders, foldersLoaded, createFolder } = useCocola();
  const [isCreating, setIsCreating] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    if (params.get("new") !== "1") return;
    setIsCreating(true);
    params.delete("new");
    const query = params.toString();
    window.history.replaceState(null, "", `/folders${query ? `?${query}` : ""}`);
  }, []);

  const folderRows = useMemo(() => {
    const chatCounts = new Map<string, number>();
    for (const conversation of conversations) {
      if (!conversation.folder_id || conversation.chat_type === "scheduled_task") continue;
      chatCounts.set(conversation.folder_id, (chatCounts.get(conversation.folder_id) ?? 0) + 1);
    }
    return [...folders]
      .sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at))
      .map((folder) => ({ ...folder, chatCount: chatCounts.get(folder.id) ?? 0 }));
  }, [conversations, folders]);

  const changeOpen = (next: boolean) => {
    if (busy) return;
    setIsCreating(next);
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
      setIsCreating(false);
      setName("");
      router.push(`/folders/${encodeURIComponent(folder.id)}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not create folder");
    } finally {
      setBusy(false);
    }
  };

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        action={
          <WorkspacePageAction onPress={() => setIsCreating(true)}>
            <Plus className="size-4" />
            New folder
          </WorkspacePageAction>
        }
        description="Organize independent chats without changing their workspace."
        icon={<Folders className="size-5" />}
        title="Folders"
      />

      <WorkspaceSectionHeader
        description={`${folderRows.length} conversation group${folderRows.length === 1 ? "" : "s"}`}
        title="All folders"
      />

      {!foldersLoaded ? (
        <div className="grid min-h-48 place-items-center">
          <Loader2 className="text-muted size-5 animate-spin" />
        </div>
      ) : folderRows.length ? (
        <WorkspaceCatalogGrid>
          {folderRows.map((folder) => (
            <WorkspaceCatalogCard
              key={folder.id}
              description="Keep related conversations together without changing their workspace."
              footerLabel="Open folder"
              footerMeta={`Updated ${relativeDate(folder.updated_at)}`}
              href={`/folders/${encodeURIComponent(folder.id)}`}
              icon={<FolderOpen className="size-5" />}
              iconClassName="bg-amber-500/15 text-amber-500"
              metadata={
                <Chip size="sm" variant="soft">
                  <MessagesSquare className="size-3.5" />
                  {folder.chatCount} {folder.chatCount === 1 ? "conversation" : "conversations"}
                </Chip>
              }
              status={
                <Chip size="sm" variant="soft">
                  Workspace
                </Chip>
              }
              title={folder.name}
            />
          ))}
        </WorkspaceCatalogGrid>
      ) : (
        <Card className="p-5">
          <EmptyState size="sm">
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <FolderOpen className="text-amber-500" />
              </EmptyState.Media>
              <EmptyState.Title>No folders yet</EmptyState.Title>
              <EmptyState.Description>
                Create a folder to organize related chats.
              </EmptyState.Description>
            </EmptyState.Header>
            <EmptyState.Content>
              <Button size="sm" variant="outline" onPress={() => setIsCreating(true)}>
                <Plus className="size-4" />
                New folder
              </Button>
            </EmptyState.Content>
          </EmptyState>
        </Card>
      )}

      <WorkspaceEntitySheet
        description="Create a lightweight group for related conversations."
        isOpen={isCreating}
        title="New folder"
        onOpenChange={changeOpen}
      >
        <form className="grid gap-5" onSubmit={submit}>
          <TextField autoFocus value={name} onChange={setName}>
            <Label>Folder name</Label>
            <Input placeholder="e.g. Product launch" />
          </TextField>
          {error ? (
            <p className="text-danger text-sm" role="alert">
              {error}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button isDisabled={busy} variant="ghost" onPress={() => changeOpen(false)}>
              Cancel
            </Button>
            <Button isDisabled={busy || !name.trim()} type="submit" variant="primary">
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
              Create folder
            </Button>
          </div>
        </form>
      </WorkspaceEntitySheet>
    </WorkspacePageFrame>
  );
}
