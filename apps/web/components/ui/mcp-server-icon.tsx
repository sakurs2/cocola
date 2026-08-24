import {
  Blocks,
  Boxes,
  Cloud,
  Cpu,
  Network,
  Plug2,
  Radio,
  Search,
  Server,
  Terminal,
  Waypoints,
  type LucideIcon,
} from "lucide-react";
import type { CSSProperties } from "react";
import { cn } from "@/lib/utils";

const GLYPH_ICONS: LucideIcon[] = [
  Server,
  Network,
  Cloud,
  Cpu,
  Boxes,
  Blocks,
  Radio,
  Waypoints,
  Plug2,
  Terminal,
];

type GlyphTone = { ink: string; soft: string; ring: string };

const DEFAULT_GLYPH_TONE: GlyphTone = {
  ink: "#ea580c",
  soft: "#ffedd5",
  ring: "#fdba74",
};

const GLYPH_TONES: GlyphTone[] = [
  DEFAULT_GLYPH_TONE,
  { ink: "#d97706", soft: "#fef3c7", ring: "#fcd34d" },
  { ink: "#0d9488", soft: "#ccfbf1", ring: "#5eead4" },
  { ink: "#2563eb", soft: "#dbeafe", ring: "#93c5fd" },
  { ink: "#7c3aed", soft: "#ede9fe", ring: "#c4b5fd" },
  { ink: "#db2777", soft: "#fce7f3", ring: "#f9a8d4" },
  { ink: "#0891b2", soft: "#cffafe", ring: "#67e8f9" },
  { ink: "#16a34a", soft: "#dcfce7", ring: "#86efac" },
  { ink: "#c026d3", soft: "#fae8ff", ring: "#f0abfc" },
  { ink: "#4f46e5", soft: "#e0e7ff", ring: "#a5b4fc" },
];

const TAVILY_GLYPH = {
  Icon: Search,
  tone: { ink: "#3370FF", soft: "#EEF3FF", ring: "#A9C1FF" },
} as const;

export function MCPServerIcon({
  id,
  className,
  iconClassName,
}: {
  id: string;
  className?: string;
  iconClassName?: string;
}) {
  const { Icon, tone } = glyphFor(id);
  const style = {
    "--glyph-ink": tone.ink,
    "--glyph-soft": tone.soft,
    "--glyph-ring": tone.ring,
    background: "var(--glyph-soft)",
    color: "var(--glyph-ink)",
  } as CSSProperties;

  return (
    <span
      aria-hidden="true"
      className={cn("flex size-10 shrink-0 items-center justify-center rounded-2xl", className)}
      style={style}
    >
      <Icon className={cn("size-[18px]", iconClassName)} />
    </span>
  );
}

function glyphFor(id: string): { Icon: LucideIcon; tone: GlyphTone } {
  if (id.trim().toLowerCase() === "tavily") return TAVILY_GLYPH;

  const hash = hashString(id || "mcp");
  return {
    Icon: GLYPH_ICONS[hash % GLYPH_ICONS.length] ?? Server,
    tone: GLYPH_TONES[hash % GLYPH_TONES.length] ?? DEFAULT_GLYPH_TONE,
  };
}

function hashString(value: string): number {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) | 0;
  }
  return Math.abs(hash);
}
