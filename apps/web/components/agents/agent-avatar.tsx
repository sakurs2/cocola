import {
  BarChart3,
  Bot,
  BriefcaseBusiness,
  Code2,
  FileText,
  Headphones,
  Search,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

const ICONS: Record<string, LucideIcon> = {
  sparkle: Sparkles,
  robot: Bot,
  code: Code2,
  chart: BarChart3,
  document: FileText,
  search: Search,
  briefcase: BriefcaseBusiness,
  support: Headphones,
};

const COLORS: Record<string, string> = {
  slate: "bg-slate-500/15 text-slate-600 dark:text-slate-300",
  blue: "bg-blue-500/15 text-blue-600 dark:text-blue-300",
  cyan: "bg-cyan-500/15 text-cyan-600 dark:text-cyan-300",
  emerald: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-300",
  amber: "bg-amber-500/15 text-amber-600 dark:text-amber-300",
  orange: "bg-orange-500/15 text-orange-600 dark:text-orange-300",
  rose: "bg-rose-500/15 text-rose-600 dark:text-rose-300",
  violet: "bg-violet-500/15 text-violet-600 dark:text-violet-300",
};

export function AgentAvatar({
  avatarKey,
  avatarColor,
  className,
  iconClassName,
}: {
  avatarKey?: string;
  avatarColor?: string;
  className?: string;
  iconClassName?: string;
}) {
  const Icon = ICONS[avatarKey ?? ""] ?? Sparkles;
  return (
    <span
      className={cn(
        "inline-grid size-9 shrink-0 place-items-center rounded-xl",
        COLORS[avatarColor ?? ""] ?? COLORS.blue,
        className,
      )}
    >
      <Icon className={cn("size-4", iconClassName)} />
    </span>
  );
}
