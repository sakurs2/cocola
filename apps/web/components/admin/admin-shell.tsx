"use client";

import { cn } from "@/lib/utils";
import {
  Activity,
  ArrowLeft,
  BookOpenText,
  Box,
  Clock3,
  Cpu,
  FileClock,
  LayoutDashboard,
  MessageSquareText,
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

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex h-screen bg-background text-foreground">
      <aside className="flex w-64 shrink-0 flex-col border-r border-border bg-card">
        <div className="flex h-16 items-center gap-3 border-b border-border px-4">
          <div className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground">
            <Activity className="size-4" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold">Admin</div>
            <div className="truncate text-xs text-muted-foreground">Monitoring</div>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 py-4">
          <Link
            href="/"
            className="mb-4 flex h-9 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          >
            <ArrowLeft className="size-4 shrink-0" />
            <span className="truncate">Back to chat</span>
          </Link>
          {NAV_GROUPS.map((group) => (
            <div key={group.label} className="mb-4">
              <div className="mb-1 px-2 text-xs font-medium text-muted-foreground">
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
                        "flex h-9 items-center gap-2 rounded-md px-2 text-sm transition-colors",
                        active
                          ? "bg-accent text-accent-foreground"
                          : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
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
      </aside>
      <div className="min-w-0 flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}
