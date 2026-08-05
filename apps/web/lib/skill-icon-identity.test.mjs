import assert from "node:assert/strict";
import test from "node:test";

import { resolveSkillGlyphKey } from "./skill-icon-identity.ts";

test("Lark skill families resolve to distinct semantic glyphs", () => {
  const skills = [
    "lark-approval",
    "lark-apps",
    "lark-attendance",
    "lark-base",
    "lark-calendar",
    "lark-contact",
    "lark-doc",
    "lark-drive",
    "lark-im",
    "lark-sheets",
    "lark-vc",
  ];

  const glyphs = skills.map(resolveSkillGlyphKey);
  assert.equal(new Set(glyphs).size, skills.length);
  assert.deepEqual(glyphs, [
    "badge-check",
    "app-window",
    "clock",
    "database",
    "calendar",
    "contact",
    "file-text",
    "hard-drive",
    "message",
    "table",
    "video",
  ]);
});

test("unknown skill names keep a stable non-document fallback", () => {
  const first = resolveSkillGlyphKey("custom-capability-alpha");
  const second = resolveSkillGlyphKey("custom-capability-alpha");

  assert.equal(first, second);
});
