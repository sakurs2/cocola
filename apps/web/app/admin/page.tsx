"use client";

import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  ArrowRight,
  Box,
  CalendarClock,
  Cpu,
  Server,
  Settings,
  ShieldCheck,
  Users,
} from "lucide-react";
import Link from "next/link";

const MODULES = [
  {
    title: "Sandbox Nodes",
    href: "/admin/sandbox-nodes",
    icon: Server,
    group: "Infrastructure",
    summary: "k3s node health, sandbox pod counts, capacity limits, and node operations.",
    stats: ["Nodes", "Sandbox Pods", "Max Sandbox Pods"],
  },
  {
    title: "Sandbox Runtime",
    href: "/admin/sandboxes",
    icon: Box,
    group: "Infrastructure",
    summary:
      "Current sandboxes, conversation bindings, owners, lifecycle state, and pod placement.",
    stats: ["Running", "Starting", "To Reclaim"],
  },
  {
    title: "Users",
    href: "/admin/users",
    icon: Users,
    group: "Access",
    summary: "Whitelist accounts, admin roles, enabled status, and password resets.",
    stats: ["Users", "Enabled", "Admins"],
  },
  {
    title: "Models",
    href: "/admin/models",
    icon: Cpu,
    group: "AI",
    summary: "Providers, model aliases, API keys, default model, visibility, and logos.",
    stats: ["Providers", "Routes", "Default"],
  },
  {
    title: "Scheduled Tasks",
    href: "/admin/scheduled-tasks",
    icon: CalendarClock,
    group: "AI",
    summary: "System task schedules, prompt inputs, model selection, attachments, and run history.",
    stats: ["Tasks", "Runs", "Errors"],
  },
  {
    title: "Settings",
    href: "/admin/settings",
    icon: Settings,
    group: "Settings",
    summary:
      "Runtime configuration defaults, database overrides, hot reload status, and secrets state.",
    stats: ["Defaults", "Overrides", "Restart Required"],
  },
];

export default function AdminPage() {
  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground">
            <ShieldCheck className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Admin Monitoring</h1>
            <p className="truncate text-xs text-muted-foreground">
              Current operations surfaces available in cocola admin
            </p>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-6">
        <section className="grid gap-3 md:grid-cols-3">
          <Metric label="Modules" value={String(MODULES.length)} />
          <Metric label="Access" value="1" />
          <Metric label="Infrastructure" value="2" />
        </section>

        <section className="grid gap-4 lg:grid-cols-2">
          {MODULES.map((module) => {
            const Icon = module.icon;
            return (
              <div key={module.href} className="rounded-lg border border-border bg-card p-4">
                <div className="flex items-start gap-3">
                  <div className="grid size-9 shrink-0 place-items-center rounded-md bg-muted">
                    <Icon className="size-4 text-muted-foreground" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="text-xs font-medium text-muted-foreground">{module.group}</div>
                    <h2 className="mt-1 text-sm font-semibold">{module.title}</h2>
                    <p className="mt-1 text-sm text-muted-foreground">{module.summary}</p>
                  </div>
                </div>
                <div className="mt-4 flex flex-wrap gap-2">
                  {module.stats.map((stat) => (
                    <span
                      key={stat}
                      className="rounded-md border border-border bg-background px-2 py-1 text-xs text-muted-foreground"
                    >
                      {stat}
                    </span>
                  ))}
                </div>
                <div className="mt-4 flex justify-end">
                  <Link
                    href={module.href}
                    className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
                  >
                    Open
                    <ArrowRight className="ml-2 size-4" />
                  </Link>
                </div>
              </div>
            );
          })}
        </section>
      </div>
    </main>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
    </div>
  );
}
