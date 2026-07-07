"use client";

import { cn } from "@/lib/utils";
import {
  Activity,
  ArrowLeft,
  BarChart3,
  BookOpenText,
  Box,
  Clock3,
  Cpu,
  FileClock,
  LayoutDashboard,
  MessageSquareText,
  Network,
  PlugZap,
  Sparkles,
  Server,
  Settings,
  Terminal,
  Users,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

type AdminNavItem = {
  href: string;
  label: string;
  icon: typeof LayoutDashboard;
};

const NAV_GROUPS: { label: string; items: AdminNavItem[] }[] = [
  {
    label: "Overview",
    items: [
      { href: "/admin", label: "Summary", icon: LayoutDashboard },
      { href: "/admin/api-docs", label: "API Docs", icon: BookOpenText },
      { href: "/admin/token-usage", label: "Token Usage", icon: BarChart3 },
      { href: "/admin/architecture", label: "Architecture", icon: Network },
    ],
  },
  {
    label: "Access",
    items: [{ href: "/admin/users", label: "Users", icon: Users }],
  },
  {
    label: "AI",
    items: [
      { href: "/admin/models", label: "Models", icon: Cpu },
      { href: "/admin/prompts", label: "Prompt", icon: MessageSquareText },
      { href: "/admin/skills", label: "Skills", icon: Sparkles },
      { href: "/admin/mcps", label: "MCP", icon: PlugZap },
      { href: "/admin/scheduled-tasks", label: "Scheduled Tasks", icon: Clock3 },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { href: "/admin/sandboxes", label: "Sandbox Runtime", icon: Box },
      { href: "/admin/sandbox-nodes", label: "Sandbox Nodes", icon: Server },
    ],
  },
  {
    label: "Logs",
    items: [
      { href: "/admin/audit", label: "Audit Logs", icon: FileClock },
      { href: "/admin/component-logs", label: "Component Logs", icon: Terminal },
    ],
  },
  {
    label: "Settings",
    items: [{ href: "/admin/settings", label: "System Settings", icon: Settings }],
  },
];

const FALLBACK_NAV_ITEM = {
  href: "/admin",
  label: "Summary",
  icon: LayoutDashboard,
  group: "Overview",
};

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const navItems = NAV_GROUPS.flatMap((group) =>
    group.items.map((item) => ({ ...item, group: group.label })),
  );
  const currentItem =
    navItems.find((item) =>
      item.href === "/admin" ? pathname === item.href : pathname.startsWith(item.href),
    ) ?? FALLBACK_NAV_ITEM;

  return (
    <div className="cocola-admin-ui admin-ops-bg flex h-screen text-foreground">
      <aside className="admin-glass-sidebar m-2 flex w-64 shrink-0 flex-col overflow-hidden rounded-3xl border">
        <div className="flex h-16 items-center gap-3 border-b border-white/35 px-4">
          <div className="grid size-9 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
            <Activity className="size-4" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold">Admin</div>
            <div className="truncate text-xs text-muted-foreground">Control plane</div>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 py-4">
          {NAV_GROUPS.map((group) => (
            <div key={group.label} className="mb-4">
              <div className="mb-1 px-2 text-xs font-semibold uppercase tracking-normal text-muted-foreground">
                {group.label}
              </div>
              <div className="space-y-1">
                {group.items.map((item) => {
                  const Icon = item.icon;
                  const active =
                    item.href === "/admin"
                      ? pathname === item.href
                      : pathname === item.href || pathname.startsWith(`${item.href}/`);
                  return (
                    <Link
                      key={item.href}
                      href={item.href}
                      className={cn(
                        "flex h-9 items-center gap-2 rounded-2xl px-2 text-sm transition-colors",
                        active
                          ? "bg-white/45 text-accent-foreground shadow-[inset_0_1px_0_hsl(0_0%_100%/0.65),0_10px_22px_hsl(207_78%_38%/0.11)]"
                          : "text-muted-foreground hover:bg-white/32 hover:text-accent-foreground",
                      )}
                    >
                      <Icon className="size-4 shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </Link>
                  );
                })}
              </div>
            </div>
          ))}
        </nav>
        <div className="border-t border-white/35 p-3">
          <Link
            href="/"
            className="flex h-10 items-center gap-2 rounded-2xl border border-white/45 bg-white/24 px-3 text-sm font-medium text-foreground shadow-[inset_0_1px_0_hsl(0_0%_100%/0.55)] backdrop-blur-md transition-colors hover:bg-white/42"
          >
            <ArrowLeft className="size-4 shrink-0" />
            <span className="truncate">Back to chat</span>
          </Link>
        </div>
      </aside>
      <section className="min-w-0 flex-1 py-2 pr-2">
        <div className="admin-glass-shell flex h-full min-w-0 flex-col overflow-hidden rounded-3xl border">
          <header className="admin-topbar sticky top-0 z-20 flex h-16 shrink-0 items-center justify-between gap-4 border-b px-5">
            <div className="min-w-0">
              <div className="text-xs font-semibold uppercase tracking-normal text-muted-foreground">
                {currentItem.group}
              </div>
              <div className="truncate text-base font-semibold">{currentItem.label}</div>
            </div>
            <div className="hidden items-center gap-2 text-xs text-muted-foreground sm:flex">
              <span className="rounded-full border border-white/45 bg-white/28 px-3 py-1">
                Operations
              </span>
              <span className="rounded-full border border-white/45 bg-white/28 px-3 py-1">
                Sky Glass
              </span>
            </div>
          </header>
          <div className="min-w-0 flex-1 overflow-y-auto">{children}</div>
        </div>
      </section>
    </div>
  );
}
