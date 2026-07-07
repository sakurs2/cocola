"use client";

import { useCallback, useEffect, useState } from "react";
import {
  CalendarClock,
  CheckCircle2,
  FolderClosed,
  Hash,
  LoaderCircle,
  LogOut,
  MoreHorizontal,
  MessagesSquare,
  NotebookPen,
  PanelLeft,
  Pause,
  Pencil,
  PlugZap,
  Play,
  Plus,
  Search,
  ShieldCheck,
  Sparkles,
  LayoutGrid,
  Trash2,
} from "lucide-react";
import { signOut, useSession } from "next-auth/react";
import { motion } from "framer-motion";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { cn } from "@/lib/utils";
import { useCocola } from "@/app/runtime-provider";

// User workspace sidebar. New Chat + the Chats list are wired to the backend
// (conversation persistence, route A); secondary areas remain lightweight
// product shells until their backing features land.

type NavItem = { icon: typeof Plus; label: string; href?: string };

// "New Chat" is wired to the runtime (rotates session_id + clears messages);
// the rest stay decorative until multi-thread persistence lands.
const PRIMARY_NAV: NavItem[] = [
  { icon: Search, label: "Search" },
  { icon: NotebookPen, label: "Notes" },
  { icon: LayoutGrid, label: "Workspace" },
  { icon: Sparkles, label: "Skills", href: "/skills" },
  { icon: PlugZap, label: "MCP", href: "/mcps" },
  { icon: ShieldCheck, label: "Admin", href: "/admin" },
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
  const [collapsed, setCollapsed] = useState(false);
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; title: string } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const {
    newConversation,
    conversations,
    refreshConversations,
    loadConversation,
    renameConversation,
    deleteConversation,
    activeSessionId,
    runningSessionIds,
    unreadCompletedSessionIds,
    models,
    selectedModelAlias,
  } = useCocola();
  const isAdmin = session?.user?.role === "admin";
  const userLabel = session?.user?.name || session?.user?.email || "User";
  const userSubtitle = session?.user?.role;
  const userInitial = userLabel.trim().slice(0, 1).toUpperCase() || "U";

  const navigateTo = useCallback(
    (href: string) => {
      if (pathname === href || pathname?.startsWith(`${href}/`)) return;
      router.push(href);
    },
    [pathname, router],
  );

  const openNewChat = () => {
    if (pathname !== "/") {
      router.push("/");
      return;
    }
    newConversation();
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
        animate={{ width: collapsed ? 52 : 264 }}
        transition={{ type: "spring", stiffness: 380, damping: 36 }}
        className={cn(
          "glass-panel m-2 flex h-[calc(100%-1rem)] shrink-0 flex-col overflow-hidden rounded-2xl border border-sidebar-border/80 bg-sidebar text-sidebar-foreground",
          collapsed ? "w-[3.25rem]" : "w-[16.5rem]",
        )}
      >
        {/* Header: brand + collapse toggle */}
        <div
          className={cn("flex h-16 items-center gap-2 px-3", collapsed && "justify-center px-0")}
        >
          {!collapsed && (
            <>
              <div className="flex size-8 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-sm">
                <MessagesSquare className="size-4" />
              </div>
              <div className="min-w-0 flex-1">
                <span className="block truncate text-sm font-semibold">cocola</span>
                <span className="block truncate text-[11px] text-sidebar-foreground/55">
                  agent workspace
                </span>
              </div>
            </>
          )}
          <button
            type="button"
            onClick={() => setCollapsed((v) => !v)}
            aria-label="Toggle sidebar"
            title="Toggle sidebar"
            className="flex size-8 shrink-0 items-center justify-center rounded-xl text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          >
            <PanelLeft className="size-4" />
          </button>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-2">
          {/* Primary actions */}
          <div className="flex flex-col gap-0.5">
            <SidebarButton collapsed={collapsed} title="New Chat" onClick={openNewChat}>
              <Plus className="size-4 shrink-0" />
              {!collapsed && <span className="truncate">New Chat</span>}
            </SidebarButton>
            <SidebarButton
              collapsed={collapsed}
              title="Schedule"
              onClick={() => setScheduleOpen(true)}
            >
              <CalendarClock className="size-4 shrink-0" />
              {!collapsed && <span className="truncate">Schedule</span>}
            </SidebarButton>
            {PRIMARY_NAV.filter((item) => !item.href?.startsWith("/admin") || isAdmin).map(
              ({ icon: Icon, label, href }) => {
                const active = href ? pathname === href || pathname?.startsWith(`${href}/`) : false;
                return (
                  <SidebarButton
                    key={label}
                    collapsed={collapsed}
                    title={label}
                    active={active}
                    onClick={href ? () => navigateTo(href) : undefined}
                  >
                    <Icon className="size-4 shrink-0" />
                    {!collapsed && <span className="truncate">{label}</span>}
                  </SidebarButton>
                );
              },
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
            </>
          )}
        </nav>

        <div className="border-t border-sidebar-border bg-sidebar/80 p-2">
          <div className="flex items-center gap-2">
            <Link
              href="/profile"
              title="Profile"
              className={cn(
                "flex min-w-0 flex-1 items-center gap-2 rounded-xl px-2 py-1.5 text-sidebar-foreground/90 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                collapsed && "justify-center px-0",
              )}
            >
              <div className="grid size-7 shrink-0 place-items-center rounded-full bg-primary text-[11px] font-medium text-primary-foreground">
                {userInitial}
              </div>
              {!collapsed && (
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{userLabel}</div>
                  {userSubtitle && (
                    <div className="truncate text-[11px] text-sidebar-foreground/55">
                      {userSubtitle}
                    </div>
                  )}
                </div>
              )}
            </Link>
            {!collapsed && (
              <button
                type="button"
                title="Sign out"
                aria-label="Sign out"
                onClick={() => void signOut({ callbackUrl: "/login" })}
                className="grid size-8 shrink-0 place-items-center rounded-xl text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
              >
                <LogOut className="size-4" />
              </button>
            )}
          </div>
        </div>
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
      {scheduleOpen && (
        <ScheduleTaskDialog
          models={models}
          defaultModelAlias={selectedModelAlias}
          onClose={() => setScheduleOpen(false)}
          onCreated={() => {
            refreshConversations();
          }}
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
        "flex h-8 items-center gap-2 rounded-xl px-2.5 text-sm text-sidebar-foreground/80 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground shadow-sm",
        collapsed && "justify-center px-0",
      )}
    >
      {children}
    </button>
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
          <ChatTypeIcon type={chatType} />
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

function ChatTypeIcon({ type }: { type: string }) {
  if (type === "scheduled_task") {
    return <CalendarClock className="size-4 shrink-0 text-sidebar-foreground/55" />;
  }
  return <MessagesSquare className="size-4 shrink-0 text-sidebar-foreground/45" />;
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

type ScheduleFormState = {
  name: string;
  scheduleKind: "interval" | "cron" | "once";
  everySeconds: string;
  cron: string;
  runAt: string;
  timezone: string;
  modelAlias: string;
  prompt: string;
  files: ScheduledAttachment[];
};

type ScheduledTaskSummary = {
  id: string;
  name: string;
  status: "active" | "paused" | "completed";
  schedule_kind: "interval" | "cron" | "once";
  schedule_spec: Record<string, unknown>;
  timezone: string;
  prompt: string;
  model_alias: string;
  next_run_at?: string;
  last_run_at?: string;
  last_status: string;
  last_error: string;
  run_count: number;
};

type ScheduledAttachment = {
  filename: string;
  mime: string;
  size_bytes: number;
  content_b64: string;
};

type SchedulePromptState =
  | {
      kind: "notice";
      title: string;
      message: string;
      tone?: "default" | "danger";
    }
  | {
      kind: "confirm";
      title: string;
      message: string;
      confirmLabel: string;
      tone?: "default" | "danger";
      onConfirm: () => void;
    };

function ScheduleTaskDialog({
  models,
  defaultModelAlias,
  onClose,
  onCreated,
}: {
  models: { alias: string; label: string }[];
  defaultModelAlias: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [form, setForm] = useState<ScheduleFormState>(() =>
    emptyScheduleForm(defaultModelAlias || models[0]?.alias || ""),
  );
  const [tasks, setTasks] = useState<ScheduledTaskSummary[]>([]);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [loadingTasks, setLoadingTasks] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [prompt, setPrompt] = useState<SchedulePromptState | null>(null);

  const loadTasks = useCallback(async () => {
    setLoadingTasks(true);
    try {
      const res = await fetch("/api/scheduled-tasks", { cache: "no-store" });
      if (!res.ok) throw new Error(await res.text());
      const body = (await res.json()) as { tasks?: ScheduledTaskSummary[] };
      setTasks(body.tasks ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoadingTasks(false);
    }
  }, []);

  useEffect(() => {
    void loadTasks();
  }, [loadTasks]);

  function resetForm() {
    setEditingId(null);
    setForm(emptyScheduleForm(defaultModelAlias || models[0]?.alias || ""));
  }

  function editTask(task: ScheduledTaskSummary) {
    setEditingId(task.id);
    setForm({
      name: task.name,
      scheduleKind: task.schedule_kind,
      everySeconds: String(Number(task.schedule_spec?.every_seconds ?? 3600)),
      cron: String(task.schedule_spec?.expression ?? "0 * * * *"),
      runAt: toLocalInput(String(task.schedule_spec?.run_at ?? "")),
      timezone: task.timezone || "Asia/Shanghai",
      modelAlias: task.model_alias,
      prompt: task.prompt,
      files: [],
    });
  }

  async function submit(options?: { skipDistantOnceWarning?: boolean }) {
    const frequencyError = validateScheduleFrequency(form);
    if (frequencyError) {
      setPrompt({
        kind: "notice",
        title: "Invalid schedule",
        message: frequencyError,
        tone: "danger",
      });
      return;
    }
    const distantOnceWarning = distantOnceScheduleWarning(form);
    if (distantOnceWarning && !options?.skipDistantOnceWarning) {
      setPrompt({
        kind: "confirm",
        title: "Confirm schedule time",
        message: distantOnceWarning,
        confirmLabel: editingId ? "Save" : "Create",
        onConfirm: () => void submit({ skipDistantOnceWarning: true }),
      });
      return;
    }
    setSaving(true);
    setError("");
    try {
      const res = await fetch(
        editingId
          ? `/api/scheduled-tasks/${encodeURIComponent(editingId)}`
          : "/api/scheduled-tasks",
        {
          method: editingId ? "PATCH" : "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(toSchedulePayload(form, !editingId || form.files.length > 0)),
        },
      );
      if (!res.ok) throw new Error(await res.text());
      resetForm();
      await loadTasks();
      onCreated();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("INVALID_SCHEDULE_FREQUENCY")) {
        setPrompt({
          kind: "notice",
          title: "Invalid schedule",
          message: "Scheduled tasks can run at most once per hour.",
          tone: "danger",
        });
      } else if (msg.includes("INVALID_SCHEDULE_TIME")) {
        setPrompt({
          kind: "notice",
          title: "Invalid schedule",
          message: "Scheduled time must be in the future.",
          tone: "danger",
        });
      }
      setError(msg);
    } finally {
      setSaving(false);
    }
  }

  async function mutateTask(task: ScheduledTaskSummary, action: "pause" | "resume" | "delete") {
    setSaving(true);
    setError("");
    try {
      const res = await fetch(
        action === "delete"
          ? `/api/scheduled-tasks/${encodeURIComponent(task.id)}`
          : `/api/scheduled-tasks/${encodeURIComponent(task.id)}/${action}`,
        { method: action === "delete" ? "DELETE" : "POST" },
      );
      if (!res.ok) throw new Error(await res.text());
      if (editingId === task.id) resetForm();
      await loadTasks();
      onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function onFiles(files: FileList | null) {
    if (!files) return;
    const next: ScheduledAttachment[] = [];
    for (const file of Array.from(files)) {
      next.push({
        filename: file.name,
        mime: file.type || "application/octet-stream",
        size_bytes: file.size,
        content_b64: await fileToBase64(file),
      });
    }
    setForm((prev) => ({ ...prev, files: next }));
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/35 px-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="schedule-task-title"
        className="max-h-[88vh] w-full max-w-4xl overflow-hidden rounded-lg border border-border bg-background text-foreground shadow-xl"
      >
        <header className="flex items-center gap-3 border-b border-border p-4">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
            <CalendarClock className="size-4" />
          </div>
          <h2 id="schedule-task-title" className="min-w-0 flex-1 truncate text-sm font-semibold">
            Scheduled Tasks
          </h2>
          <button
            type="button"
            onClick={resetForm}
            className="h-8 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            New
          </button>
        </header>
        <div className="grid max-h-[calc(88vh-4rem)] gap-0 overflow-y-auto md:grid-cols-[1fr_1.05fr]">
          <div className="border-b border-border p-4 md:border-b-0 md:border-r">
            <div className="mb-3 text-xs font-medium text-muted-foreground">
              {editingId ? "Edit task" : "Create task"}
            </div>
            <div className="grid gap-3">
              <ScheduleField label="Name">
                <input
                  className={scheduleInputClass}
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </ScheduleField>
              <ScheduleField label="Model">
                <select
                  className={scheduleInputClass}
                  value={form.modelAlias}
                  onChange={(e) => setForm({ ...form, modelAlias: e.target.value })}
                >
                  {models.map((model) => (
                    <option key={model.alias} value={model.alias}>
                      {model.label || model.alias}
                    </option>
                  ))}
                </select>
              </ScheduleField>
              <div className="grid grid-cols-2 gap-2">
                <ScheduleField label="Schedule">
                  <select
                    className={scheduleInputClass}
                    value={form.scheduleKind}
                    onChange={(e) => {
                      const scheduleKind = e.target.value as ScheduleFormState["scheduleKind"];
                      setForm({
                        ...form,
                        scheduleKind,
                        runAt:
                          scheduleKind === "once" && !form.runAt
                            ? nextLocalDateTimeInput()
                            : form.runAt,
                      });
                    }}
                  >
                    <option value="interval">Interval</option>
                    <option value="cron">Cron</option>
                    <option value="once">Once</option>
                  </select>
                </ScheduleField>
                <ScheduleField label="Timezone">
                  <input
                    className={scheduleInputClass}
                    value={form.timezone}
                    onChange={(e) => setForm({ ...form, timezone: e.target.value })}
                  />
                </ScheduleField>
              </div>
              {form.scheduleKind === "interval" ? (
                <ScheduleField label="Every Seconds">
                  <input
                    className={scheduleInputClass}
                    value={form.everySeconds}
                    onChange={(e) => setForm({ ...form, everySeconds: e.target.value })}
                  />
                </ScheduleField>
              ) : null}
              {form.scheduleKind === "cron" ? (
                <ScheduleField label="Cron">
                  <input
                    className={scheduleInputClass}
                    value={form.cron}
                    onChange={(e) => setForm({ ...form, cron: e.target.value })}
                  />
                </ScheduleField>
              ) : null}
              {form.scheduleKind === "once" ? (
                <ScheduleField label="Run At">
                  <input
                    type="datetime-local"
                    className={scheduleInputClass}
                    value={form.runAt}
                    onChange={(e) => setForm({ ...form, runAt: e.target.value })}
                  />
                </ScheduleField>
              ) : null}
              <ScheduleField label="Prompt">
                <textarea
                  className="min-h-28 rounded-md border border-input bg-background px-3 py-2 text-sm outline-none transition-colors focus:border-ring focus:ring-1 focus:ring-ring"
                  value={form.prompt}
                  onChange={(e) => setForm({ ...form, prompt: e.target.value })}
                />
              </ScheduleField>
              <ScheduleField label="Files">
                <input
                  className={scheduleInputClass}
                  type="file"
                  multiple
                  onChange={(e) => void onFiles(e.target.files)}
                />
              </ScheduleField>
              {form.files.length ? (
                <div className="rounded-md border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
                  {form.files.map((file) => file.filename).join(", ")}
                </div>
              ) : null}
              {error ? (
                <div className="rounded-md border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-500">
                  {error}
                </div>
              ) : null}
              <div className="flex justify-end gap-2 pt-1">
                <button
                  type="button"
                  disabled={saving}
                  onClick={onClose}
                  className="h-8 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                >
                  Close
                </button>
                <button
                  type="button"
                  disabled={
                    saving ||
                    !form.name.trim() ||
                    !form.prompt.trim() ||
                    !form.modelAlias ||
                    (form.scheduleKind === "once" && !form.runAt)
                  }
                  onClick={() => void submit()}
                  className="h-8 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
                >
                  {saving ? "Saving..." : editingId ? "Save" : "Create"}
                </button>
              </div>
            </div>
          </div>
          <div className="p-4">
            <div className="mb-3 flex items-center justify-between gap-2">
              <div className="text-xs font-medium text-muted-foreground">My tasks</div>
              <button
                type="button"
                disabled={loadingTasks}
                onClick={() => void loadTasks()}
                className="h-7 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
              >
                Refresh
              </button>
            </div>
            {loadingTasks ? (
              <div className="py-8 text-center text-sm text-muted-foreground">Loading...</div>
            ) : tasks.length ? (
              <div className="grid gap-2">
                {tasks.map((task) => (
                  <div key={task.id} className="rounded-md border border-border bg-muted/20 p-3">
                    <div className="flex items-start gap-2">
                      <CalendarClock className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">{task.name}</div>
                        <div className="mt-0.5 text-xs text-muted-foreground">
                          {formatTaskSchedule(task)} · {task.status}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          Next {formatDate(task.next_run_at)} · Runs {task.run_count}
                        </div>
                        {task.last_error ? (
                          <div className="mt-1 truncate text-xs text-red-500">
                            {task.last_error}
                          </div>
                        ) : null}
                      </div>
                    </div>
                    <div className="mt-3 flex justify-end gap-1">
                      <button
                        type="button"
                        title="Edit"
                        onClick={() => editTask(task)}
                        className="grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                      >
                        <Pencil className="size-4" />
                      </button>
                      <button
                        type="button"
                        title={task.status === "active" ? "Pause" : "Resume"}
                        onClick={() =>
                          void mutateTask(task, task.status === "active" ? "pause" : "resume")
                        }
                        className="grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                      >
                        {task.status === "active" ? (
                          <Pause className="size-4" />
                        ) : (
                          <Play className="size-4" />
                        )}
                      </button>
                      <button
                        type="button"
                        title="Delete"
                        onClick={() =>
                          setPrompt({
                            kind: "confirm",
                            title: "Delete scheduled task",
                            message: `Delete "${task.name}"? This will stop future runs for this task.`,
                            confirmLabel: "Delete",
                            tone: "danger",
                            onConfirm: () => void mutateTask(task, "delete"),
                          })
                        }
                        className="grid size-8 place-items-center rounded-md text-red-500 hover:bg-red-500/10"
                      >
                        <Trash2 className="size-4" />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-md border border-dashed border-border py-8 text-center text-sm text-muted-foreground">
                No scheduled tasks
              </div>
            )}
          </div>
        </div>
      </div>
      {prompt ? (
        <ScheduleTaskPrompt prompt={prompt} saving={saving} onClose={() => setPrompt(null)} />
      ) : null}
    </div>
  );
}

function ScheduleTaskPrompt({
  prompt,
  saving,
  onClose,
}: {
  prompt: SchedulePromptState;
  saving: boolean;
  onClose: () => void;
}) {
  const isDanger = prompt.tone === "danger";
  return (
    <div className="fixed inset-0 z-[60] grid place-items-center bg-black/40 px-4">
      <div
        role="dialog"
        aria-modal="true"
        className="w-full max-w-sm rounded-lg border border-border bg-background p-4 text-foreground shadow-xl"
      >
        <div className="flex items-start gap-3">
          <div
            className={`grid size-9 shrink-0 place-items-center rounded-md ${
              isDanger ? "bg-red-500/10 text-red-500" : "bg-primary/10 text-primary"
            }`}
          >
            {isDanger ? <Trash2 className="size-4" /> : <CalendarClock className="size-4" />}
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-semibold">{prompt.title}</div>
            <div className="mt-1 text-sm leading-5 text-muted-foreground">{prompt.message}</div>
          </div>
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            disabled={saving}
            onClick={onClose}
            className="h-8 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
          >
            {prompt.kind === "confirm" ? "Cancel" : "Close"}
          </button>
          {prompt.kind === "confirm" ? (
            <button
              type="button"
              disabled={saving}
              onClick={() => {
                const onConfirm = prompt.onConfirm;
                onClose();
                onConfirm();
              }}
              className={`h-8 rounded-md px-3 text-sm font-medium transition-colors disabled:opacity-50 ${
                isDanger
                  ? "bg-red-500 text-white hover:bg-red-600"
                  : "bg-primary text-primary-foreground hover:bg-primary/90"
              }`}
            >
              {saving ? "Working..." : prompt.confirmLabel}
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function emptyScheduleForm(modelAlias: string): ScheduleFormState {
  return {
    name: "",
    scheduleKind: "interval",
    everySeconds: "3600",
    cron: "0 * * * *",
    runAt: "",
    timezone: "Asia/Shanghai",
    modelAlias,
    prompt: "",
    files: [],
  };
}

function ScheduleField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">
      {label}
      {children}
    </label>
  );
}

const scheduleInputClass =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm outline-none transition-colors focus:border-ring focus:ring-1 focus:ring-ring";

function toSchedulePayload(form: ScheduleFormState, includeAttachments = true) {
  const schedule_spec =
    form.scheduleKind === "interval"
      ? { every_seconds: Number.parseInt(form.everySeconds, 10) || 0 }
      : form.scheduleKind === "cron"
        ? { expression: form.cron }
        : { run_at: new Date(form.runAt).toISOString() };
  const payload: Record<string, unknown> = {
    name: form.name,
    status: "active",
    schedule_kind: form.scheduleKind,
    schedule_spec,
    timezone: form.timezone,
    prompt: form.prompt,
    model_alias: form.modelAlias,
    config_json: {},
  };
  if (includeAttachments) {
    payload.attachments = form.files;
  }
  return payload;
}

function validateScheduleFrequency(form: ScheduleFormState): string {
  if (form.scheduleKind === "interval") {
    const seconds = Number.parseInt(form.everySeconds, 10) || 0;
    if (seconds < 3600) return "Scheduled tasks can run at most once per hour.";
  }
  if (form.scheduleKind === "cron") {
    const [minute] = form.cron.trim().split(/\s+/);
    if (
      minute === "*" ||
      minute?.includes(",") ||
      (minute?.startsWith("*/") && Number(minute.slice(2)) < 60)
    ) {
      return "Scheduled tasks can run at most once per hour.";
    }
  }
  if (form.scheduleKind === "once" && form.runAt) {
    const runAtMs = new Date(form.runAt).getTime();
    if (!Number.isFinite(runAtMs) || runAtMs <= Date.now()) {
      return "Scheduled time must be in the future.";
    }
  }
  return "";
}

function distantOnceScheduleWarning(form: ScheduleFormState): string {
  if (form.scheduleKind !== "once" || !form.runAt) return "";
  const runAtMs = new Date(form.runAt).getTime();
  if (!Number.isFinite(runAtMs)) return "";
  const thirtyDaysMs = 30 * 24 * 60 * 60 * 1000;
  if (runAtMs - Date.now() <= thirtyDaysMs) return "";
  return `This task is scheduled for ${formatFullDateTime(runAtMs)}. Continue?`;
}

function formatTaskSchedule(task: ScheduledTaskSummary): string {
  if (task.schedule_kind === "interval") {
    const seconds = Number(task.schedule_spec?.every_seconds ?? 3600);
    return `Every ${seconds}s`;
  }
  if (task.schedule_kind === "cron") {
    return `Cron ${String(task.schedule_spec?.expression ?? "")}`;
  }
  return `Once ${formatDate(String(task.schedule_spec?.run_at ?? ""))}`;
}

function formatDate(value?: string): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString();
}

function toLocalInput(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const offsetMs = date.getTimezoneOffset() * 60 * 1000;
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16);
}

function nextLocalDateTimeInput(): string {
  const date = new Date(Date.now() + 5 * 60 * 1000);
  const offsetMs = date.getTimezoneOffset() * 60 * 1000;
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16);
}

function formatFullDateTime(value: number): string {
  return new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(value));
}

async function fileToBase64(file: File): Promise<string> {
  const buf = await file.arrayBuffer();
  const bytes = new Uint8Array(buf);
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-2.5 pb-1 pt-4 text-xs font-medium text-sidebar-foreground/50">
      {children}
    </div>
  );
}
