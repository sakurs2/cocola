"use client";

import { useState } from "react";
import {
  CheckCircle2,
  FolderClosed,
  Hash,
  LoaderCircle,
  LogOut,
  MoreHorizontal,
  MessagesSquare,
  NotebookPen,
  PanelLeft,
  Pencil,
  Plus,
  Search,
  Server,
  LayoutGrid,
  Trash2,
  Users,
} from "lucide-react";
import { signOut, useSession } from "next-auth/react";
import { cn } from "@/lib/utils";
import { useCocola } from "@/app/runtime-provider";

// Sidebar mirroring the Open WebUI chrome. New Chat + the Chats list are wired
// to the backend (conversation persistence, route A): Chats lists the user's
// stored conversations and clicking one replays it into the thread. Search /
// Notes / Workspace / Channels / Folders remain decorative shells. Hand-rolled
// (plain divs + a useState collapse) to avoid pulling in Radix.

type NavItem = { icon: typeof Plus; label: string; href?: string };

// "New Chat" is wired to the runtime (rotates session_id + clears messages);
// the rest stay decorative until multi-thread persistence lands.
const PRIMARY_NAV: NavItem[] = [
  { icon: Search, label: "Search" },
  { icon: NotebookPen, label: "Notes" },
  { icon: LayoutGrid, label: "Workspace" },
  { icon: Users, label: "Users", href: "/admin/users" },
  { icon: Server, label: "Sandbox Nodes", href: "/admin/sandbox-nodes" },
];

const CHANNELS = [{ icon: Hash, label: "general" }];

const FOLDERS = [
  { emoji: "💵", label: "Finance" },
  { emoji: "📕", label: "Study" },
];

