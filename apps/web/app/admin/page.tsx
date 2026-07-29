"use client";

import {
  BarChart3 as ChartLineUp,
  Timer as ClockCountdown,
  Cpu,
  FileText,
  Workflow as Graph,
  HardDrive as HardDrives,
  Plug as PlugsConnected,
  ShieldCheck,
  Sparkles as Sparkle,
  Layers as Stack,
  SquareTerminal as TerminalWindow,
  Wrench as ToolboxIcon,
  Users as UsersThree,
  type LucideIcon as PhosphorIcon,
} from "lucide-react";
import { motion } from "framer-motion";
import { ArrowRight } from "lucide-react";
import Link from "next/link";
import type { CSSProperties } from "react";
import {
  AdminPage as AdminPageLayout,
  AdminPageHeader,
} from "@/components/admin/admin-ui";

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
      <section className="admin-overview-hero overflow-hidden rounded-3xl border px-5 py-6 sm:px-7 sm:py-7">
        <AdminPageHeader
          title="Operate cocola with context"
          icon={<ShieldCheck className="size-5" />}
        />
      </section>

      <div className="grid gap-4 xl:grid-cols-2">
        {MODULE_GROUPS.map((group, groupIndex) => (
          <motion.section
            key={group.label}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.2, delay: Math.min(groupIndex * 0.035, 0.14) }}
            className="admin-domain-panel rounded-3xl border p-3 sm:p-4"
          >
            <div className="mb-3 px-1">
              <h2 className="text-sm font-semibold text-foreground">{group.label}</h2>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {group.modules.map((module) => {
                const Icon = module.icon;
                const cardStyle = {
                  "--card-from": module.from,
                  "--card-to": module.to,
                } as CSSProperties;
                return (
                  <Link
                    key={module.href}
                    href={module.href}
                    className="admin-module-card group"
                    style={cardStyle}
                  >
                    <span className="admin-module-head">
                      <span className="admin-module-icon">
                        <Icon className="size-[18px]" />
                      </span>
                      <span className="admin-module-title">{module.title}</span>
                    </span>
                    <span className="admin-module-summary">{module.summary}</span>
                    <span className="admin-module-cta">
                      Open
                      <ArrowRight className="size-3.5 transition-transform duration-200 group-hover:translate-x-0.5" />
                    </span>
                  </Link>
                );
              })}
            </div>
          </motion.section>
        ))}
      </div>
    </AdminPageLayout>
  );
}
