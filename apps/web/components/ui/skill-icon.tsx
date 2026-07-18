import * as React from "react";
import {
  BarChart3,
  Boxes,
  Brain,
  Code2,
  Database,
  FileText,
  Globe,
  LineChart,
  type LucideIcon,
  Notebook,
  Plug,
  Search,
  Sparkles,
  Wand2,
  Wrench,
} from "lucide-react";
import { cn } from "@/lib/utils";

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

const GLYPHS: LucideIcon[] = [
  Sparkles,
  BarChart3,
  LineChart,
  FileText,
  Database,
  Globe,
  Search,
  Notebook,
  Code2,
  Wrench,
  Wand2,
  Brain,
  Boxes,
  Plug,
];

// Keyword hints let common skills get a semantically fitting glyph while still
// falling back to the stable hash for everything else.
const KEYWORD_GLYPHS: Array<[RegExp, LucideIcon]> = [
  [/chart|graph|plot|viz|visuali/i, BarChart3],
  [/query|aeolus|dashboard|data|sql|table/i, Database],
  [/doc|report|write|feishu|lark|text/i, FileText],
  [/search|find|lookup/i, Search],
  [/note|memo/i, Notebook],
  [/code|dev|git|build/i, Code2],
  [/web|http|browser|url/i, Globe],
  [/mcp|plugin|connect/i, Plug],
];

function hashString(input: string): number {
  let hash = 0;
  for (let i = 0; i < input.length; i += 1) {
    hash = (hash << 5) - hash + input.charCodeAt(i);
    hash |= 0; // force 32-bit
  }
  return Math.abs(hash);
}

function pickGlyph(name: string, hash: number): LucideIcon {
  for (const [pattern, glyph] of KEYWORD_GLYPHS) {
    if (pattern.test(name)) return glyph;
  }
  return GLYPHS[hash % GLYPHS.length] ?? Sparkles;
}

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
  const hash = hashString(name || "skill");
  const palette = PALETTE[hash % PALETTE.length] ?? PALETTE[0];
  const Glyph = pickGlyph(name || "", hash);
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
