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
  slate: "bg-slate-100 text-slate-700 ring-slate-200",
  blue: "bg-blue-100 text-blue-700 ring-blue-200",
  cyan: "bg-cyan-100 text-cyan-700 ring-cyan-200",
  emerald: "bg-emerald-100 text-emerald-700 ring-emerald-200",
  amber: "bg-amber-100 text-amber-700 ring-amber-200",
  orange: "bg-orange-100 text-orange-700 ring-orange-200",
  rose: "bg-rose-100 text-rose-700 ring-rose-200",
  violet: "bg-violet-100 text-violet-700 ring-violet-200",
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
        "inline-grid size-9 shrink-0 place-items-center rounded-xl ring-1 ring-inset",
        COLORS[avatarColor ?? ""] ?? COLORS.blue,
        className,
      )}
    >
      <Icon className={cn("size-4", iconClassName)} />
    </span>
  );
}
