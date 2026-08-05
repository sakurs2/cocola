"use client";

import { Button } from "@heroui/react";
import { Sheet } from "@heroui-pro/react/sheet";
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
      {
        href: "/admin/mcps",
        label: "MCP Servers",
        icon: PlugsConnected,
        iconClassName: "text-orange-600",
      },
      {
        href: "/admin/toolbox",
        label: "Toolbox",
        icon: ToolboxIcon,
        iconClassName: "text-cyan-600",
      },
    ],
  },
  {
    label: "Operations",
    items: [
      {
        href: "/admin/scheduled-tasks",
        label: "Tasks",
        icon: ClockCountdown,
        iconClassName: "text-green-600",
      },
      {
        href: "/admin/audit",
        label: "Agent Runs",
        icon: FileText,
        match: ["/admin/traces"],
        iconClassName: "text-indigo-600",
      },
      {
        href: "/admin/token-usage",
        label: "Token Usage",
        icon: ChartLineUp,
        iconClassName: "text-rose-600",
      },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { href: "/admin/sandboxes", label: "Sandboxes", icon: Stack, iconClassName: "text-teal-600" },
      { href: "/admin/sandbox-nodes", label: "Nodes", icon: Cpu, iconClassName: "text-sky-600" },
      {
        href: "/admin/storage",
        label: "Storage",
        icon: HardDrives,
        iconClassName: "text-purple-600",
      },
      {
        href: "/admin/architecture",
        label: "Architecture",
        icon: Graph,
        iconClassName: "text-fuchsia-600",
      },
      {
        href: "/admin/component-logs",
        label: "Service Logs",
        icon: TerminalWindow,
        iconClassName: "text-slate-600",
      },
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
            <Button isIconOnly aria-label="Open admin navigation" className="md:hidden" variant="ghost" onPress={() => setMobileOpen(true)}><List className="size-[18px]" /></Button>
            <Sheet isOpen={mobileOpen} placement="left" onOpenChange={setMobileOpen}><Sheet.Backdrop><Sheet.Content className="cocola-admin-ui w-[min(19rem,calc(100vw-1rem))]"><Sheet.Dialog><Sheet.CloseTrigger aria-label="Close admin navigation" /><Sheet.Header><span className="flex items-center gap-3"><span className="bg-accent text-accent-foreground grid size-9 place-items-center rounded-2xl"><CocolaLogo mono className="size-5" /></span><span><Sheet.Heading>cocola admin</Sheet.Heading><span className="text-muted text-xs">control plane</span></span></span></Sheet.Header><Sheet.Body className="p-0"><div className="min-h-0 flex-1 overflow-y-auto" onClick={() => setMobileOpen(false)}><AdminNavigation pathname={pathname} mobile /></div></Sheet.Body><Sheet.Footer className="p-0"><AdminSidebarFooter onNavigate={() => setMobileOpen(false)} /></Sheet.Footer></Sheet.Dialog></Sheet.Content></Sheet.Backdrop></Sheet>

            <div className="min-w-0 flex-1">
              <div className="truncate text-[11px] font-semibold uppercase tracking-[0.14em] text-accent/65">
                Control plane
              </div>
              <div className="truncate text-sm font-medium text-foreground">
                {currentItem?.label}
              </div>
            </div>

            <div className="hidden items-center gap-2 sm:flex">
              <span className="admin-context-pill">
                <span className="size-1.5 rounded-full bg-emerald-500" />
                Self-hosted
              </span>
              <span className="admin-context-pill max-w-48 truncate">
                <ShieldCheck className="size-3.5 text-accent" />
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
      <div className="grid size-9 shrink-0 place-items-center rounded-2xl bg-accent text-accent-foreground shadow-lg shadow-accent/20">
        <CocolaLogo mono className="size-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[15px] font-bold text-foreground">cocola admin</div>
        <div className="truncate text-xs font-medium text-foreground/70">control plane</div>
      </div>
    </div>
  );
}

function AdminNavigation({ pathname, mobile = false }: { pathname: string; mobile?: boolean }) {
  return (
    <nav className={cn("min-h-0 flex-1 overflow-y-auto px-2 pb-3", mobile && "px-3 pt-3")}>
      <div className="mb-4">
        <AdminNavLink item={OVERVIEW_ITEM} pathname={pathname} />
      </div>
      {NAV_GROUPS.map((group) => (
        <div key={group.label} className="mb-3">
          <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted/75">
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
          ? "bg-default text-foreground"
          : "text-foreground hover:bg-default hover:text-foreground",
      )}
    >
      <Icon
        className={cn("size-4 shrink-0", item.iconClassName ?? "text-foreground")}
      />
      <span className="truncate">{item.label}</span>
    </Link>
  );
}

function AdminSidebarFooter({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <div className="border-t border-separator p-2.5">
      <Link
        href="/"
        onClick={onNavigate}
        className="flex h-10 items-center gap-2 rounded-xl border border-separator bg-background px-3 text-sm font-medium text-foreground transition-colors hover:bg-default hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/40"
      >
        <ArrowLeft className="size-4 shrink-0 text-accent" />
        <span className="truncate">Back to workspace</span>
      </Link>
    </div>
  );
}
