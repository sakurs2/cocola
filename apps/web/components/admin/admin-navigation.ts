import type { CSSProperties } from "react";
import type { LucideIcon } from "lucide-react";
import {
  BarChart3,
  Clock3,
  Cpu,
  Database,
  FileText,
  HardDrive,
  Layers,
  Plug,
  Server,
  Settings,
  ShieldCheck,
  Sparkles,
  SquareTerminal,
  Users,
  Wrench,
} from "lucide-react";

export type AdminSectionId =
  | "overview"
  | "users"
  | "models"
  | "skills"
  | "mcps"
  | "toolbox"
  | "tasks"
  | "runs"
  | "usage"
  | "sandboxes"
  | "nodes"
  | "storage"
  | "architecture"
  | "logs"
  | "settings";

export type AdminThemeKey =
  | "amber"
  | "blue"
  | "cyan"
  | "fuchsia"
  | "green"
  | "indigo"
  | "orange"
  | "purple"
  | "rose"
  | "sky"
  | "slate"
  | "teal"
  | "violet";

export type AdminSection = {
  description: string;
  icon: LucideIcon;
  iconClassName: string;
  id: AdminSectionId;
  label: string;
  path: string;
  theme: AdminThemeKey;
};

type AdminTheme = {
  accent: string;
  accentForeground: string;
};

const ADMIN_THEMES: Record<AdminThemeKey, AdminTheme> = {
  amber: { accent: "oklch(76% 0.17 70)", accentForeground: "var(--eclipse)" },
  blue: { accent: "oklch(62.04% 0.195 253.83)", accentForeground: "var(--snow)" },
  cyan: { accent: "oklch(68% 0.14 215)", accentForeground: "var(--eclipse)" },
  fuchsia: { accent: "oklch(62% 0.22 330)", accentForeground: "var(--snow)" },
  green: { accent: "oklch(64% 0.17 150)", accentForeground: "var(--eclipse)" },
  indigo: { accent: "oklch(56% 0.22 275)", accentForeground: "var(--snow)" },
  orange: { accent: "oklch(67% 0.19 45)", accentForeground: "var(--snow)" },
  purple: { accent: "oklch(58% 0.22 305)", accentForeground: "var(--snow)" },
  rose: { accent: "oklch(61% 0.22 16)", accentForeground: "var(--snow)" },
  sky: { accent: "oklch(68% 0.15 235)", accentForeground: "var(--eclipse)" },
  slate: { accent: "oklch(53% 0.03 258)", accentForeground: "var(--snow)" },
  teal: { accent: "oklch(62% 0.14 180)", accentForeground: "var(--eclipse)" },
  violet: { accent: "oklch(60% 0.22 292)", accentForeground: "var(--snow)" },
};

