"use client";

import { useCallback, useRef, useState } from "react";
import {
  Cable,
  CalendarCheck as CalendarDots,
  MessagesSquare as ChatsCircle,
  CheckCircle2 as CheckCircle,
  Folder,
  FolderGit2,
  Settings as Gear,
  Plug as PlugsConnected,
  Plus as PlusCircle,
  ShieldCheck,
  Sparkles as Sparkle,
  Loader2 as SpinnerGap,
  type LucideIcon as PhosphorIcon,
} from "lucide-react";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { cn } from "@/lib/utils";
import { CocolaLogo } from "@/components/cocola-logo";
import {
  useCocola,
  type ConversationFolder,
  type ConversationSummary,
} from "@/app/runtime-provider";
import { ConversationActionsMenu } from "@/components/assistant-ui/conversation-actions-menu";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";

// User workspace sidebar. New Chat + the Chats list are wired to the backend
// (conversation persistence, route A); secondary areas remain lightweight
// product shells until their backing features land.

type NavItem = { icon: PhosphorIcon; label: string; href?: string; iconClassName?: string };
type SidebarSection = "actions" | "navigation" | "projects" | "folders" | "chats" | "account";

type PrimaryNavItem = NavItem & {
  section: SidebarSection;
};

const PRIMARY_NAV: PrimaryNavItem[] = [
  {
    icon: CalendarDots,
    label: "Tasks",
    href: "/tasks",
    section: "navigation",
    iconClassName: "text-blue-600",
  },
  {
    icon: Sparkle,
    label: "Skills",
    href: "/skills",
    section: "navigation",
    iconClassName: "text-violet-600",
  },
  {
    icon: PlugsConnected,
    label: "MCP",
    href: "/mcps",
    section: "navigation",
    iconClassName: "text-orange-600",
  },
  {
    icon: Cable,
    label: "Connectors",
    href: "/connectors",
    section: "navigation",
    iconClassName: "text-emerald-600",
  },
  {
    icon: ShieldCheck,
    label: "Admin",
    href: "/admin",
    section: "navigation",
    iconClassName: "text-slate-500",
  },
];