export function AppSidebar() {
  const { data: session } = useSession();
  const [collapsed, setCollapsed] = useState(false);
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null);
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
    activeConversationId,
    runningConversationIds,
    unreadCompletedConversationIds,
  } = useCocola();
  const isAdmin = session?.user?.role === "admin";
  const userLabel = session?.user?.name || session?.user?.email || "User";
  const userSubtitle = session?.user?.role;
  const userInitial = userLabel.trim().slice(0, 1).toUpperCase() || "U";

  const startRename = (id: string, title: string) => {
    setMenuOpenId(null);
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
    setMenuOpenId(null);
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
    } catch {
      setDeleteError("Delete failed. Please try again.");
    } finally {
      setDeleting(false);
    }
  };

  return (
    <>
      <aside
        className={cn(
          "flex h-full shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-[width] duration-200",
          collapsed ? "w-[3.25rem]" : "w-64",
        )}
      >
        {/* Header: brand + collapse toggle */}
        <div
          className={cn("flex h-14 items-center gap-2 px-3", collapsed && "justify-center px-0")}
        >
          {!collapsed && (
            <>
              <div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-foreground text-background">
                <MessagesSquare className="size-4" />
              </div>
              <span className="flex-1 truncate text-sm font-semibold">cocola</span>
            </>
          )}
          <button
            type="button"
            onClick={() => setCollapsed((v) => !v)}
            aria-label="Toggle sidebar"
            title="Toggle sidebar"
            className="flex size-7 shrink-0 items-center justify-center rounded-md text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          >
            <PanelLeft className="size-4" />
          </button>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-2">
          {/* Primary actions */}
          <div className="flex flex-col gap-0.5">
            <SidebarButton collapsed={collapsed} title="New Chat" onClick={newConversation}>
              <Plus className="size-4 shrink-0" />
              {!collapsed && <span className="truncate">New Chat</span>}
            </SidebarButton>
            {PRIMARY_NAV.filter((item) => !item.href?.startsWith("/admin") || isAdmin).map(
              ({ icon: Icon, label, href }) => (
                <SidebarButton
                  key={label}
                  collapsed={collapsed}
                  title={label}
                  onClick={
                    href
                      ? () => {
                          window.location.href = href;
                        }
                      : undefined
                  }
                >
                  <Icon className="size-4 shrink-0" />
                  {!collapsed && <span className="truncate">{label}</span>}
                </SidebarButton>
              ),
            )}
          </div>

          {!collapsed && (
            <>
              <SectionLabel>Channels</SectionLabel>
              <div className="flex flex-col gap-0.5">
                {CHANNELS.map(({ icon: Icon, label }) => (
                  <SidebarButton key={label} collapsed={collapsed} title={label}>
                    <Icon className="size-4 shrink-0" />
                    <span className="truncate">{label}</span>
                  </SidebarButton>
                ))}
              </div>

              <SectionLabel>Folders</SectionLabel>
              <div className="flex flex-col gap-0.5">
                {FOLDERS.map(({ emoji, label }) => (
                  <SidebarButton key={label} collapsed={collapsed} title={label}>
                    <span className="grid size-4 shrink-0 place-items-center text-xs">{emoji}</span>
                    <span className="truncate">{label}</span>
                  </SidebarButton>
                ))}
              </div>

              <SectionLabel>Chats</SectionLabel>
              {conversations.length === 0 ? (
                <div className="px-2.5 py-1 text-xs text-sidebar-foreground/50">
                  No conversations yet
                </div>
              ) : (
                <div className="flex flex-col gap-0.5">
                  {conversations.map((c) => (
                    <ChatHistoryItem
                      key={c.id}
                      title={c.title || "Untitled"}
                      active={c.id === activeConversationId}
                      running={runningConversationIds.has(c.id)}
                      unread={unreadCompletedConversationIds.has(c.id)}
                      menuOpen={menuOpenId === c.id}
                      editing={editingId === c.id}
                      draftTitle={draftTitle}
                      onOpen={() => {
                        setMenuOpenId(null);
                        void loadConversation(c.id);
                      }}
                      onToggleMenu={() => setMenuOpenId((prev) => (prev === c.id ? null : c.id))}
                      onRename={() => startRename(c.id, c.title || "Untitled")}
                      onDelete={() => openDeleteDialog(c.id, c.title || "Untitled")}
                      onDraftChange={setDraftTitle}
                      onCommitRename={() => void commitRename(c.id)}
                      onCancelRename={() => setEditingId(null)}
                    />
                  ))}
                </div>
              )}
            </>
          )}
        </nav>

        <div className="border-t border-sidebar-border p-2">
          <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
            <div className="grid size-6 shrink-0 place-items-center rounded-full bg-amber-500/90 text-[11px] font-medium text-white">
              {userInitial}
            </div>
            {!collapsed && (
              <>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{userLabel}</div>
                  {userSubtitle && (
                    <div className="truncate text-[11px] text-sidebar-foreground/55">
                      {userSubtitle}
                    </div>
                  )}
                </div>
                <button
                  type="button"
                  title="Sign out"
                  aria-label="Sign out"
                  onClick={() => void signOut({ callbackUrl: "/login" })}
                  className="grid size-7 shrink-0 place-items-center rounded-md text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                >
                  <LogOut className="size-4" />
                </button>
              </>
            )}
          </div>
        </div>
      </aside>

      {deleteTarget && (
        <DeleteConversationDialog
          title={deleteTarget.title}
          deleting={deleting}
          error={deleteError}
          onCancel={() => {
            if (deleting) return;
            setDeleteTarget(null);
            setDeleteError(null);
          }}
          onConfirm={() => void confirmDelete()}
        />
      )}
    </>
  );
}

function SidebarButton({
  children,
  collapsed,
  title,
  onClick,
  active,
}: {
  children: React.ReactNode;
  collapsed: boolean;
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
        "flex h-8 items-center gap-2 rounded-md px-2.5 text-sm text-sidebar-foreground/90 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground",
        collapsed && "justify-center px-0",
      )}
    >
      {children}
    </button>
  );
}

