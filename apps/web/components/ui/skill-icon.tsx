import * as React from "react";
import {
  AppWindow,
  BadgeCheck,
  BarChart3,
  BookOpen,
  Boxes,
  Brain,
  CalendarDays,
  Clock3,
  Code2,
  ContactRound,
  Database,
  FileText,
  FlaskConical,
  Globe,
  HardDrive,
  ListChecks,
  Mail,
  MessageSquare,
  type LucideIcon,
  Palette,
  Plug,
  Search,
  ServerCog,
  ShieldCheck,
  Sparkles,
  Table2,
  Video,
  Wand2,
  Wrench,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  resolveSkillGlyphKey,
  skillIdentityHash,
  type SkillGlyphKey,
} from "@/lib/skill-icon-identity";

// Shared skill icon: renders a stable, colored icon tile derived from the skill
// name. Same name -> same color + glyph across the whole app (skills page, the
// "/" skill menu in the composer, etc.). Skinned for the cocola user UI
// (light tint + matching ring, large radius).

// Curated tint palette (bg + text + ring) — soft, on-brand, high-legibility.
const PALETTE = [
  { bg: "bg-violet-50", text: "text-violet-600", ring: "ring-violet-100" },
  { bg: "bg-blue-50", text: "text-blue-600", ring: "ring-blue-100" },
  { bg: "bg-sky-50", text: "text-sky-600", ring: "ring-sky-100" },
  { bg: "bg-emerald-50", text: "text-emerald-600", ring: "ring-emerald-100" },
  { bg: "bg-amber-50", text: "text-amber-600", ring: "ring-amber-100" },
  { bg: "bg-rose-50", text: "text-rose-600", ring: "ring-rose-100" },
  { bg: "bg-cyan-50", text: "text-cyan-600", ring: "ring-cyan-100" },
  { bg: "bg-indigo-50", text: "text-indigo-600", ring: "ring-indigo-100" },
  { bg: "bg-teal-50", text: "text-teal-600", ring: "ring-teal-100" },
  { bg: "bg-orange-50", text: "text-orange-600", ring: "ring-orange-100" },
] as const;

const GLYPHS: Record<SkillGlyphKey, LucideIcon> = {
  "app-window": AppWindow,
  "badge-check": BadgeCheck,
  "bar-chart": BarChart3,
  "book-open": BookOpen,
  boxes: Boxes,
  brain: Brain,
  calendar: CalendarDays,
  clock: Clock3,
  code: Code2,
  contact: ContactRound,
  database: Database,
  "file-text": FileText,
  flask: FlaskConical,
  globe: Globe,
  "hard-drive": HardDrive,
  "list-checks": ListChecks,
  mail: Mail,
  message: MessageSquare,
  palette: Palette,
  plug: Plug,
  search: Search,
  "server-cog": ServerCog,
  "shield-check": ShieldCheck,
  sparkles: Sparkles,
  table: Table2,
  video: Video,
  wand: Wand2,
  wrench: Wrench,
};

const SIZES = {
  sm: { box: "size-8 rounded-lg", icon: "size-4" },
  md: { box: "size-10 rounded-xl", icon: "size-5" },
} as const;

export interface SkillIconProps {
  name: string;
  size?: keyof typeof SIZES;
  className?: string;
}

export function SkillIcon({ name, size = "md", className }: SkillIconProps) {
  const hash = skillIdentityHash(name || "skill");
  const palette = PALETTE[hash % PALETTE.length] ?? PALETTE[0];
  const Glyph = GLYPHS[resolveSkillGlyphKey(name || "")];
  const dims = SIZES[size];

  return (
    <div
      className={cn(
        "grid shrink-0 place-items-center ring-1",
        dims.box,
        palette.bg,
        palette.text,
        palette.ring,
        className,
      )}
    >
      <Glyph className={dims.icon} />
    </div>
  );
}
