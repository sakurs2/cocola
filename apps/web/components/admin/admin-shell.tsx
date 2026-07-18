"use client";

import * as Dialog from "@radix-ui/react-dialog";
import {
  ArrowLeft,
  BarChart3 as ChartLineUp,
  Timer as ClockCountdown,
  Cpu,
  FileText,
  Settings as Gear,
  Workflow as Graph,
  HardDrive as HardDrives,
  Menu as List,
  Plug as PlugsConnected,
  ShieldCheck,
  Sparkles as Sparkle,
  LayoutGrid as SquaresFour,
  Layers as Stack,
  SquareTerminal as TerminalWindow,
  Wrench as ToolboxIcon,
  Users as UsersThree,
  X,
  type LucideIcon,
} from "lucide-react";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, type ReactNode } from "react";
import { CocolaLogo } from "@/components/cocola-logo";
import { cn } from "@/lib/utils";

type AdminNavItem = {
  href: string;
  label: string;
  icon: LucideIcon;
  match?: string[];
  iconClassName?: string;
};

const OVERVIEW_ITEM: AdminNavItem = {
  href: "/admin",
  label: "Overview",
  icon: SquaresFour,
  iconClassName: "text-blue-600",
};
const SETTINGS_ITEM: AdminNavItem = {
  href: "/admin/settings",
  label: "Settings",
  icon: Gear,
  iconClassName: "text-slate-500",
};

const NAV_GROUPS: { label: string; items: AdminNavItem[] }[] = [
  {
    label: "Configuration",
    items: [
      { href: "/admin/users", label: "Users", icon: UsersThree, iconClassName: "text-blue-600" },
      { href: "/admin/models", label: "Models", icon: Cpu, iconClassName: "text-violet-600" },
      { href: "/admin/skills", label: "Skills", icon: Sparkle, iconClassName: "text-amber-500" },
      { href: "/admin/mcps", label: "MCP Servers", icon: PlugsConnected, iconClassName: "text-orange-600" },
      { href: "/admin/toolbox", label: "Toolbox", icon: ToolboxIcon, iconClassName: "text-cyan-600" },
    ],
  },
  {
    label: "Operations",
    items: [
      { href: "/admin/scheduled-tasks", label: "Tasks", icon: ClockCountdown, iconClassName: "text-green-600" },
      { href: "/admin/audit", label: "Agent Runs", icon: FileText, match: ["/admin/traces"], iconClassName: "text-indigo-600" },
      { href: "/admin/token-usage", label: "Token Usage", icon: ChartLineUp, iconClassName: "text-rose-600" },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { href: "/admin/sandboxes", label: "Sandboxes", icon: Stack, iconClassName: "text-teal-600" },
      { href: "/admin/sandbox-nodes", label: "Nodes", icon: Cpu, iconClassName: "text-sky-600" },
      { href: "/admin/storage", label: "Storage", icon: HardDrives, iconClassName: "text-purple-600" },
      { href: "/admin/architecture", label: "Architecture", icon: Graph, iconClassName: "text-fuchsia-600" },
      { href: "/admin/component-logs", label: "Service Logs", icon: TerminalWindow, iconClassName: "text-slate-600" },
    ],
  },
  {
    label: "System",
    items: [SETTINGS_ITEM],
  },
];

const navItems = [
  { ...OVERVIEW_ITEM, group: "Overview" },
  ...NAV_GROUPS.flatMap((group) => group.items.map((item) => ({ ...item, group: group.label }))),
];

