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
  slate: "bg-white text-slate-600 ring-slate-200",
  blue: "bg-white text-blue-600 ring-blue-200",
  cyan: "bg-white text-cyan-600 ring-cyan-200",
  emerald: "bg-white text-emerald-600 ring-emerald-200",
  amber: "bg-white text-amber-600 ring-amber-200",
  orange: "bg-white text-orange-600 ring-orange-200",
  rose: "bg-white text-rose-600 ring-rose-200",
  violet: "bg-white text-violet-600 ring-violet-200",
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