export const ADMIN_SECTIONS: readonly AdminSection[] = [
  {
    id: "overview",
    path: "",
    label: "Overview",
    description: "Health, attention queue, and recent control-plane activity.",
    icon: ShieldCheck,
    iconClassName: "text-blue-600",
    theme: "blue",
  },
  {
    id: "users",
    path: "users",
    label: "Users",
    description: "Whitelist accounts, roles, teams, and account state.",
    icon: Users,
    iconClassName: "text-blue-600",
    theme: "blue",
  },
  {
    id: "models",
    path: "models",
    label: "Models",
    description: "Providers, routes, credentials, and default models.",
    icon: Cpu,
    iconClassName: "text-violet-600",
    theme: "violet",
  },
  {
    id: "skills",
    path: "skills",
    label: "Skills",
    description: "Shared capabilities installed for agents.",
    icon: Sparkles,
    iconClassName: "text-amber-500",
    theme: "amber",
  },
  {
    id: "mcps",
    path: "mcps",
    label: "MCP Servers",
    description: "Runtime tool servers and transport availability.",
    icon: Plug,
    iconClassName: "text-orange-600",
    theme: "orange",
  },
  {
    id: "toolbox",
    path: "toolbox",
    label: "Toolbox",
    description: "Small global controls, including the system prompt.",
    icon: Wrench,
    iconClassName: "text-cyan-600",
    theme: "cyan",
  },
  {
    id: "tasks",
    path: "scheduled-tasks",
    label: "Tasks",
    description: "Schedules, ownership, and recent task outcomes.",
    icon: Clock3,
    iconClassName: "text-green-600",
    theme: "green",
  },
  {
    id: "runs",
    path: "audit",
    label: "Agent Runs",
    description: "Conversation runs, traces, timing, and failures.",
    icon: FileText,
    iconClassName: "text-indigo-600",
    theme: "indigo",
  },
  {
    id: "usage",
    path: "token-usage",
    label: "Token Usage",
    description: "Token totals, trends, ranked users, and exports.",
    icon: BarChart3,
    iconClassName: "text-rose-600",
    theme: "rose",
  },
  {
    id: "sandboxes",
    path: "sandboxes",
    label: "Sandboxes",
    description: "Active sandboxes, owners, bindings, and lifecycle.",
    icon: Layers,
    iconClassName: "text-teal-600",
    theme: "teal",
  },
  {
    id: "nodes",
    path: "sandbox-nodes",
    label: "Nodes",
    description: "Node health, pod capacity, and placement controls.",
    icon: Server,
    iconClassName: "text-sky-600",
    theme: "sky",
  },
  {
    id: "storage",
    path: "storage",
    label: "Storage",
    description: "Disk headroom, session volumes, and orphan cleanup.",
    icon: HardDrive,
    iconClassName: "text-purple-600",
    theme: "purple",
  },
  {
    id: "architecture",
    path: "architecture",
    label: "Architecture",
    description: "Service topology, dependencies, and component health.",
    icon: Database,
    iconClassName: "text-fuchsia-600",
    theme: "fuchsia",
  },
  {
    id: "logs",
    path: "component-logs",
    label: "Service Logs",
    description: "Recent output from Cocola runtime services.",
    icon: SquareTerminal,
    iconClassName: "text-slate-600",
    theme: "slate",
  },
  {
    id: "settings",
    path: "settings",
    label: "Settings",
    description: "Configuration sources and hot-reloadable controls.",
    icon: Settings,
    iconClassName: "text-slate-500",
    theme: "slate",
  },
] as const;

export const ADMIN_GROUPS = [
  { label: "Configuration", sectionIds: ["users", "models", "skills", "mcps", "toolbox"] },
  { label: "Operations", sectionIds: ["tasks", "runs", "usage"] },
  {
    label: "Infrastructure",
    sectionIds: ["sandboxes", "nodes", "storage", "architecture", "logs"],
  },
] as const satisfies readonly {
  label: string;
  sectionIds: readonly AdminSectionId[];
}[];

export function getAdminSection(id: AdminSectionId): AdminSection {
  return ADMIN_SECTIONS.find((section) => section.id === id) ?? ADMIN_SECTIONS[0]!;
}

export function getAdminSectionForPathname(pathname: string): AdminSection {
  if (pathname === "/admin") return getAdminSection("overview");
  if (pathname.startsWith("/admin/traces")) return getAdminSection("runs");

  return (
    ADMIN_SECTIONS.filter((section) => section.path)
      .sort((left, right) => right.path.length - left.path.length)
      .find((section) => pathname.startsWith(`/admin/${section.path}`)) ??
    getAdminSection("overview")
  );
}

export function getAdminThemeStyle(themeKey: AdminThemeKey): CSSProperties {
  const theme = ADMIN_THEMES[themeKey];

  return {
    "--accent": theme.accent,
    "--accent-foreground": theme.accentForeground,
    "--accent-hover": "color-mix(in oklab, var(--accent) 90%, var(--accent-foreground) 10%)",
    "--accent-soft": "color-mix(in oklab, var(--accent) 15%, transparent)",
    "--accent-soft-foreground": "var(--accent)",
    "--focus": "var(--accent)",
  } as CSSProperties;
}