function ChatHistoryItem({
  title,
  active,
  running,
  unread,
  menuOpen,
  editing,
  draftTitle,
  onOpen,
  onToggleMenu,
  onRename,
  onDelete,
  onDraftChange,
  onCommitRename,
  onCancelRename,
}: {
  title: string;
  active: boolean;
  running: boolean;
  unread: boolean;
  menuOpen: boolean;
  editing: boolean;
  draftTitle: string;
  onOpen: () => void;
  onToggleMenu: () => void;
  onRename: () => void;
  onDelete: () => void;
  onDraftChange: (title: string) => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
}) {
  return (
    <div
      className={cn(
        "group relative flex h-8 items-center gap-2 rounded-md px-2.5 text-sm text-sidebar-foreground/90 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground",
      )}
      title={title}
    >
      {editing ? (
        <>
          <FolderClosed className="size-4 shrink-0 opacity-0" />
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
          <FolderClosed className="size-4 shrink-0 opacity-0" />
          <span className="min-w-0 flex-1 truncate">{title}</span>
        </button>
      )}

      {running ? (
        <LoaderCircle
          className="size-3.5 shrink-0 animate-spin text-sidebar-foreground/55"
          aria-label="Agent is answering"
        />
      ) : !editing ? (
        <div className="relative size-6 shrink-0">
          {unread && !menuOpen && (
            <CheckCircle2
              className="absolute inset-0 m-auto size-3.5 text-emerald-500 transition-opacity group-hover:opacity-0"
              aria-label="Answer completed"
            />
          )}
          <button
            type="button"
            className={cn(
              "absolute inset-0 grid place-items-center rounded-md text-sidebar-foreground/60 opacity-0 transition hover:bg-sidebar-accent-foreground/10 hover:text-sidebar-foreground group-hover:opacity-100",
              menuOpen && "opacity-100",
            )}
            aria-label={`Conversation actions for ${title}`}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={(e) => {
              e.stopPropagation();
              onToggleMenu();
            }}
          >
            <MoreHorizontal className="size-4" />
          </button>
        </div>
      ) : null}

      {menuOpen && !editing && (
        <div
          role="menu"
          className="absolute right-1 top-7 z-20 w-32 overflow-hidden rounded-md border border-sidebar-border bg-sidebar p-1 shadow-lg"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            role="menuitem"
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-sidebar-accent"
            onClick={onRename}
          >
            <Pencil className="size-3.5" />
            Rename
          </button>
          <button
            type="button"
            role="menuitem"
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-red-500 hover:bg-red-500/10"
            onClick={onDelete}
          >
            <Trash2 className="size-3.5" />
            Delete
          </button>
        </div>
      )}
    </div>
  );
}

function DeleteConversationDialog({
  title,
  deleting,
  error,
  onCancel,
  onConfirm,
}: {
  title: string;
  deleting: boolean;
  error: string | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/35 px-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="delete-conversation-title"
        className="w-full max-w-sm rounded-lg border border-border bg-background p-4 text-foreground shadow-xl"
      >
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-red-500/10 text-red-500">
            <Trash2 className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 id="delete-conversation-title" className="text-sm font-semibold">
              Delete conversation
            </h2>
            <p className="mt-1 text-sm text-muted-foreground">
              This will delete <span className="font-medium text-foreground">{title}</span> and stop
              any running answer for this conversation.
            </p>
          </div>
        </div>

        {error && (
          <div className="mt-3 rounded-md border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-500">
            {error}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            disabled={deleting}
            onClick={onCancel}
            className="h-8 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={deleting}
            onClick={onConfirm}
            className="h-8 rounded-md bg-red-500 px-3 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-70"
          >
            {deleting ? "Deleting..." : "Delete"}
          </button>
        </div>
      </div>
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-2.5 pb-1 pt-4 text-xs font-medium text-sidebar-foreground/50">
      {children}
    </div>
  );
}
