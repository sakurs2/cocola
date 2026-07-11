"use client";

import {
  ChartLineUp,
  ClockCountdown,
  Cpu,
  FileText,
  Graph,
  PlugsConnected,
  ShieldCheck,
  Sparkle,
  Stack,
  TerminalWindow,
  ToolboxIcon,
  UsersThree,
  type Icon as PhosphorIcon,
} from "@phosphor-icons/react";
import { motion } from "framer-motion";
import { ArrowRight } from "lucide-react";
import Link from "next/link";
import {
  AdminPage as AdminPageLayout,
  AdminPageHeader,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";

type AdminModule = {
  title: string;
  href: string;
  icon: PhosphorIcon;
  summary: string;
  accent: string;
};

const MODULE_GROUPS: { label: string; description: string; modules: AdminModule[] }[] = [
  {
    label: "Configuration",
    description: "Manage access and the capabilities available to every agent.",
    modules: [
      {
        title: "Users",
        href: "/admin/users",
        icon: UsersThree,
        summary: "Manage whitelist accounts, roles, teams, and account status.",
        accent: "text-cyan-700 bg-cyan-500/10",
      },
      {
        title: "Models",
        href: "/admin/models",
        icon: Cpu,
        summary: "Configure providers, aliases, credentials, and the default route.",
        accent: "text-indigo-600 bg-indigo-500/10",
      },
      {
        title: "Skills",
        href: "/admin/skills",
        icon: Sparkle,
        summary: "Review installed skills and the capabilities exposed to agents.",
        accent: "text-fuchsia-600 bg-fuchsia-500/10",
      },
      {
        title: "MCP Servers",
        href: "/admin/mcps",
        icon: PlugsConnected,
        summary: "Manage MCP servers, transport settings, and availability.",
        accent: "text-emerald-700 bg-emerald-500/10",
      },
      {
        title: "Toolbox",
        href: "/admin/toolbox",
        icon: ToolboxIcon,
        summary: "Open lightweight admin controls such as the global system prompt.",
        accent: "text-sky-700 bg-sky-500/10",
      },
    ],
  },
  {
    label: "Operations",
    description: "Review scheduled work, agent execution, and model consumption.",
    modules: [
      {
        title: "Tasks",
        href: "/admin/scheduled-tasks",
        icon: ClockCountdown,
        summary: "Review user-owned schedules, task status, and recent results.",
        accent: "text-amber-700 bg-amber-500/10",
      },
      {
        title: "Agent Runs",
        href: "/admin/audit",
        icon: FileText,
        summary: "Inspect conversation runs, traces, timing, and failures.",
        accent: "text-rose-700 bg-rose-500/10",
      },
      {
        title: "Token Usage",
        href: "/admin/token-usage",
        icon: ChartLineUp,
        summary: "Review token totals, usage trends, ranked users, and exports.",
        accent: "text-blue-600 bg-blue-500/10",
      },
    ],
  },
  {
    label: "Infrastructure",
    description: "Operate the isolated compute capacity behind every conversation.",
    modules: [
      {
        title: "Sandboxes",
        href: "/admin/sandboxes",
        icon: Stack,
        summary: "Inspect active sandboxes, owners, bindings, and lifecycle state.",
        accent: "text-orange-700 bg-orange-500/10",
      },
      {
        title: "Nodes",
        href: "/admin/sandbox-nodes",
        icon: Cpu,
        summary: "Track node health, pod capacity, placement, and node operations.",
        accent: "text-teal-700 bg-teal-500/10",
      },
      {
        title: "Architecture",
        href: "/admin/architecture",
        icon: Graph,
        summary: "Inspect the system DAG, dependencies, and component health.",
        accent: "text-violet-600 bg-violet-500/10",
      },
      {
        title: "Service Logs",
        href: "/admin/component-logs",
        icon: TerminalWindow,
        summary: "Read recent output from Cocola's core runtime services.",
        accent: "text-slate-700 bg-slate-500/10",
      },
    ],
  },
];

export default function AdminPage() {
  return (
    <AdminPageLayout>
      <section className="admin-overview-hero overflow-hidden rounded-3xl border px-5 py-6 sm:px-7 sm:py-7">
        <AdminPageHeader
          eyebrow="Sky Glass Control Plane"
          title="Operate cocola with context"
          description="Configure access and agent capabilities, observe usage, and inspect the infrastructure that keeps every conversation isolated."
          icon={<ShieldCheck className="size-5" weight="duotone" />}
          actions={
            <AdminStatusBadge tone="green" dot>
              Self-hosted
            </AdminStatusBadge>
          }
        />
        <div className="mt-6 flex flex-wrap gap-2 border-t border-white/55 pt-4 text-xs text-muted-foreground">
          {MODULE_GROUPS.map((group) => (
            <span key={group.label} className="admin-context-pill">
              {group.label}
              <span className="font-mono text-[10px] text-primary/70">{group.modules.length}</span>
            </span>
          ))}
        </div>
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
              <div className="flex items-center gap-2">
                <h2 className="text-sm font-semibold text-foreground">{group.label}</h2>
                <span className="font-mono text-[10px] text-muted-foreground">
                  {group.modules.length}
                </span>
              </div>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">{group.description}</p>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              {group.modules.map((module) => {
                const Icon = module.icon;
                return (
                  <Link key={module.href} href={module.href} className="admin-module-card group">
                    <span className={`admin-module-icon ${module.accent}`}>
                      <Icon className="size-[18px]" weight="duotone" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-sm font-semibold text-foreground">
                        {module.title}
                      </span>
                      <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                        {module.summary}
                      </span>
                    </span>
                    <ArrowRight className="mt-1 size-4 shrink-0 -translate-x-1 text-muted-foreground opacity-0 transition-all duration-200 group-hover:translate-x-0 group-hover:text-primary group-hover:opacity-100" />
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
