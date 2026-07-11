"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  CalendarDots,
  ChatsCircle,
  CheckCircle,
  DotsThree,
  Folder,
  Gear,
  Hash,
  MagnifyingGlass,
  Notebook,
  PencilSimple,
  PlugsConnected,
  PlusCircle,
  ShieldCheck,
  SidebarSimple,
  Sparkle,
  SpinnerGap,
  Trash,
  type Icon as PhosphorIcon,
} from "@phosphor-icons/react";
import { useSession } from "next-auth/react";
import { motion } from "framer-motion";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { cn } from "@/lib/utils";
import { CocolaLogo } from "@/components/cocola-logo";
import { useCocola } from "@/app/runtime-provider";

// User workspace sidebar. New Chat + the Chats list are wired to the backend
// (conversation persistence, route A); secondary areas remain lightweight
// product shells until their backing features land.

type NavItem = { icon: PhosphorIcon; label: string; href?: string };
type SidebarSection = "actions" | "navigation" | "channels" | "folders" | "chats" | "account";

type PrimaryNavItem = NavItem & {
  section: SidebarSection;
};

const PRIMARY_NAV: PrimaryNavItem[] = [
  { icon: CalendarDots, label: "Tasks", href: "/tasks", section: "navigation" },
  { icon: MagnifyingGlass, label: "Search", section: "navigation" },
  { icon: Notebook, label: "Notes", section: "navigation" },
  { icon: Sparkle, label: "Skills", href: "/skills", section: "navigation" },
  { icon: PlugsConnected, label: "MCP", href: "/mcps", section: "navigation" },
  { icon: ShieldCheck, label: "Admin", href: "/admin", section: "navigation" },
];

const CHANNELS = [{ icon: Hash, label: "general" }];

const FOLDERS = [
  { emoji: "💵", label: "Finance" },
  { emoji: "📕", label: "Study" },
];

