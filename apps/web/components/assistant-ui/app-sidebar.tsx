"use client";

import { useCallback, useRef, useState } from "react";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import {
  CalendarCheck as CalendarDots,
  MessagesSquare as ChatsCircle,
  CheckCircle2 as CheckCircle,
  MoreHorizontal as DotsThree,
  Folder,
  Settings as Gear,
  Search as MagnifyingGlass,
  Notebook,
  Pencil as PencilSimple,
  Plug as PlugsConnected,
  Plus as PlusCircle,
  ShieldCheck,
  Sparkles as Sparkle,
  Loader2 as SpinnerGap,
  Trash2 as Trash,
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
type SidebarSection = "actions" | "navigation" | "folders" | "chats" | "account";

type PrimaryNavItem = NavItem & {
  section: SidebarSection;
};

const PRIMARY_NAV: PrimaryNavItem[] = [
  { icon: CalendarDots, label: "Tasks", href: "/tasks", section: "navigation", iconClassName: "text-blue-600" },
  { icon: MagnifyingGlass, label: "Search", section: "navigation", iconClassName: "text-green-600" },
  { icon: Notebook, label: "Notes", section: "navigation", iconClassName: "text-cyan-600" },
  { icon: Sparkle, label: "Skills", href: "/skills", section: "navigation", iconClassName: "text-violet-600" },
  { icon: PlugsConnected, label: "MCP", href: "/mcps", section: "navigation", iconClassName: "text-orange-600" },
  { icon: ShieldCheck, label: "Admin", href: "/admin", section: "navigation", iconClassName: "text-slate-500" },
];

export function AppSidebar() {
  const { data: session } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const { showSuccess } = useWorkspaceToast();
  const sectionRefs = useRef<Record<SidebarSection, HTMLDivElement | null>>({
    actions: null,
    navigation: null,
    folders: null,
    chats: null,
    account: null,
  });
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState("");
  const [creatingFolder, setCreatingFolder] = useState(false);
  const [editingFolderId, setEditingFolderId] = useState<string | null>(null);
  const [folderDraft, setFolderDraft] = useState("");
  const [sidebarError, setSidebarError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<{
    kind: "conversation" | "folder";
    id: string;
    title: string;
  } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const {
    newConversation,
    conversations,
    loadConversation,
    renameConversation,
    deleteConversation,
    folders,
    createFolder,
    renameFolder,
    deleteFolder,
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

  const openDeleteDialog = (kind: "conversation" | "folder", id: string, title: string) => {
    setDeleteError(null);
    setDeleteTarget({ kind, id, title });
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      if (deleteTarget.kind === "folder") {
        await deleteFolder(deleteTarget.id);
        if (pathname === `/folders/${deleteTarget.id}`) router.push("/");
      } else {
        await deleteConversation(deleteTarget.id);
      }
      setDeleteTarget(null);
    } catch (error) {
      setDeleteError(error instanceof Error ? error.message : "Delete failed. Please try again.");
    } finally {
      setDeleting(false);
    }
  };

  const commitCreateFolder = async () => {
    const name = folderDraft.trim();
    if (!name) {
      setCreatingFolder(false);
      setFolderDraft("");
      return;
    }
    try {
      await createFolder(name);
      setCreatingFolder(false);
      setFolderDraft("");
      setSidebarError(null);
    } catch (error) {
      setSidebarError(error instanceof Error ? error.message : "Could not create folder");
    }
  };

  const commitRenameFolder = async (id: string) => {
    const name = folderDraft.trim();
    if (!name) {
      setEditingFolderId(null);
      setFolderDraft("");
      return;
    }
    try {
      await renameFolder(id, name);
      setEditingFolderId(null);
      setFolderDraft("");
      setSidebarError(null);
    } catch (error) {
      setSidebarError(error instanceof Error ? error.message : "Could not rename folder");
    }
  };

  const moveChat = async (conversationId: string, folderId: string | null) => {
    try {
      await moveConversation(conversationId, folderId);
      setSidebarError(null);
      const destination = folderId
        ? folders.find((folder) => folder.id === folderId)?.name || "folder"
        : "Chats";
      showSuccess(`Moved to ${destination}`);
    } catch (error) {
      setSidebarError(error instanceof Error ? error.message : "Could not move conversation");
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
              <span className="block truncate text-[15px] font-bold text-sidebar-foreground">cocola</span>
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
                        className={cn("size-4 shrink-0", iconClassName ?? "text-sidebar-accent-foreground")}
                      />
                      <span className="truncate">{label}</span>
                    </SidebarExpandedRow>
                  );
                })}
              </SidebarSectionPanel>

              <SidebarSectionPanel refSetter={setSectionRef("folders")}>
                <div className="flex items-center justify-between px-2.5 pb-1 pt-3 text-[13px] font-semibold text-sidebar-foreground/70">
                  <span>Folders</span>
                  <button
                    type="button"
                    aria-label="Create folder"
                    title="Create folder"
                    onClick={() => {
                      setCreatingFolder(true);
                      setEditingFolderId(null);
                      setFolderDraft("");
                      setSidebarError(null);
                    }}
                    className="grid size-6 place-items-center rounded-lg transition hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus:outline-none"
                  >
                    <PlusCircle className="size-4" />
                  </button>
                </div>
                <div className="flex flex-col gap-0.5">
                  {creatingFolder ? (
                    <FolderNameInput
                      value={folderDraft}
                      placeholder="Folder name"
                      onChange={setFolderDraft}
                      onBlur={() => void commitCreateFolder()}
                      onCancel={() => {
                        setCreatingFolder(false);
                        setFolderDraft("");
                      }}
                    />
                  ) : null}
                  {folders.map((folder) => (
                    <FolderSidebarItem
                      key={folder.id}
                      folder={folder}
                      active={pathname === `/folders/${folder.id}`}
                      editing={editingFolderId === folder.id}
                      draft={folderDraft}
                      onOpen={() => router.push(`/folders/${encodeURIComponent(folder.id)}`)}
                      onStartRename={() => {
                        setCreatingFolder(false);
                        setEditingFolderId(folder.id);
                        setFolderDraft(folder.name);
                        setSidebarError(null);
                      }}
                      onDelete={() => openDeleteDialog("folder", folder.id, folder.name)}
                      onDraftChange={setFolderDraft}
                      onCommitRename={() => void commitRenameFolder(folder.id)}
                      onCancelRename={() => {
                        setEditingFolderId(null);
                        setFolderDraft("");
                      }}
                    />
                  ))}
                </div>
                {sidebarError ? (
                  <p className="px-2.5 pt-1.5 text-[11px] leading-4 text-red-600">{sidebarError}</p>
                ) : null}
              </SidebarSectionPanel>

              <SidebarSectionPanel refSetter={setSectionRef("chats")}>
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
                        onDelete={() =>
                          openDeleteDialog("conversation", c.id, c.title || "Untitled")
                        }
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

            <div
              ref={setSectionRef("account")}
              className="border-t border-sidebar-border p-2"
            >
              <Link
                href="/profile"
                title="Profile"
                className="group flex min-w-0 items-center gap-2 rounded-2xl px-2 py-1.5 text-sidebar-foreground/90 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
              >
                <div className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-[11px] font-medium text-primary-foreground">
                  {userInitial}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[15px] font-semibold text-sidebar-foreground">{userLabel}</div>
                  {userSubtitle && (
                    <div className="truncate text-xs font-medium text-sidebar-foreground/70">
                      {userSubtitle}
                    </div>
                  )}
                </div>
                <Gear
                  className="size-4 shrink-0 text-sidebar-foreground/45 transition-colors group-hover:text-sidebar-accent-foreground"
                />
              </Link>
            </div>
      </aside>

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        title={
          deleteTarget?.kind === "folder" ? "Delete folder and chats?" : "Delete conversation?"
        }
        description={
          deleteTarget?.kind === "folder" ? (
            <>
              <span className="font-medium text-foreground">{deleteTarget.title}</span> and every
              conversation inside it will be permanently deleted. Stop any running answers first.
            </>
          ) : (
            <>
              <span className="font-medium text-foreground">{deleteTarget?.title}</span> will be
              permanently deleted. Stop its running answer first.
            </>
          )
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
        active &&
          "bg-sidebar-accent text-sidebar-accent-foreground",
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

function FolderSidebarItem({
  folder,
  active,
  editing,
  draft,
  onOpen,
  onStartRename,
  onDelete,
  onDraftChange,
  onCommitRename,
  onCancelRename,
}: {
  folder: ConversationFolder;
  active: boolean;
  editing: boolean;
  draft: string;
  onOpen: () => void;
  onStartRename: () => void;
  onDelete: () => void;
  onDraftChange: (value: string) => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
}) {
  if (editing) {
    return (
      <FolderNameInput
        value={draft}
        onChange={onDraftChange}
        onBlur={onCommitRename}
        onCancel={onCancelRename}
      />
    );
  }
  return (
    <div
      className={cn(
        "group flex h-9 items-center gap-1 rounded-2xl px-2.5 text-[15px] font-medium text-sidebar-foreground transition hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground",
      )}
    >
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-2">
        <Folder className="size-4 shrink-0" />
        <span className="truncate">{folder.name}</span>
      </button>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger asChild>
          <button
            type="button"
            aria-label={`Actions for folder ${folder.name}`}
            className="grid size-7 shrink-0 place-items-center rounded-lg opacity-0 transition hover:bg-sidebar-accent focus:opacity-100 focus:outline-none group-hover:opacity-100 data-[state=open]:opacity-100"
          >
            <DotsThree className="size-4" />
          </button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            sideOffset={5}
            className="cocola-user-ui z-50 min-w-36 rounded-xl border border-border bg-popover p-1 text-foreground shadow-xl outline-none"
          >
            <DropdownMenu.Item
              onSelect={onStartRename}
              className="flex cursor-default items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-foreground outline-none focus:bg-accent focus:text-foreground data-[highlighted]:bg-accent data-[highlighted]:text-foreground"
            >
              <PencilSimple className="size-4" />
              Rename
            </DropdownMenu.Item>
            <DropdownMenu.Item
              onSelect={onDelete}
              className="flex cursor-default items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-red-500 outline-none focus:bg-red-500/10 focus:text-red-600"
            >
              <Trash className="size-4" />
              Delete
            </DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </div>
  );
}

function FolderNameInput({
  value,
  placeholder,
  onChange,
  onBlur,
  onCancel,
}: {
  value: string;
  placeholder?: string;
  onChange: (value: string) => void;
  onBlur: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="flex h-9 items-center gap-2 rounded-2xl bg-sidebar-accent px-2.5">
      <Folder className="size-4 shrink-0 text-sidebar-accent-foreground" />
      <input
        autoFocus
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        onBlur={onBlur}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            event.currentTarget.blur();
          } else if (event.key === "Escape") {
            event.preventDefault();
            onCancel();
          }
        }}
        className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-sidebar-foreground/45"
      />
    </div>
  );
}

function ChatTypeIcon({ type }: { type: string }) {
  if (type === "scheduled_task") {
    return (
      <CalendarDots className="size-4 shrink-0 text-sidebar-accent-foreground" />
    );
  }
  return (
    <ChatsCircle className="size-4 shrink-0 text-sidebar-accent-foreground" />
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-2.5 pb-1 pt-3 text-[13px] font-semibold text-sidebar-foreground/70">
      {children}
    </div>
  );
}