export function AppSidebar() {
  const { data: session } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const { showSuccess } = useWorkspaceToast();
  const sectionRefs = useRef<Record<SidebarSection, HTMLDivElement | null>>({
    actions: null,
    navigation: null,
    projects: null,
    folders: null,
    chats: null,
    account: null,
  });
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; title: string } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const {
    newConversation,
    conversations,
    loadConversation,
    renameConversation,
    deleteConversation,
    folders,
    moveConversation,
    activeSessionId,
    runningSessionIds,
    unreadCompletedSessionIds,
  } = useCocola();
  const isAdmin = session?.user?.role === "admin";
  const userLabel = session?.user?.name || session?.user?.email || "User";
  const userSubtitle = session?.user?.role;
  const userInitial = userLabel.trim().slice(0, 1).toUpperCase() || "U";
  const visiblePrimaryNav = PRIMARY_NAV.filter(
    (item) => !item.href?.startsWith("/admin") || isAdmin,
  );
  const regularConversations = conversations.filter((conversation) => !conversation.project_id);

  const setSectionRef = (section: SidebarSection) => (node: HTMLDivElement | null) => {
    sectionRefs.current[section] = node;
  };

  const navigateTo = useCallback(
    (href: string) => {
      if (pathname === href || pathname?.startsWith(`${href}/`)) return;
      router.push(href);
    },
    [pathname, router],
  );

  const openNewChat = () => {
    newConversation();
    if (pathname !== "/") router.push("/");
  };

  const openConversation = (id: string) => {
    if (pathname !== "/") {
      router.push(`/?conversation=${encodeURIComponent(id)}`);
      return;
    }
    void loadConversation(id);
  };

  const startRename = (id: string, title: string) => {
    setEditingId(id);
    setDraftTitle(title);
  };

  const commitRename = async (id: string) => {
    const title = draftTitle.trim();
    setEditingId(null);
    if (!title) return;
    try {
      await renameConversation(id, title);
    } catch {
      window.alert("Rename failed. Please try again.");
    }
  };

  const openDeleteDialog = (id: string, title: string) => {
    setDeleteError(null);
    setDeleteTarget({ id, title });
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

  const moveChat = async (conversationId: string, folderId: string | null) => {
    try {
      await moveConversation(conversationId, folderId);
      const destination = folderId
        ? folders.find((folder) => folder.id === folderId)?.name || "folder"
        : "Chats";
      showSuccess(`Moved to ${destination}`);
    } catch (error) {
      window.alert(error instanceof Error ? error.message : "Could not move conversation");
    }
  };

  return (
    <>
      <aside className="sky-glass-sidebar flex h-full w-[17rem] shrink-0 flex-col overflow-hidden border-r border-sidebar-border text-sidebar-foreground max-sm:absolute max-sm:left-0 max-sm:top-0 max-sm:z-40">
        <div className="flex h-16 items-center justify-between gap-2 px-3">
          <div className="flex min-w-0 items-center gap-2">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
              <CocolaLogo mono className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <span className="block truncate text-[15px] font-bold text-sidebar-foreground">
                cocola
              </span>
              <span className="block truncate text-xs font-medium text-sidebar-foreground/70">
                agent workspace
              </span>
            </div>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-2">
          <SidebarSectionPanel refSetter={setSectionRef("actions")}>
            <button
              type="button"
              title="New Chat"
              onClick={openNewChat}
              className="flex w-full items-center justify-center gap-2 rounded-xl px-3 py-2.5 text-[13.5px] font-semibold text-white transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
              style={{ background: "linear-gradient(135deg,#2563eb,#7c3aed)" }}
            >
              <PlusCircle className="size-4 shrink-0" />
              New Chat
            </button>
          </SidebarSectionPanel>

          <SidebarSectionPanel refSetter={setSectionRef("navigation")}>
            {visiblePrimaryNav.map(({ icon: Icon, label, href, iconClassName }) => {
              const active = href
                ? href === "/"
                  ? pathname === "/"
                  : pathname === href || pathname?.startsWith(`${href}/`)
                : false;
              return (
                <SidebarExpandedRow
                  key={label}
                  title={label}
                  active={active}
                  onClick={href ? () => navigateTo(href) : undefined}
                >
                  <Icon
                    className={cn(
                      "size-4 shrink-0",
                      iconClassName ?? "text-sidebar-accent-foreground",
                    )}
                  />
                  <span className="truncate">{label}</span>
                </SidebarExpandedRow>
              );
            })}
          </SidebarSectionPanel>

          <SidebarSectionPanel refSetter={setSectionRef("projects")}>
            <div
              className={cn(
                "flex h-9 items-center rounded-2xl px-2.5 text-[13.5px] font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                pathname === "/projects" || pathname?.startsWith("/projects/")
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "",
              )}
            >
              <Link
                href="/projects"
                className="flex min-w-0 flex-1 items-center gap-2.5 self-stretch"
              >
                <FolderGit2 className="size-4 shrink-0 text-indigo-500" />
                <span className="truncate">Projects</span>
              </Link>
              <Link
                href="/projects/new"
                aria-label="Create or import project"
                title="Create or import project"
                className="grid size-7 place-items-center rounded-lg text-sidebar-foreground/65 transition hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
              >
                <PlusCircle className="size-4" />
              </Link>
            </div>
          </SidebarSectionPanel>

          <SidebarSectionPanel refSetter={setSectionRef("folders")}>
            <div
              className={cn(
                "flex h-9 items-center rounded-2xl px-2.5 text-[13.5px] font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                pathname === "/folders" || pathname?.startsWith("/folders/")
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "",
              )}
            >
              <Link
                href="/folders"
                className="flex min-w-0 flex-1 items-center gap-2.5 self-stretch"
              >
                <Folder className="size-4 shrink-0 text-amber-500" />
                <span className="truncate">Folders</span>
              </Link>
              <Link
                href="/folders/new"
                aria-label="Create folder"
                title="Create folder"
                className="grid size-7 place-items-center rounded-lg text-sidebar-foreground/65 transition hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
              >
                <PlusCircle className="size-4" />
              </Link>
            </div>
          </SidebarSectionPanel>

          <SidebarSectionPanel refSetter={setSectionRef("chats")}>
            <SectionLabel>Chats</SectionLabel>
            {regularConversations.length === 0 ? (
              <div className="px-2.5 py-1 text-xs text-sidebar-foreground/50">
                No conversations yet
              </div>
            ) : (
              <div className="flex flex-col gap-0.5">
                {regularConversations.map((c) => (
                  <ChatHistoryItem
                    key={c.id}
                    conversation={c}
                    folders={folders}
                    active={c.id === activeSessionId}
                    running={runningSessionIds.has(c.id)}
                    unread={unreadCompletedSessionIds.has(c.id)}
                    editing={editingId === c.id}
                    draftTitle={draftTitle}
                    onOpen={() => {
                      openConversation(c.id);
                    }}
                    onRename={() => startRename(c.id, c.title || "Untitled")}
                    onDelete={() => openDeleteDialog(c.id, c.title || "Untitled")}
                    onMove={(folderId) => void moveChat(c.id, folderId)}
                    onDraftChange={setDraftTitle}
                    onCommitRename={() => void commitRename(c.id)}
                    onCancelRename={() => setEditingId(null)}
                  />
                ))}
              </div>
            )}
          </SidebarSectionPanel>
        </nav>

        <div ref={setSectionRef("account")} className="border-t border-sidebar-border p-2">
          <Link
            href="/profile"
            title="Profile"
            className="group flex min-w-0 items-center gap-2 rounded-2xl px-2 py-1.5 text-sidebar-foreground/90 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
          >
            <div className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-[11px] font-medium text-primary-foreground">
              {userInitial}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[15px] font-semibold text-sidebar-foreground">
                {userLabel}
              </div>
              {userSubtitle && (
                <div className="truncate text-xs font-medium text-sidebar-foreground/70">
                  {userSubtitle}
                </div>
              )}
            </div>
            <Gear className="size-4 shrink-0 text-sidebar-foreground/45 transition-colors group-hover:text-sidebar-accent-foreground" />
          </Link>
        </div>
      </aside>

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

function SidebarExpandedRow({
  children,
  title,
  onClick,
  active,
}: {
  children: React.ReactNode;
  title: string;
  onClick?: () => void;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-2xl px-2.5 py-2 text-[13.5px] font-medium text-sidebar-foreground transition-all hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45",
        active && "bg-sidebar-accent text-sidebar-accent-foreground",
      )}
    >
      {children}
    </button>
  );
}

