"use client";

import { useState } from "react";
import {
  FolderClosed,
  Hash,
  MessagesSquare,
  NotebookPen,
  PanelLeft,
  Plus,
  Search,
  LayoutGrid,
} from "lucide-react";
import { cn } from "@/lib/utils";

// Static, dependency-light sidebar mirroring the Open WebUI chrome (New Chat /
// Search / Notes / Workspace + Channels / Folders / Chats). Deliberately NOT
// wired to the backend — the cocola ExternalStore runtime only supports a
// single thread (send/cancel), so history / folders / channels are decorative
// shells until multi-thread persistence lands. Authored against the shadcn
// sidebar structure from @assistant-ui/ui but hand-rolled (plain divs + a
// useState collapse) to avoid pulling in Radix.

type NavItem = { icon: typeof Plus; label: string };

const PRIMARY_NAV: NavItem[] = [
  { icon: Plus, label: "New Chat" },
  { icon: Search, label: "Search" },
  { icon: NotebookPen, label: "Notes" },
  { icon: LayoutGrid, label: "Workspace" },
];

const CHANNELS = [{ icon: Hash, label: "general" }];

const FOLDERS = [
  { emoji: "💵", label: "Finance" },
  { emoji: "📕", label: "Study" },
];

const CHATS_TODAY = ["Roman Concrete Durability"];

export function AppSidebar() {
  const [collapsed, setCollapsed] = useState(false);

  return (
    <aside
      className={cn(
        "flex h-full shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-[width] duration-200",
        collapsed ? "w-[3.25rem]" : "w-64",
      )}
    >
      {/* Header: brand + collapse toggle */}
      <div className={cn("flex h-14 items-center gap-2 px-3", collapsed && "justify-center px-0")}>
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
          {PRIMARY_NAV.map(({ icon: Icon, label }) => (
            <SidebarButton key={label} collapsed={collapsed} title={label}>
              <Icon className="size-4 shrink-0" />
              {!collapsed && <span className="truncate">{label}</span>}
            </SidebarButton>
          ))}
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
            <div className="px-2.5 pb-1 pt-1 text-xs font-medium text-sidebar-foreground/50">
              Today
            </div>
            <div className="flex flex-col gap-0.5">
              {CHATS_TODAY.map((title) => (
                <SidebarButton key={title} collapsed={collapsed} title={title}>
                  <FolderClosed className="size-4 shrink-0 opacity-0" />
                  <span className="truncate">{title}</span>
                </SidebarButton>
              ))}
            </div>
          </>
        )}
      </nav>

      {/* Footer: user area (static placeholder) */}
      <div className="border-t border-sidebar-border p-2">
        <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
          <div className="grid size-6 shrink-0 place-items-center rounded-full bg-amber-500/90 text-[11px] font-medium text-white">
            U
          </div>
          {!collapsed && <span className="truncate text-sm">User</span>}
        </div>
      </div>
    </aside>
  );
}

function SidebarButton({
  children,
  collapsed,
  title,
}: {
  children: React.ReactNode;
  collapsed: boolean;
  title: string;
}) {
  return (
    <button
      type="button"
      title={title}
      className={cn(
        "flex h-8 items-center gap-2 rounded-md px-2.5 text-sm text-sidebar-foreground/90 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        collapsed && "justify-center px-0",
      )}
    >
      {children}
    </button>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-2.5 pb-1 pt-4 text-xs font-medium text-sidebar-foreground/50">
      {children}
    </div>
  );
}
