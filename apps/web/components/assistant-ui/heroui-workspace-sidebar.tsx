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
  label: string;
};

const WORKSPACE_NAVIGATION: WorkspaceNavigationItem[] = [
  { href: "/tasks", icon: Calendar, iconClassName: "text-blue-600", label: "Tasks" },
  { href: "/agents", icon: FaceRobot, iconClassName: "text-cyan-600", label: "Agents" },
  { href: "/skills", icon: Sparkles, iconClassName: "text-violet-600", label: "Skills" },
  { href: "/mcps", icon: PlugConnection, iconClassName: "text-orange-600", label: "MCP" },
  { href: "/wiki", icon: BookOpen, iconClassName: "text-blue-600", label: "Wiki" },
  { href: "/connectors", icon: Link, iconClassName: "text-emerald-600", label: "Connectors" },
  {
    adminOnly: true,
    href: "/admin",
    icon: ShieldCheck,
    iconClassName: "text-slate-500",
    label: "Admin",
  },
];

type WorkspaceResourceItem = WorkspaceNavigationItem & { createHref: string };

const WORKSPACE_RESOURCES: WorkspaceResourceItem[] = [
  {
    createHref: "/projects/new",
    href: "/projects",
    icon: Folder,
    iconClassName: "text-indigo-600",
    label: "Projects",
  },
  {
    createHref: "/folders?new=1",
    href: "/folders",
    icon: Folders,
    iconClassName: "text-amber-500",
    label: "Folders",
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
        <Sheet.Heading className="sr-only">Cocola workspace navigation</Sheet.Heading>
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
  const userLabel = session?.user?.name || session?.user?.email || "User";
  const userSubtitle = session?.user?.email || session?.user?.role || "Workspace member";
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
      showError(error instanceof Error ? error.message : "Rename failed. Please try again.");
    }
  };

  const handleConversationAction = async (conversation: ConversationSummary, action: string) => {
    if (action === "rename") {
      setDraftTitle(conversation.title || "Untitled");
      setEditingId(conversation.id);
      return;
    }
    if (action === "delete") {
      setDeleteError(null);
      setDeleteTarget({ id: conversation.id, title: conversation.title || "Untitled" });
      return;
    }
    if (action === "move-root") {
      await moveConversation(conversation.id, null);
      showSuccess("Moved to Chats");
      return;
    }
    if (action.startsWith("move:")) {
      const folderId = action.slice("move:".length);
      await moveConversation(conversation.id, folderId);
      const folder = folders.find((item) => item.id === folderId);
      showSuccess(`Moved to ${folder?.name || "folder"}`);
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
      setDeleteError(error instanceof Error ? error.message : "Delete failed. Please try again.");
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
            <span className="text-muted text-xs leading-tight">agent workspace</span>
          </div>
          <Tooltip delay={0}>
            <Button
              isIconOnly
              aria-label="Enter immersive mode"
              aria-pressed="false"
              className="shrink-0"
              size="sm"
              variant="ghost"
              onPress={onToggleImmersive}
            >
              <ChevronsLeft className="size-4" />
            </Button>
            <Tooltip.Content>Enter immersive mode</Tooltip.Content>
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
            <span className="text-sm font-semibold">New Chat</span>
          </Button>
        </Sidebar.Group>

        <Sidebar.Group>
          <Sidebar.Menu aria-label="Workspace tools">
            {visibleWorkspaceNavigation.map((item) => (
              <WorkspaceSidebarItem
                key={item.href}
                id={`${idPrefix}${item.label.toLowerCase()}`}
                item={item}
                isCurrent={pathname === item.href || pathname.startsWith(`${item.href}/`)}
                taskCount={item.href === "/tasks" ? taskCount : null}
                onPress={() => navigate(item.href)}
              />
            ))}
          </Sidebar.Menu>
        </Sidebar.Group>

        <Sidebar.Group>
          <Sidebar.GroupLabel>Workspace</Sidebar.GroupLabel>
          <Sidebar.Menu aria-label="Workspace resources">
            {WORKSPACE_RESOURCES.map((item) => (
              <WorkspaceSidebarItem
                key={item.href}
                createHref={item.createHref}
                id={`${idPrefix}${item.label.toLowerCase()}`}
                item={item}
                isCurrent={pathname === item.href || pathname.startsWith(`${item.href}/`)}
                onCreate={() => navigate(item.createHref)}
                onPress={() => navigate(item.href)}
              />
            ))}
          </Sidebar.Menu>
        </Sidebar.Group>

        <Sidebar.Group>
          <Sidebar.GroupLabel>Chats</Sidebar.GroupLabel>
          <Sidebar.Menu aria-label="Recent chats">
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
        title="Delete conversation?"
        description={
          <>
            <span className="font-medium text-foreground">{deleteTarget?.title}</span> will be
            permanently deleted. Stop its running answer first.
          </>
        }
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
  const Icon = item.icon;
  return (
    <Sidebar.MenuItem
      className="cocola-sidebar-tab"
      id={id}
      isCurrent={isCurrent}
      textValue={item.label}
      onAction={onPress}
    >
      <Sidebar.MenuIcon className="cocola-sidebar-tab-icon">
        <Icon className={`size-4 ${item.iconClassName}`} />
      </Sidebar.MenuIcon>
      <Sidebar.MenuLabel>{item.label}</Sidebar.MenuLabel>
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
            aria-label={`Create ${item.label.toLowerCase()}`}
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
  const title = conversation.title || "Untitled";
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
            aria-label="Conversation title"
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
              aria-label="Agent is answering"
              className="text-accent size-3.5 animate-spin"
            />
          ) : requiresUserAction ? (
            <Tooltip delay={0}>
              <span
                aria-label="Waiting for your confirmation"
                className="text-warning grid size-5 place-items-center"
                role="img"
              >
                <CircleQuestionFill className="size-3.5" />
              </span>
              <Tooltip.Content>Waiting for your confirmation</Tooltip.Content>
            </Tooltip>
          ) : null}
          {unread && !running && !requiresUserAction ? (
            <CircleCheck aria-label="Answer completed" className="size-3.5 text-emerald-500" />
          ) : null}
          <Dropdown>
            <Dropdown.Trigger
              aria-label={`Actions for ${title}`}
              className="text-muted hover:text-foreground grid size-7 place-items-center rounded-lg"
            >
              <Ellipsis className="size-3.5" />
            </Dropdown.Trigger>
            <Dropdown.Popover placement="bottom end">
              <Dropdown.Menu
                aria-label={`Actions for ${title}`}
                onAction={(key) => onAction(String(key))}
              >
                <Dropdown.Section>
                  <Dropdown.Item id="rename" textValue="Rename">
                    <Pencil className="text-muted size-4 shrink-0" />
                    <span data-slot="label">Rename</span>
                  </Dropdown.Item>
                  {conversation.chat_type !== "scheduled_task" && !conversation.project_id ? (
                    <Dropdown.SubmenuTrigger>
                      <Dropdown.Item id="move" textValue="Move to folder">
                        <FolderOpen className="text-muted size-4 shrink-0" />
                        <span data-slot="label">Move to folder</span>
                        <Dropdown.SubmenuIndicator />
                      </Dropdown.Item>
                      <Dropdown.Popover placement="right top">
                        <Dropdown.Menu
                          aria-label={`Move ${title} to folder`}
                          onAction={(key) => onAction(String(key))}
                        >
                          <Dropdown.Item id="move-root" textValue="No folder">
                            <Folder className="text-muted size-4 shrink-0" />
                            <span className="min-w-0 flex-1 truncate" data-slot="label">
                              No folder
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
                  <Dropdown.Item id="delete" textValue="Delete" variant="danger">
                    <TrashBin className="size-4 shrink-0" />
                    <span data-slot="label">Delete</span>
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
