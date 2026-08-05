"use client";

import {
  BarChart3 as ChartLineUp,
  Timer as ClockCountdown,
  Cpu,
  FileText,
  Workflow as Graph,
  HardDrive as HardDrives,
  Plug as PlugsConnected,
  Sparkles as Sparkle,
  Layers as Stack,
  SquareTerminal as TerminalWindow,
  Wrench as ToolboxIcon,
  Users as UsersThree,
  type LucideIcon as PhosphorIcon,
} from "lucide-react";
import { ArrowRight } from "lucide-react";
import Link from "next/link";
import { Card, Chip } from "@heroui/react";
import { AdminPage as AdminPageLayout, AdminPageHeader } from "@/components/admin/admin-ui";

type AdminModule = {
  title: string;
  href: string;
  icon: PhosphorIcon;
  summary: string;
  from: string;
  to: string;
};

const MODULE_GROUPS: { label: string; modules: AdminModule[] }[] = [
  {
    label: "Configuration",
    modules: [
      {
        title: "Users",
        href: "/admin/users",
        icon: UsersThree,
        summary: "Manage whitelist accounts, roles, teams, and account status.",
        from: "#60a5fa",
        to: "#2563eb",
      },
      {
        title: "Models",
        href: "/admin/models",
        icon: Cpu,
        summary: "Configure providers, aliases, credentials, and the default route.",
        from: "#a78bfa",
        to: "#7c3aed",
      },
      {
        title: "Skills",
        href: "/admin/skills",
        icon: Sparkle,
        summary: "Review installed skills and the capabilities exposed to agents.",
        from: "#fbbf24",
        to: "#f59e0b",
      },
      {
        title: "MCP Servers",
        href: "/admin/mcps",
        icon: PlugsConnected,
        summary: "Manage MCP servers, transport settings, and availability.",
        from: "#fb923c",
        to: "#ea580c",
      },
      {
        title: "Toolbox",
        href: "/admin/toolbox",
        icon: ToolboxIcon,
        summary: "Open lightweight admin controls such as the global system prompt.",
        from: "#22d3ee",
        to: "#0891b2",
      },
    ],
  },
  {
    label: "Operations",
    modules: [
      {
        title: "Tasks",
        href: "/admin/scheduled-tasks",
        icon: ClockCountdown,
        summary: "Review user-owned schedules, task status, and recent results.",
        from: "#4ade80",
        to: "#16a34a",
      },
      {
        title: "Agent Runs",
        href: "/admin/audit",
        icon: FileText,
        summary: "Inspect conversation runs, traces, timing, and failures.",
        from: "#818cf8",
        to: "#4f46e5",
      },
      {
        title: "Token Usage",
        href: "/admin/token-usage",
        icon: ChartLineUp,
        summary: "Review token totals, usage trends, ranked users, and exports.",
        from: "#fb7185",
        to: "#e11d48",
      },
    ],
  },
  {
    label: "Infrastructure",
    modules: [
      {
        title: "Sandboxes",
        href: "/admin/sandboxes",
        icon: Stack,
        summary: "Inspect active sandboxes, owners, bindings, and lifecycle state.",
        from: "#2dd4bf",
        to: "#0d9488",
      },
      {
        title: "Nodes",
        href: "/admin/sandbox-nodes",
        icon: Cpu,
        summary: "Track node health, pod capacity, placement, and node operations.",
        from: "#38bdf8",
        to: "#0284c7",
      },
      {
        title: "Storage",
        href: "/admin/storage",
        icon: HardDrives,
        summary: "Inspect node disk headroom, Session Volumes, and on-demand usage.",
        from: "#c084fc",
        to: "#9333ea",
      },
      {
        title: "Architecture",
        href: "/admin/architecture",
        icon: Graph,
        summary: "Inspect the system DAG, dependencies, and component health.",
        from: "#e879f9",
        to: "#c026d3",
      },
      {
        title: "Service Logs",
        href: "/admin/component-logs",
        icon: TerminalWindow,
        summary: "Read recent output from Cocola's core runtime services.",
        from: "#94a3b8",
        to: "#475569",
      },
    ],
  },
];

export default function AdminPage() {
  return (
    <AdminPageLayout>
      <AdminPageHeader icon={<Graph className="size-5" />} title="Overview" description="Open a control-plane area to manage configuration, operations, or infrastructure." />
      <div className="grid gap-5 xl:grid-cols-2">
        {MODULE_GROUPS.map((group) => (
          <section key={group.label} className="grid content-start gap-3">
            <div className="flex items-center justify-between px-1"><h2 className="text-sm font-semibold">{group.label}</h2><Chip size="sm" variant="soft">{group.modules.length}</Chip></div>
            <div className="grid items-stretch gap-3 sm:grid-cols-2">
              {group.modules.map((module) => {
                const Icon = module.icon;
                return (
                  <Link
                    key={module.href}
                    href={module.href}
                    className="cocola-admin-module-trigger group rounded-2xl no-underline outline-none focus-visible:ring-2 focus-visible:ring-focus"
                  >
                    <Card className="cocola-admin-module-card h-full min-h-52 p-5">
                    <Card.Content className="flex h-full min-w-0 flex-col items-start p-0">
                      <span className="flex w-full items-start justify-between gap-3">
                      <span className="cocola-admin-module-icon flex size-11 shrink-0 items-center justify-center rounded-2xl text-white" style={{background:`linear-gradient(135deg, ${module.from}, ${module.to})`}}>
                        <Icon className="size-6" strokeWidth={2} />
                      </span>
                      <ArrowRight className="text-muted cocola-admin-module-arrow size-4" />
                      </span>
                      <span className="mt-4 font-semibold">{module.title}</span>
                      <span className="text-muted mt-2 line-clamp-3 text-sm leading-6">{module.summary}</span>
                      <span className="text-accent mt-auto flex items-center gap-1 pt-5 text-sm font-medium">Open<ArrowRight className="cocola-admin-module-arrow size-4" /></span>
                    </Card.Content>
                    </Card>
                  </Link>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </AdminPageLayout>
  );
}
