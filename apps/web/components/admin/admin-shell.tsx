"use client";

import * as Dialog from "@radix-ui/react-dialog";
import {
  ArrowLeft,
  ChartLineUp,
  ClockCountdown,
  Cpu,
  FileText,
  Gear,
  Graph,
  List,
  PlugsConnected,
  ShieldCheck,
  SidebarSimple,
  Sparkle,
  SquaresFour,
  Stack,
  TerminalWindow,
  UsersThree,
  X,
  type Icon as PhosphorIcon,
} from "@phosphor-icons/react";
import { MotionConfig, motion } from "framer-motion";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, type ReactNode } from "react";
import { CocolaLogo } from "@/components/cocola-logo";
import { cn } from "@/lib/utils";

type AdminNavItem = {
  href: string;
  label: string;
  icon: PhosphorIcon;
};

const NAV_GROUPS: { label: string; items: AdminNavItem[] }[] = [
  {
    label: "Overview",
    items: [
      { href: "/admin", label: "Summary", icon: SquaresFour },
      { href: "/admin/token-usage", label: "Token Usage", icon: ChartLineUp },
      { href: "/admin/architecture", label: "Architecture", icon: Graph },
    ],
  },
  {
    label: "Access",
    items: [{ href: "/admin/users", label: "Users", icon: UsersThree }],
  },
  {
    label: "AI",
    items: [
      { href: "/admin/models", label: "Models", icon: Cpu },
      { href: "/admin/prompts", label: "Prompt", icon: FileText },
      { href: "/admin/skills", label: "Skills", icon: Sparkle },
      { href: "/admin/mcps", label: "MCP", icon: PlugsConnected },
      { href: "/admin/scheduled-tasks", label: "Scheduled Tasks", icon: ClockCountdown },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { href: "/admin/sandboxes", label: "Sandbox Runtime", icon: Stack },
      { href: "/admin/sandbox-nodes", label: "Sandbox Nodes", icon: Cpu },
    ],
  },
  {
    label: "Logs",
    items: [
      { href: "/admin/audit", label: "Audit Logs", icon: FileText },
      { href: "/admin/component-logs", label: "Component Logs", icon: TerminalWindow },
    ],
  },
  {
    label: "Settings",
    items: [{ href: "/admin/settings", label: "System Settings", icon: Gear }],
  },
];

const navItems = NAV_GROUPS.flatMap((group) =>
  group.items.map((item) => ({ ...item, group: group.label })),
);

function isActive(pathname: string, href: string) {
  return href === "/admin"
    ? pathname === href
    : pathname === href || pathname.startsWith(`${href}/`);
}

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const { data: session } = useSession();
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const currentItem = navItems.find((item) => isActive(pathname, item.href)) ?? navItems[0];
  const userLabel = session?.user?.name || session?.user?.email || "Administrator";

  return (
    <MotionConfig reducedMotion="user">
      <div className="cocola-admin-ui admin-ops-bg flex h-screen overflow-hidden font-sans text-foreground">
        <motion.aside
          initial={false}
          animate={{ width: collapsed ? 64 : 272 }}
          transition={{ type: "spring", stiffness: 380, damping: 36 }}
          className="admin-glass-sidebar m-1.5 hidden shrink-0 flex-col overflow-hidden rounded-[1.4rem] border md:flex"
        >
          <AdminBrand collapsed={collapsed} onCollapse={() => setCollapsed((value) => !value)} />
          <AdminNavigation pathname={pathname} collapsed={collapsed} />
          <AdminSidebarFooter collapsed={collapsed} />
        </motion.aside>

        <section className="min-w-0 flex-1 py-1.5 pr-1.5 max-md:pl-1.5">
          <div className="admin-glass-shell flex h-full min-w-0 flex-col overflow-hidden rounded-[1.4rem] border">
            <header className="admin-topbar relative z-20 flex h-14 shrink-0 items-center gap-3 border-b px-3 sm:px-5">
              <Dialog.Root open={mobileOpen} onOpenChange={setMobileOpen}>
                <Dialog.Trigger asChild>
                  <button
                    type="button"
                    aria-label="Open admin navigation"
                    className="inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground transition-colors hover:bg-white/55 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 md:hidden"
                  >
                    <List className="size-[18px]" weight="duotone" />
                  </button>
                </Dialog.Trigger>
                <Dialog.Portal>
                  <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/20 backdrop-blur-sm data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out data-[state=open]:fade-in" />
                  <Dialog.Content className="cocola-admin-ui admin-mobile-nav fixed inset-y-2 left-2 z-50 flex w-[min(19rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-3xl border text-foreground outline-none data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left">
                    <Dialog.Title className="sr-only">Admin navigation</Dialog.Title>
                    <Dialog.Description className="sr-only">
                      Navigate between control plane pages.
                    </Dialog.Description>
                    <div className="flex h-16 items-center gap-3 border-b border-border/70 px-4">
                      <div className="grid size-9 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
                        <CocolaLogo mono className="size-5" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-semibold">cocola admin</div>
                        <div className="truncate text-[11px] text-muted-foreground">
                          control plane
                        </div>
                      </div>
                      <Dialog.Close className="inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground hover:bg-muted hover:text-foreground">
                        <X className="size-4" />
                      </Dialog.Close>
                    </div>
                    <Dialog.Close asChild>
                      <div className="min-h-0 flex-1 overflow-y-auto">
                        <AdminNavigation pathname={pathname} collapsed={false} mobile />
                      </div>
                    </Dialog.Close>
                    <AdminSidebarFooter collapsed={false} />
                  </Dialog.Content>
                </Dialog.Portal>
              </Dialog.Root>

              <div className="min-w-0 flex-1">
                <div className="truncate text-[11px] font-semibold uppercase tracking-[0.14em] text-primary/65">
                  Control plane
                </div>
                <div className="truncate text-sm font-medium text-foreground">
                  {currentItem?.group}
                </div>
              </div>

              <div className="hidden items-center gap-2 sm:flex">
                <span className="admin-context-pill">
                  <span className="size-1.5 rounded-full bg-emerald-500" />
                  Self-hosted
                </span>
                <span className="admin-context-pill max-w-48 truncate">
                  <ShieldCheck className="size-3.5 text-primary" weight="duotone" />
                  {userLabel}
                </span>
              </div>
            </header>
            <div className="min-w-0 flex-1 overflow-y-auto">{children}</div>
          </div>
        </section>
      </div>
    </MotionConfig>
  );
}