export function AppSidebar() {
  const { data: session } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const [collapsed, setCollapsed] = useState(true);
  const sectionRefs = useRef<Record<SidebarSection, HTMLDivElement | null>>({
    actions: null,
    navigation: null,
    channels: null,
    folders: null,
    chats: null,
    account: null,
  });
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

  const revealSection = (section: SidebarSection) => {
    setCollapsed(false);
    window.setTimeout(() => {
      sectionRefs.current[section]?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }, 180);
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
    setMenuOpenId(null);
    if (pathname !== "/") {
      router.push(`/?conversation=${encodeURIComponent(id)}`);
      return;
    }
    void loadConversation(id);
  };

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
      <motion.aside
        initial={false}
        animate={{ width: collapsed ? 64 : 272 }}
        transition={{ type: "spring", stiffness: 380, damping: 36 }}
        className={cn(
          "sky-glass-sidebar my-1.5 ml-1.5 flex h-[calc(100%-0.75rem)] shrink-0 flex-col overflow-hidden rounded-[1.4rem] border text-sidebar-foreground max-sm:absolute max-sm:left-0 max-sm:top-0 max-sm:z-40",
          collapsed ? "w-16" : "w-[17rem]",
        )}
      >
        <div
          className={cn(
            "flex h-16 items-center gap-2 px-3",
            collapsed ? "justify-center px-2" : "justify-between",
          )}
        >
          {collapsed ? (
            <SidebarRailButton
              title="Expand sidebar"
              active={false}
              onClick={() => revealSection("navigation")}
            >
              <CocolaLogo className="size-7" />
            </SidebarRailButton>
          ) : (
            <>
              <div className="flex min-w-0 items-center gap-2">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
                  <CocolaLogo mono className="size-5" />
                </div>
                <div className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-semibold">cocola</span>
                  <span className="block truncate text-[11px] text-sidebar-foreground/55">
                    agent workspace
                  </span>
                </div>
              </div>
              <button
                type="button"
                onClick={() => setCollapsed(true)}
                aria-label="Collapse sidebar"
                title="Collapse sidebar"
                className="flex size-8 shrink-0 items-center justify-center rounded-xl text-sidebar-foreground/70 transition-colors hover:bg-white/38 hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
              >
                <SidebarSimple className="size-4 text-sidebar-accent-foreground" weight="duotone" />
              </button>
            </>
          )}
        </div>

        {collapsed ? (
          <>
            <nav className="flex flex-1 flex-col items-center gap-2 px-2 pb-2">
              <SidebarRailButton title="New Chat" onClick={openNewChat}>
                <PlusCircle className="size-4 text-sidebar-accent-foreground" weight="duotone" />
              </SidebarRailButton>
              <div className="my-1 h-px w-8 bg-white/36" />
              {visiblePrimaryNav.map(({ icon: Icon, label, href, section }) => {
                const active = href
                  ? href === "/"
                    ? pathname === "/"
                    : pathname === href || pathname?.startsWith(`${href}/`)
                  : false;
                return (
                  <SidebarRailButton
                    key={label}
                    title={label}
                    active={active}
                    onClick={() => revealSection(section)}
                  >
                    <Icon className="size-4 text-sidebar-accent-foreground" weight="duotone" />
                  </SidebarRailButton>
                );
              })}
              <div className="my-1 h-px w-8 bg-white/36" />
              <SidebarRailButton title="Channels" onClick={() => revealSection("channels")}>
                <Hash className="size-4 text-sidebar-accent-foreground" weight="duotone" />
              </SidebarRailButton>
              <SidebarRailButton title="Folders" onClick={() => revealSection("folders")}>
                <Folder className="size-4 text-sidebar-accent-foreground" weight="duotone" />
              </SidebarRailButton>
              <SidebarRailButton
                title="Chats"
                active={conversations.some((c) => c.id === activeSessionId)}
                onClick={() => revealSection("chats")}
              >
                <ChatsCircle className="size-4 text-sidebar-accent-foreground" weight="duotone" />
              </SidebarRailButton>
            </nav>
            <div className="flex flex-col items-center gap-2 px-2 pb-3">
              <SidebarRailButton title="Profile" onClick={() => revealSection("account")}>
                <span className="grid size-6 place-items-center rounded-full bg-primary text-[10px] font-semibold text-primary-foreground">
                  {userInitial}
                </span>
              </SidebarRailButton>
            </div>
          </>
        ) : (
          <>
            <nav className="flex-1 overflow-y-auto px-2 pb-2">
              <SidebarSectionPanel refSetter={setSectionRef("actions")}>
                <SidebarExpandedRow title="New Chat" onClick={openNewChat}>
                  <PlusCircle
                    className="size-4 shrink-0 text-sidebar-accent-foreground"
                    weight="duotone"
                  />
                  <span className="truncate">New Chat</span>
                </SidebarExpandedRow>
              </SidebarSectionPanel>

              <SidebarSectionPanel refSetter={setSectionRef("navigation")}>
                {visiblePrimaryNav.map(({ icon: Icon, label, href }) => {
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
                        className="size-4 shrink-0 text-sidebar-accent-foreground"
                        weight="duotone"
                      />
                      <span className="truncate">{label}</span>
                    </SidebarExpandedRow>
                  );
                })}
              </SidebarSectionPanel>

              <SidebarSectionPanel refSetter={setSectionRef("channels")}>
                <SectionLabel>Channels</SectionLabel>
                <div className="flex flex-col gap-0.5">
                  {CHANNELS.map(({ icon: Icon, label }) => (
                    <SidebarExpandedRow key={label} title={label}>
                      <Icon
                        className="size-4 shrink-0 text-sidebar-accent-foreground"
                        weight="duotone"
                      />
                      <span className="truncate">{label}</span>
                    </SidebarExpandedRow>
                  ))}
                </div>
              </SidebarSectionPanel>

              <SidebarSectionPanel refSetter={setSectionRef("folders")}>
                <SectionLabel>Folders</SectionLabel>
                <div className="flex flex-col gap-0.5">
                  {FOLDERS.map(({ emoji, label }) => (
                    <SidebarExpandedRow key={label} title={label}>
                      <span className="grid size-4 shrink-0 place-items-center text-xs">
                        {emoji}
                      </span>
                      <span className="truncate">{label}</span>
                    </SidebarExpandedRow>
                  ))}
                </div>
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
                        title={c.title || "Untitled"}
                        chatType={c.chat_type || "chat"}
                        active={c.id === activeSessionId}
                        running={runningSessionIds.has(c.id)}
                        unread={unreadCompletedSessionIds.has(c.id)}
                        menuOpen={menuOpenId === c.id}
                        editing={editingId === c.id}
                        draftTitle={draftTitle}
                        onOpen={() => {
                          openConversation(c.id);
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
              </SidebarSectionPanel>
            </nav>

            <div
              ref={setSectionRef("account")}
              className="border-t border-white/35 bg-white/10 p-2"
            >
              <Link
                href="/profile"
                title="Profile"
                className="group flex min-w-0 items-center gap-2 rounded-2xl px-2 py-1.5 text-sidebar-foreground/90 transition-colors hover:bg-white/38 hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
              >
                <div className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-[11px] font-medium text-primary-foreground">
                  {userInitial}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{userLabel}</div>
                  {userSubtitle && (
                    <div className="truncate text-[11px] text-sidebar-foreground/55">
                      {userSubtitle}
                    </div>
                  )}
                </div>
                <Gear
                  className="size-4 shrink-0 text-sidebar-foreground/45 transition-colors group-hover:text-sidebar-accent-foreground"
                  weight="duotone"
                />
              </Link>
            </div>
          </>
        )}
      </motion.aside>

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

function SidebarRailButton({
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
    <motion.button
      type="button"
      title={title}
      aria-label={title}
      onClick={onClick}
      whileHover={{ scale: 1.04, y: -1 }}
      whileTap={{ scale: 0.97 }}
      className={cn(
        "grid size-10 place-items-center rounded-2xl text-sidebar-foreground/70 transition-colors hover:bg-white/38 hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45",
        active &&
          "bg-white/44 text-sidebar-accent-foreground shadow-[inset_0_1px_0_hsl(0_0%_100%/0.7),0_10px_26px_hsl(207_78%_38%/0.14)] backdrop-blur-md",
      )}
    >
      {children}
    </motion.button>
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
        "flex h-9 w-full items-center gap-2 rounded-2xl px-2.5 text-sm text-sidebar-foreground/80 transition-all hover:bg-white/38 hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45",
        active &&
          "bg-white/42 text-sidebar-accent-foreground shadow-[inset_0_1px_0_hsl(0_0%_100%/0.68),0_10px_24px_hsl(207_78%_38%/0.12)] backdrop-blur-md",
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
    <div ref={refSetter} className="mb-2 p-1">
      {children}
    </div>
  );
}

function ChatHistoryItem({
  title,
  chatType,
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
  chatType: string;
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
        "group relative flex h-8 items-center gap-2 rounded-xl px-2.5 text-sm text-sidebar-foreground/85 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground shadow-sm",
      )}
      title={title}
      onMouseLeave={() => {
        if (menuOpen) onToggleMenu();
      }}
    >
      {editing ? (
        <>
          <Folder className="size-4 shrink-0 opacity-0" weight="duotone" />
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
          <ChatTypeIcon type={chatType} />
          <span className="min-w-0 flex-1 truncate">{title}</span>
        </button>
      )}

      {running ? (
        <SpinnerGap
          className="size-3.5 shrink-0 animate-spin text-sidebar-accent-foreground/70"
          weight="bold"
          aria-label="Agent is answering"
        />
      ) : !editing ? (
        <div className="relative size-3.5 shrink-0">
          {unread && !menuOpen && (
            <CheckCircle
              className="absolute inset-0 size-3.5 text-emerald-500 transition-opacity group-hover:opacity-0"
              weight="duotone"
              aria-label="Answer completed"
            />
          )}
          <button
            type="button"
            className={cn(
              "absolute left-1/2 top-1/2 grid size-6 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-md text-sidebar-foreground/60 opacity-0 transition hover:text-sidebar-foreground group-hover:opacity-100",
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
            <DotsThree className="size-4" weight="bold" />
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
            <PencilSimple className="size-3.5" weight="duotone" />
            Rename
          </button>
          <button
            type="button"
            role="menuitem"
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-red-500 hover:bg-red-500/10"
            onClick={onDelete}
          >
            <Trash className="size-3.5" weight="duotone" />
            Delete
          </button>
        </div>
      )}
    </div>
  );
}

function ChatTypeIcon({ type }: { type: string }) {
  if (type === "scheduled_task") {
    return (
      <CalendarDots className="size-4 shrink-0 text-sidebar-accent-foreground" weight="duotone" />
    );
  }
  return (
    <ChatsCircle className="size-4 shrink-0 text-sidebar-accent-foreground" weight="duotone" />
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
            <Trash className="size-4" weight="duotone" />
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