function isActive(pathname: string, item: AdminNavItem) {
  return [item.href, ...(item.match ?? [])].some((href) =>
    href === "/admin" ? pathname === href : pathname === href || pathname.startsWith(`${href}/`),
  );
}

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const { data: session } = useSession();
  const [mobileOpen, setMobileOpen] = useState(false);
  const currentItem = navItems.find((item) => isActive(pathname, item)) ?? navItems[0];
  const userLabel = session?.user?.name || session?.user?.email || "Administrator";

  return (
    <div className="cocola-admin-ui admin-ops-bg flex h-screen overflow-hidden font-sans text-foreground">
      <aside className="admin-glass-sidebar hidden w-[17rem] shrink-0 flex-col overflow-hidden border-r md:flex">
        <AdminBrand />
        <AdminNavigation pathname={pathname} />
        <AdminSidebarFooter />
      </aside>

      <section className="flex min-w-0 flex-1 flex-col">
        <div className="admin-glass-shell flex h-full min-w-0 flex-col overflow-hidden">
          <header className="admin-topbar relative z-20 flex h-14 shrink-0 items-center gap-3 border-b px-3 sm:px-5">
            <Dialog.Root open={mobileOpen} onOpenChange={setMobileOpen}>
              <Dialog.Trigger asChild>
                <button
                  type="button"
                  aria-label="Open admin navigation"
                  className="inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 md:hidden"
                >
                  <List className="size-[18px]" />
                </button>
              </Dialog.Trigger>
              <Dialog.Portal>
                <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/20 data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out data-[state=open]:fade-in" />
                <Dialog.Content className="cocola-admin-ui admin-mobile-nav fixed inset-y-2 left-2 z-50 flex w-[min(19rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-3xl border text-foreground outline-none data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left">
                  <Dialog.Title className="sr-only">Admin navigation</Dialog.Title>
                  <Dialog.Description className="sr-only">
                    Navigate between control plane pages.
                  </Dialog.Description>
                  <div className="flex h-16 items-center gap-3 border-b border-border px-4">
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
                      <AdminNavigation pathname={pathname} mobile />
                    </div>
                  </Dialog.Close>
                  <AdminSidebarFooter onNavigate={() => setMobileOpen(false)} />
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
                <ShieldCheck className="size-3.5 text-primary" />
                {userLabel}
              </span>
            </div>
          </header>
          <div className="min-w-0 flex-1 overflow-y-auto">{children}</div>
        </div>
      </section>
    </div>
  );
}

function AdminBrand() {
  return (
    <div className="flex h-16 shrink-0 items-center gap-2 px-3">
      <div className="grid size-9 shrink-0 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
        <CocolaLogo mono className="size-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[15px] font-bold text-sidebar-foreground">cocola admin</div>
        <div className="truncate text-xs font-medium text-sidebar-foreground/70">control plane</div>
      </div>
    </div>
  );
}

function AdminNavigation({
  pathname,
  mobile = false,
}: {
  pathname: string;
  mobile?: boolean;
}) {
  return (
    <nav className={cn("min-h-0 flex-1 overflow-y-auto px-2 pb-3", mobile && "px-3 pt-3")}>
      <div className="mb-4">
        <AdminNavLink item={OVERVIEW_ITEM} pathname={pathname} />
      </div>
      {NAV_GROUPS.map((group) => (
        <div key={group.label} className="mb-3">
          <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/75">
            {group.label}
          </div>
          <div className="space-y-1">
            {group.items.map((item) => (
              <AdminNavLink key={item.href} item={item} pathname={pathname} />
            ))}
          </div>
        </div>
      ))}
    </nav>
  );
}

function AdminNavLink({
  item,
  pathname,
  onNavigate,
}: {
  item: AdminNavItem;
  pathname: string;
  onNavigate?: () => void;
}) {
  const active = isActive(pathname, item);
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      onClick={onNavigate}
      className={cn(
        "admin-nav-item group flex h-9 items-center gap-2.5 rounded-xl px-2.5 text-[13.5px] font-medium",
        active
          ? "bg-sidebar-accent text-sidebar-accent-foreground"
          : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
      )}
    >
      <Icon className={cn("size-4 shrink-0", item.iconClassName ?? "text-sidebar-accent-foreground")} />
      <span className="truncate">{item.label}</span>
    </Link>
  );
}

function AdminSidebarFooter({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <div className="border-t border-sidebar-border p-2.5">
      <Link
        href="/"
        onClick={onNavigate}
        className="flex h-10 items-center gap-2 rounded-xl border border-sidebar-border bg-sidebar px-3 text-sm font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
      >
        <ArrowLeft className="size-4 shrink-0 text-primary" />
        <span className="truncate">Back to workspace</span>
      </Link>
    </div>
  );
}