function AdminBrand({ collapsed, onCollapse }: { collapsed: boolean; onCollapse: () => void }) {
  return (
    <div
      className={cn(
        "flex h-16 shrink-0 items-center gap-2 px-3",
        collapsed && "justify-center px-2",
      )}
    >
      {collapsed ? (
        <button
          type="button"
          title="Expand sidebar"
          aria-label="Expand sidebar"
          onClick={onCollapse}
          className="admin-rail-button"
        >
          <CocolaLogo className="size-7" />
        </button>
      ) : (
        <>
          <div className="grid size-9 shrink-0 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
            <CocolaLogo mono className="size-5" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">cocola admin</div>
            <div className="truncate text-[11px] text-muted-foreground">control plane</div>
          </div>
          <button
            type="button"
            title="Collapse sidebar"
            aria-label="Collapse sidebar"
            onClick={onCollapse}
            className="admin-rail-button"
          >
            <SidebarSimple className="size-4" weight="duotone" />
          </button>
        </>
      )}
    </div>
  );
}

function AdminNavigation({
  pathname,
  collapsed,
  mobile = false,
}: {
  pathname: string;
  collapsed: boolean;
  mobile?: boolean;
}) {
  return (
    <nav className={cn("min-h-0 flex-1 overflow-y-auto px-2 pb-3", mobile && "px-3 pt-3")}>
      {NAV_GROUPS.map((group) => (
        <div key={group.label} className={cn("mb-3", collapsed && "mb-2")}>
          {!collapsed ? (
            <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/75">
              {group.label}
            </div>
          ) : null}
          <div
            className={cn("space-y-1", collapsed && "flex flex-col items-center gap-1 space-y-0")}
          >
            {group.items.map((item) => {
              const active = isActive(pathname, item.href);
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  title={collapsed ? item.label : undefined}
                  aria-label={collapsed ? item.label : undefined}
                  className={cn(
                    "admin-nav-item group relative flex h-9 items-center gap-2.5 overflow-hidden rounded-xl px-2.5 text-sm",
                    collapsed && "size-10 justify-center px-0",
                    active ? "text-primary" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {active ? (
                    <motion.span
                      layoutId={mobile ? "admin-mobile-active-nav" : "admin-active-nav"}
                      className="absolute inset-0 rounded-xl border border-white/65 bg-white/58 shadow-[inset_0_1px_0_hsl(0_0%_100%/0.85),0_8px_20px_hsl(221_83%_53%/0.1)]"
                      transition={{ type: "spring", stiffness: 420, damping: 38 }}
                    />
                  ) : null}
                  <Icon className="relative z-[1] size-[17px] shrink-0" weight="duotone" />
                  {!collapsed ? (
                    <span className="relative z-[1] truncate">{item.label}</span>
                  ) : null}
                </Link>
              );
            })}
          </div>
        </div>
      ))}
    </nav>
  );
}

function AdminSidebarFooter({ collapsed }: { collapsed: boolean }) {
  return (
    <div className="border-t border-white/35 p-2.5">
      <Link
        href="/"
        title={collapsed ? "Back to workspace" : undefined}
        className={cn(
          "flex h-10 items-center gap-2 rounded-xl border border-white/50 bg-white/28 px-3 text-sm font-medium text-foreground shadow-[inset_0_1px_0_hsl(0_0%_100%/0.7)] transition-colors hover:bg-white/52 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
          collapsed && "justify-center px-0",
        )}
      >
        <ArrowLeft className="size-4 shrink-0 text-primary" weight="duotone" />
        {!collapsed ? <span className="truncate">Back to workspace</span> : null}
      </Link>
    </div>
  );
}
