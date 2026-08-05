export const SKILL_GLYPH_KEYS = [
  "app-window",
  "badge-check",
  "bar-chart",
  "book-open",
  "boxes",
  "brain",
  "calendar",
  "clock",
  "code",
  "contact",
  "database",
  "file-text",
  "flask",
  "globe",
  "hard-drive",
  "list-checks",
  "mail",
  "message",
  "palette",
  "plug",
  "search",
  "server-cog",
  "shield-check",
  "sparkles",
  "table",
  "video",
  "wand",
  "wrench",
] as const;

export type SkillGlyphKey = (typeof SKILL_GLYPH_KEYS)[number];

const KEYWORD_GLYPHS: Array<[RegExp, SkillGlyphKey]> = [
  [/approval|review/i, "badge-check"],
  [/attendance|clock|timecard/i, "clock"],
  [/calendar|schedule/i, "calendar"],
  [/contact|people|directory|user/i, "contact"],
  [/(?:^|[-_])(im|chat|message)(?:$|[-_])/i, "message"],
  [/(?:^|[-_])(vc|video|meeting)(?:$|[-_])/i, "video"],
  [/sheet|excel|spreadsheet/i, "table"],
  [/base|database|sql|data|query|aeolus|table/i, "database"],
  [/drive|storage|filesystem|file-system/i, "hard-drive"],
  [/wiki|knowledge|notebook|note|memo/i, "book-open"],
  [/doc|report|write|text/i, "file-text"],
  [/app|workflow|automation/i, "app-window"],
  [/task|todo|checklist/i, "list-checks"],
  [/mail|email/i, "mail"],
  [/design|draw|whiteboard|image|creative/i, "palette"],
  [/test|qa|debug|probe/i, "flask"],
  [/deploy|infra|cloud|server|environment|env/i, "server-cog"],
  [/auth|security|permission|shared/i, "shield-check"],
  [/chart|graph|plot|viz|visuali/i, "bar-chart"],
  [/search|find|lookup/i, "search"],
  [/code|dev|git|build/i, "code"],
  [/web|http|browser|url/i, "globe"],
  [/mcp|plugin|connect/i, "plug"],
  [/agent|model|brain|(?:^|[-_])ai(?:$|[-_])/i, "brain"],
  [/tool|repair|ops/i, "wrench"],
];

export function skillIdentityHash(input: string): number {
  let hash = 0;
  for (let index = 0; index < input.length; index += 1) {
    hash = (hash << 5) - hash + input.charCodeAt(index);
    hash |= 0;
  }
  return Math.abs(hash);
}

export function resolveSkillGlyphKey(name: string): SkillGlyphKey {
  for (const [pattern, glyph] of KEYWORD_GLYPHS) {
    if (pattern.test(name)) return glyph;
  }
  return (
    SKILL_GLYPH_KEYS[skillIdentityHash(name || "skill") % SKILL_GLYPH_KEYS.length] ?? "sparkles"
  );
}
