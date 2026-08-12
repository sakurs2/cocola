"use client";

import { Button, Card, Chip, Input, Label, TextField } from "@heroui/react";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { FolderOpen, Folders, Loader2, MessagesSquare, Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useFormatter, useTranslations } from "next-intl";
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

export default function FoldersPage() {
  const t = useTranslations("workspace.folders");
  const common = useTranslations("common.actions");
  const format = useFormatter();
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
      setError(cause instanceof Error ? cause.message : t("createFailed"));
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
            {t("new")}
          </WorkspacePageAction>
        }
        description={t("description")}
        icon={<Folders className="size-5" />}
        title={t("title")}
      />

      <WorkspaceSectionHeader
        description={t("count", { count: folderRows.length })}
        title={t("all")}
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
              description={t("cardDescription")}
              footerLabel={t("open")}
              footerMeta={t("updated", {
                date: Number.isNaN(Date.parse(folder.updated_at))
                  ? t("recently")
                  : format.dateTime(new Date(folder.updated_at), { dateStyle: "medium" }),
              })}
              href={`/folders/${encodeURIComponent(folder.id)}`}
              icon={<FolderOpen className="size-5" />}
              iconClassName="bg-amber-500/15 text-amber-500"
              metadata={
                <Chip size="sm" variant="soft">
                  <MessagesSquare className="size-3.5" />
                  {t("conversationCount", { count: folder.chatCount })}
                </Chip>
              }
              status={
                <Chip size="sm" variant="soft">
                  {t("workspace")}
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
              <EmptyState.Title>{t("emptyTitle")}</EmptyState.Title>
              <EmptyState.Description>{t("emptyDescription")}</EmptyState.Description>
            </EmptyState.Header>
            <EmptyState.Content>
              <Button size="sm" variant="outline" onPress={() => setIsCreating(true)}>
                <Plus className="size-4" />
                {t("new")}
              </Button>
            </EmptyState.Content>
          </EmptyState>
        </Card>
      )}

      <WorkspaceEntitySheet
        description={t("sheetDescription")}
        isOpen={isCreating}
        title={t("new")}
        onOpenChange={changeOpen}
      >
        <form className="grid gap-5" onSubmit={submit}>
          <TextField autoFocus value={name} onChange={setName}>
            <Label>{t("name")}</Label>
            <Input placeholder={t("namePlaceholder")} />
          </TextField>
          {error ? (
            <p className="text-danger text-sm" role="alert">
              {error}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button isDisabled={busy} variant="ghost" onPress={() => changeOpen(false)}>
              {common("cancel")}
            </Button>
            <Button isDisabled={busy || !name.trim()} type="submit" variant="primary">
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
              {t("create")}
            </Button>
          </div>
        </form>
      </WorkspaceEntitySheet>
    </WorkspacePageFrame>
  );
}
