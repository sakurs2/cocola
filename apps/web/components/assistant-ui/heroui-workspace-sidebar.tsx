"use client";

import {
  ArrowRotateRight,
  BookOpen,
  Calendar,
  Check,
  ChevronsLeft,
  CircleCheck,
  CircleQuestionFill,
  Comments,
  Ellipsis,
  FaceRobot,
  Folder,
  FolderOpen,
  Folders,
  Gear,
  Link,
  Pencil,
  PlugConnection,
  Plus,
  ShieldCheck,
  Sparkles,
  TrashBin,
} from "@gravity-ui/icons";
import { Button, Dropdown, Tooltip } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import { Sidebar } from "@cocola/ui-compat/sidebar";
import { useSession } from "next-auth/react";
import { usePathname, useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { type ComponentType, useCallback, useEffect, useState } from "react";

import {
  useCocola,
  type ConversationFolder,
  type ConversationSummary,
} from "@/app/runtime-provider";
import { CocolaCoreLogo } from "@/components/cocola-core-logo";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
import { useWorkspaceUnsavedChanges } from "@/components/assistant-ui/workspace-unsaved-changes";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";

type WorkspaceNavigationItem = {
  adminOnly?: boolean;
  href: string;
  icon: ComponentType<{ className?: string }>;
  iconClassName: string;
  labelKey:
    | "tasks"
    | "agents"
    | "skills"
    | "mcps"
    | "wiki"
    | "connectors"
    | "admin"
    | "projects"
    | "folders";
};

const WORKSPACE_NAVIGATION: WorkspaceNavigationItem[] = [
  { href: "/tasks", icon: Calendar, iconClassName: "text-blue-600", labelKey: "tasks" },
  { href: "/agents", icon: FaceRobot, iconClassName: "text-cyan-600", labelKey: "agents" },
  { href: "/skills", icon: Sparkles, iconClassName: "text-violet-600", labelKey: "skills" },
  { href: "/mcps", icon: PlugConnection, iconClassName: "text-orange-600", labelKey: "mcps" },
  { href: "/wiki", icon: BookOpen, iconClassName: "text-blue-600", labelKey: "wiki" },
  { href: "/connectors", icon: Link, iconClassName: "text-emerald-600", labelKey: "connectors" },
  {
    adminOnly: true,
    href: "/admin",
    icon: ShieldCheck,
    iconClassName: "text-slate-500",
    labelKey: "admin",
  },
];

type WorkspaceResourceItem = WorkspaceNavigationItem & { createHref: string };

const WORKSPACE_RESOURCES: WorkspaceResourceItem[] = [
  {
    createHref: "/projects/new",
    href: "/projects",
    icon: Folder,
    iconClassName: "text-indigo-600",
    labelKey: "projects",
  },
  {
    createHref: "/folders?new=1",
    href: "/folders",
    icon: Folders,
    iconClassName: "text-amber-500",
    labelKey: "folders",
  },
];

export function HeroUIWorkspaceSidebar({
  immersive,
  onPeekChange,
  onToggleImmersive,
}: {
  immersive: boolean;
  onPeekChange: (peeked: boolean) => void;
  onToggleImmersive: () => void;
}) {
  const t = useTranslations("navigation");
  return (
    <>
      <Sidebar
        onMouseEnter={() => {
          if (immersive) onPeekChange(true);
        }}
        onMouseLeave={() => {
          if (immersive) onPeekChange(false);
        }}
      >
        <HeroUIWorkspaceSidebarContents onToggleImmersive={onToggleImmersive} />
      </Sidebar>
      <Sidebar.Mobile>
        <Sheet.Heading className="sr-only">{t("sidebarAria")}</Sheet.Heading>
        <HeroUIWorkspaceSidebarContents idPrefix="mobile-" onToggleImmersive={onToggleImmersive} />
      </Sidebar.Mobile>
    </>
  );
}

function HeroUIWorkspaceSidebarContents({
  idPrefix = "",
  onToggleImmersive,
}: {
  idPrefix?: string;
  onToggleImmersive: () => void;
}) {
  const t = useTranslations("navigation");
  const { data: session } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const { runWithNavigationGuard } = useWorkspaceUnsavedChanges();
  const { showError, showSuccess } = useWorkspaceToast();
  const {
    activeSessionId,
    conversations,
    deleteConversation,
    folders,
    loadConversation,
    moveConversation,
    newConversation,
    renameConversation,
    runningSessionIds,
    unreadCompletedSessionIds,
  } = useCocola();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; title: string } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [taskCount, setTaskCount] = useState<number | null>(null);

  const isAdmin = session?.user?.role === "admin";
  const userLabel = session?.user?.name || session?.user?.email || t("user");
  const userSubtitle = session?.user?.email || session?.user?.role || t("workspaceMember");
  const userInitial = userLabel.trim().slice(0, 1).toUpperCase() || "U";
  const visibleWorkspaceNavigation = WORKSPACE_NAVIGATION.filter(
    (item) => !item.adminOnly || isAdmin,
  );
  const recentConversations = conversations;

  useEffect(() => {
    let cancelled = false;
    void fetch("/api/scheduled-tasks")
      .then(async (response) => {
        if (!response.ok) return null;
        return (await response.json()) as { tasks?: unknown[] };
      })
      .then((payload) => {
        if (!cancelled && payload?.tasks) setTaskCount(payload.tasks.length);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  const navigate = useCallback(
    (href: string) => {
      if (pathname === href || pathname.startsWith(`${href}/`)) return;
      runWithNavigationGuard(() => router.push(href));
    },
    [pathname, router, runWithNavigationGuard],
  );

  const openNewChat = () => {
    runWithNavigationGuard(() => {
      newConversation();
      if (pathname !== "/") router.push("/");
    });
  };

  const openConversation = (conversation: ConversationSummary) => {
    runWithNavigationGuard(async () => {
      if (conversation.project_id) {
        router.push(
          `/projects/${encodeURIComponent(conversation.project_id)}/tasks/${encodeURIComponent(conversation.id)}`,
        );
        return;
      }
      if (pathname !== "/") {
        router.push(`/?conversation=${encodeURIComponent(conversation.id)}`);
        return;
      }
      await loadConversation(conversation.id);
    });
  };

  const commitRename = async (conversationId: string) => {
    const title = draftTitle.trim();
    setEditingId(null);
    if (!title) return;
    try {
      await renameConversation(conversationId, title);
    } catch (error) {
      showError(error instanceof Error ? error.message : t("renameFailed"));
    }
  };

  const handleConversationAction = async (conversation: ConversationSummary, action: string) => {
    if (action === "rename") {
      setDraftTitle(conversation.title || t("untitled"));
      setEditingId(conversation.id);
      return;
    }
    if (action === "delete") {
      setDeleteError(null);
      setDeleteTarget({ id: conversation.id, title: conversation.title || t("untitled") });
      return;
    }
    if (action === "move-root") {
      await moveConversation(conversation.id, null);
      showSuccess(t("movedChats"));
      return;
    }
    if (action.startsWith("move:")) {
      const folderId = action.slice("move:".length);
      await moveConversation(conversation.id, folderId);
      const folder = folders.find((item) => item.id === folderId);
      showSuccess(t("movedFolder", { name: folder?.name || t("folder") }));
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await deleteConversation(deleteTarget.id);
      setDeleteTarget(null);
    } catch (error) {
      setDeleteError(error instanceof Error ? error.message : t("deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <>
      <Sidebar.Header>
        <div className="flex items-center gap-3 px-1 py-1.5">
          <CocolaCoreLogo className="size-10 shrink-0" />
          <div className="flex min-w-0 flex-1 flex-col" data-sidebar="label">
            <span className="text-foreground text-sm font-semibold leading-tight">cocola</span>
            <span className="text-muted text-xs leading-tight">{t("agentWorkspace")}</span>
          </div>
          <Tooltip delay={0}>
            <Button
              isIconOnly
              aria-label={t("enterImmersive")}
              aria-pressed="false"
              className="shrink-0"
              size="sm"
              variant="ghost"
              onPress={onToggleImmersive}
            >
              <ChevronsLeft className="size-4" />
            </Button>
            <Tooltip.Content>{t("enterImmersive")}</Tooltip.Content>
          </Tooltip>
        </div>
      </Sidebar.Header>

      <Sidebar.Content>
        <Sidebar.Group>
          <Button
            className="cocola-web-new-chat h-11 w-full justify-start gap-2.5 rounded-2xl px-2.5"
            onPress={openNewChat}
          >
            <span className="cocola-web-new-chat-icon grid size-7 shrink-0 place-items-center rounded-xl">
              <Plus className="size-4" />
            </span>
            <span className="text-sm font-semibold">{t("newChat")}</span>
          </Button>
        </Sidebar.Group>

        <Sidebar.Group>
          <Sidebar.Menu aria-label={t("workspaceTools")}>
            {visibleWorkspaceNavigation.map((item) => (
              <WorkspaceSidebarItem
                key={item.href}
                id={`${idPrefix}${item.labelKey}`}
                item={item}
                isCurrent={pathname === item.href || pathname.startsWith(`${item.href}/`)}
                taskCount={item.href === "/tasks" ? taskCount : null}
                onPress={() => navigate(item.href)}
              />
            ))}
          </Sidebar.Menu>
        </Sidebar.Group>

        <Sidebar.Group>
          <Sidebar.GroupLabel>{t("workspaceGroup")}</Sidebar.GroupLabel>
          <Sidebar.Menu aria-label={t("workspaceResources")}>
            {WORKSPACE_RESOURCES.map((item) => (
              <WorkspaceSidebarItem
                key={item.href}
                createHref={item.createHref}
                id={`${idPrefix}${item.labelKey}`}
                item={item}
                isCurrent={pathname === item.href || pathname.startsWith(`${item.href}/`)}
                onCreate={() => navigate(item.createHref)}
                onPress={() => navigate(item.href)}
              />
            ))}
          </Sidebar.Menu>
        </Sidebar.Group>

        <Sidebar.Group>
          <Sidebar.GroupLabel>{t("chats")}</Sidebar.GroupLabel>
          <Sidebar.Menu aria-label={t("recentChats")}>
            {recentConversations.map((conversation) => (
              <ConversationSidebarItem
                key={conversation.id}
                conversation={conversation}
                draftTitle={draftTitle}
                editing={editingId === conversation.id}
                folders={folders}
                idPrefix={idPrefix}
                isCurrent={
                  conversation.project_id
                    ? pathname === `/projects/${conversation.project_id}/tasks/${conversation.id}`
                    : pathname === "/" && activeSessionId === conversation.id
                }
                requiresUserAction={conversation.requires_user_action === true}
                running={runningSessionIds.has(conversation.id)}
                unread={unreadCompletedSessionIds.has(conversation.id)}
                onAction={(action) => void handleConversationAction(conversation, action)}
                onCancelRename={() => setEditingId(null)}
                onCommitRename={() => void commitRename(conversation.id)}
                onDraftTitleChange={setDraftTitle}
                onOpen={() => openConversation(conversation)}
              />
            ))}
          </Sidebar.Menu>
        </Sidebar.Group>
      </Sidebar.Content>

      <Sidebar.Footer>
        <Button
          className="h-auto w-full items-center justify-start gap-3 rounded-2xl px-2 py-2 text-left"
          variant="ghost"
          onPress={() => navigate("/profile")}
        >
          <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-blue-100 text-xs font-semibold text-blue-700 dark:bg-blue-950 dark:text-blue-300">
            {userInitial}
          </span>
          <span className="min-w-0 flex-1 text-left" data-sidebar="label">
            <span className="block truncate text-sm font-medium leading-5">{userLabel}</span>
            <span className="text-muted block truncate text-xs leading-4">{userSubtitle}</span>
          </span>
          <Gear className="text-muted ml-auto size-4" />
        </Button>
      </Sidebar.Footer>

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        title={t("deleteConversationTitle")}
        description={t("deleteConversationDescription", { title: deleteTarget?.title ?? "" })}
        busy={deleting}
        error={deleteError}
        onOpenChange={(open) => {
          if (open) return;
          setDeleteTarget(null);
          setDeleteError(null);
        }}
        onConfirm={() => void confirmDelete()}
      />
    </>
  );
}

function WorkspaceSidebarItem({
  createHref,
  id,
  isCurrent,
  item,
  onCreate,
  onPress,
  taskCount,
}: {
  createHref?: string;
  id: string;
  isCurrent: boolean;
  item: WorkspaceNavigationItem;
  onCreate?: () => void;
  onPress: () => void;
  taskCount?: number | null;
}) {
  const t = useTranslations("navigation");
  const Icon = item.icon;
  const label = t(item.labelKey);
  return (
    <Sidebar.MenuItem
      className="cocola-sidebar-tab"
      id={id}
      isCurrent={isCurrent}
      textValue={label}
      onAction={onPress}
    >
      <Sidebar.MenuIcon className="cocola-sidebar-tab-icon">
        <Icon className={`size-4 ${item.iconClassName}`} />
      </Sidebar.MenuIcon>
      <Sidebar.MenuLabel>{label}</Sidebar.MenuLabel>
      {taskCount !== null && taskCount !== undefined ? (
        <span
          className="bg-default text-muted ml-auto rounded-full px-2 py-0.5 text-[11px]"
          data-sidebar="label"
        >
          {taskCount}
        </span>
      ) : null}
      {createHref ? (
        <Sidebar.MenuActions className="cocola-sidebar-create-actions">
          <Button
            isIconOnly
            aria-label={t("createItem", { name: label })}
            className="size-7 min-h-7 min-w-7 p-0"
            variant="ghost"
            onPress={onCreate}
          >
            <Plus className="size-3.5" />
          </Button>
        </Sidebar.MenuActions>
      ) : null}
    </Sidebar.MenuItem>
  );
}

function ConversationSidebarItem({
  conversation,
  draftTitle,
  editing,
  folders,
  idPrefix,
  isCurrent,
  onAction,
  onCancelRename,
  onCommitRename,
  onDraftTitleChange,
  onOpen,
  requiresUserAction,
  running,
  unread,
}: {
  conversation: ConversationSummary;
  draftTitle: string;
  editing: boolean;
  folders: ConversationFolder[];
  idPrefix: string;
  isCurrent: boolean;
  onAction: (action: string) => void;
  onCancelRename: () => void;
  onCommitRename: () => void;
  onDraftTitleChange: (title: string) => void;
  onOpen: () => void;
  requiresUserAction: boolean;
  running: boolean;
  unread: boolean;
}) {
  const t = useTranslations("navigation");
  const title = conversation.title || t("untitled");
  return (
    <Sidebar.MenuItem
      id={`${idPrefix}conversation-${conversation.id}`}
      isCurrent={isCurrent}
      textValue={title}
      onAction={editing ? undefined : onOpen}
    >
      <Sidebar.MenuIcon>
        <ConversationTypeIcon conversation={conversation} />
      </Sidebar.MenuIcon>
      <Sidebar.MenuLabel>
        {editing ? (
          <input
            autoFocus
            aria-label={t("conversationTitle")}
            className="cocola-sidebar-rename-input border-separator bg-surface h-6 w-full min-w-0 rounded-md border px-1.5 py-0 text-sm leading-5 outline-none focus:border-[var(--focus)]"
            value={draftTitle}
            onBlur={onCommitRename}
            onChange={(event) => onDraftTitleChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                onCommitRename();
              }
              if (event.key === "Escape") {
                event.preventDefault();
                onCancelRename();
              }
            }}
          />
        ) : (
          <span className="block truncate">{title}</span>
        )}
      </Sidebar.MenuLabel>
      {!editing ? (
        <Sidebar.MenuActions className="flex items-center gap-0.5">
          {running ? (
            <ArrowRotateRight
              aria-label={t("agentAnswering")}
              className="text-accent size-3.5 animate-spin"
            />
          ) : requiresUserAction ? (
            <Tooltip delay={0}>
              <span
                aria-label={t("waitingConfirmation")}
                className="text-warning grid size-5 place-items-center"
                role="img"
              >
                <CircleQuestionFill className="size-3.5" />
              </span>
              <Tooltip.Content>{t("waitingConfirmation")}</Tooltip.Content>
            </Tooltip>
          ) : null}
          {unread && !running && !requiresUserAction ? (
            <CircleCheck aria-label={t("answerCompleted")} className="size-3.5 text-emerald-500" />
          ) : null}
          <Dropdown>
            <Dropdown.Trigger
              aria-label={t("actionsFor", { title })}
              className="text-muted hover:text-foreground grid size-7 place-items-center rounded-lg"
            >
              <Ellipsis className="size-3.5" />
            </Dropdown.Trigger>
            <Dropdown.Popover placement="bottom end">
              <Dropdown.Menu
                aria-label={t("actionsFor", { title })}
                onAction={(key) => onAction(String(key))}
              >
                <Dropdown.Section>
                  <Dropdown.Item id="rename" textValue={t("rename")}>
                    <Pencil className="text-muted size-4 shrink-0" />
                    <span data-slot="label">{t("rename")}</span>
                  </Dropdown.Item>
                  {conversation.chat_type !== "scheduled_task" && !conversation.project_id ? (
                    <Dropdown.SubmenuTrigger>
                      <Dropdown.Item id="move" textValue={t("moveFolder")}>
                        <FolderOpen className="text-muted size-4 shrink-0" />
                        <span data-slot="label">{t("moveFolder")}</span>
                        <Dropdown.SubmenuIndicator />
                      </Dropdown.Item>
                      <Dropdown.Popover placement="right top">
                        <Dropdown.Menu
                          aria-label={t("moveToFolder", { title })}
                          onAction={(key) => onAction(String(key))}
                        >
                          <Dropdown.Item id="move-root" textValue={t("noFolder")}>
                            <Folder className="text-muted size-4 shrink-0" />
                            <span className="min-w-0 flex-1 truncate" data-slot="label">
                              {t("noFolder")}
                            </span>
                            {!conversation.folder_id ? (
                              <Check className="text-accent size-4 shrink-0" />
                            ) : null}
                          </Dropdown.Item>
                          {folders.map((folder) => (
                            <Dropdown.Item
                              key={folder.id}
                              id={`move:${folder.id}`}
                              textValue={folder.name}
                            >
                              <Folder className="text-muted size-4 shrink-0" />
                              <span className="min-w-0 flex-1 truncate" data-slot="label">
                                {folder.name}
                              </span>
                              {conversation.folder_id === folder.id ? (
                                <Check className="text-accent size-4 shrink-0" />
                              ) : null}
                            </Dropdown.Item>
                          ))}
                        </Dropdown.Menu>
                      </Dropdown.Popover>
                    </Dropdown.SubmenuTrigger>
                  ) : null}
                </Dropdown.Section>
                <Dropdown.Section className="border-separator mt-1 border-t pt-1">
                  <Dropdown.Item id="delete" textValue={t("delete")} variant="danger">
                    <TrashBin className="size-4 shrink-0" />
                    <span data-slot="label">{t("delete")}</span>
                  </Dropdown.Item>
                </Dropdown.Section>
              </Dropdown.Menu>
            </Dropdown.Popover>
          </Dropdown>
        </Sidebar.MenuActions>
      ) : null}
    </Sidebar.MenuItem>
  );
}

function ConversationTypeIcon({ conversation }: { conversation: ConversationSummary }) {
  if (conversation.project_id) {
    return <Folder className="size-4 text-indigo-600 dark:text-indigo-300" />;
  }
  if (conversation.agent_id || conversation.agent) {
    return <FaceRobot className="size-4 text-cyan-600 dark:text-cyan-300" />;
  }
  if (conversation.chat_type === "scheduled_task") {
    return <Calendar className="size-4 text-amber-500 dark:text-amber-300" />;
  }
  return <Comments className="size-4 text-blue-600 dark:text-blue-300" />;
}