function SidebarSectionPanel({
  children,
  refSetter,
}: {
  children: React.ReactNode;
  refSetter: (node: HTMLDivElement | null) => void;
}) {
  return (
    <div ref={refSetter} className="p-1">
      {children}
    </div>
  );
}

function ChatHistoryItem({
  conversation,
  folders,
  active,
  running,
  unread,
  editing,
  draftTitle,
  onOpen,
  onRename,
  onDelete,
  onMove,
  onDraftChange,
  onCommitRename,
  onCancelRename,
}: {
  conversation: ConversationSummary;
  folders: ConversationFolder[];
  active: boolean;
  running: boolean;
  unread: boolean;
  editing: boolean;
  draftTitle: string;
  onOpen: () => void;
  onRename: () => void;
  onDelete: () => void;
  onMove: (folderId: string | null) => void;
  onDraftChange: (title: string) => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
}) {
  const title = conversation.title || "Untitled";
  return (
    <div
      className={cn(
        "group relative flex h-8 items-center gap-2 rounded-xl px-2.5 text-[13px] text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground shadow-sm",
      )}
      title={title}
    >
      {editing ? (
        <>
          <Folder className="size-4 shrink-0 opacity-0" />
          <input
            autoFocus
            value={draftTitle}
            onChange={(e) => onDraftChange(e.target.value)}
            onBlur={onCommitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                onCommitRename();
              } else if (e.key === "Escape") {
                e.preventDefault();
                onCancelRename();
              }
            }}
            className="min-w-0 flex-1 rounded border border-sidebar-border bg-sidebar px-1.5 py-0.5 text-sm outline-none focus:border-sidebar-foreground/30"
          />
        </>
      ) : (
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
          aria-label={title}
          onClick={onOpen}
        >
          <ChatTypeIcon type={conversation.chat_type || "chat"} />
          <span className="min-w-0 flex-1 truncate">{title}</span>
        </button>
      )}

      {running ? (
        <SpinnerGap
          className="size-3.5 shrink-0 animate-spin text-sidebar-accent-foreground/70"
          aria-label="Agent is answering"
        />
      ) : !editing ? (
        <div className="flex shrink-0 items-center gap-0.5">
          {unread ? (
            <CheckCircle
              className="size-3.5 text-emerald-500 transition-opacity group-hover:hidden"
              aria-label="Answer completed"
            />
          ) : null}
          <ConversationActionsMenu
            conversation={conversation}
            folders={folders}
            onRename={onRename}
            onDelete={onDelete}
            onMove={onMove}
            triggerClassName="text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-foreground"
          />
        </div>
      ) : null}
    </div>
  );
}

function ChatTypeIcon({ type }: { type: string }) {
  if (type === "scheduled_task") {
    return <CalendarDots className="size-4 shrink-0 text-sidebar-accent-foreground" />;
  }
  return <ChatsCircle className="size-4 shrink-0 text-sidebar-accent-foreground" />;
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-2.5 pb-1 pt-3 text-[13px] font-semibold text-sidebar-foreground/70">
      {children}
    </div>
  );
}
