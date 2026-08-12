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
  icon: LucideIcon;
  iconClassName: string;
  id: AdminSectionId;
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
    icon: ShieldCheck,
    iconClassName: "text-blue-600",
    theme: "blue",
  },
  {
    id: "users",
    path: "users",
    icon: Users,
    iconClassName: "text-blue-600",
    theme: "blue",
  },
  {
    id: "models",
    path: "models",
    icon: Cpu,
    iconClassName: "text-violet-600",
    theme: "violet",
  },
  {
    id: "skills",
    path: "skills",
    icon: Sparkles,
    iconClassName: "text-amber-500",
    theme: "amber",
  },
  {
    id: "mcps",
    path: "mcps",
    icon: Plug,
    iconClassName: "text-orange-600",
    theme: "orange",
  },
  {
    id: "toolbox",
    path: "toolbox",
    icon: Wrench,
    iconClassName: "text-cyan-600",
    theme: "cyan",
  },
  {
    id: "tasks",
    path: "scheduled-tasks",
    icon: Clock3,
    iconClassName: "text-green-600",
    theme: "green",
  },
  {
    id: "runs",
    path: "audit",
    icon: FileText,
    iconClassName: "text-indigo-600",
    theme: "indigo",
  },
  {
    id: "usage",
    path: "token-usage",
    icon: BarChart3,
    iconClassName: "text-rose-600",
    theme: "rose",
  },
  {
    id: "sandboxes",
    path: "sandboxes",
    icon: Layers,
    iconClassName: "text-teal-600",
    theme: "teal",
  },
  {
    id: "nodes",
    path: "sandbox-nodes",
    icon: Server,
    iconClassName: "text-sky-600",
    theme: "sky",
  },
  {
    id: "storage",
    path: "storage",
    icon: HardDrive,
    iconClassName: "text-purple-600",
    theme: "purple",
  },
  {
    id: "architecture",
    path: "architecture",
    icon: Database,
    iconClassName: "text-fuchsia-600",
    theme: "fuchsia",
  },
  {
    id: "logs",
    path: "component-logs",
    icon: SquareTerminal,
    iconClassName: "text-slate-600",
    theme: "slate",
  },
  {
    id: "settings",
    path: "settings",
    icon: Settings,
    iconClassName: "text-slate-500",
    theme: "slate",
  },
] as const;

export const ADMIN_GROUPS = [
  {
    id: "configuration",
    sectionIds: ["users", "models", "skills", "mcps", "toolbox"],
  },
  { id: "operations", sectionIds: ["tasks", "runs", "usage"] },
  {
    id: "infrastructure",
    sectionIds: ["sandboxes", "nodes", "storage", "architecture", "logs"],
  },
] as const satisfies readonly {
  id: "configuration" | "operations" | "infrastructure";
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
