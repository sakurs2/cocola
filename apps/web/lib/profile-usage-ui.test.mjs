import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const usagePanelSource = readFileSync(
  new URL("../components/profile/usage-panel.tsx", import.meta.url),
  "utf8",
);

test("profile usage keeps recent activity compact with client pagination", () => {
  assert.match(usagePanelSource, /const ACTIVITY_PAGE_SIZE = 5/);
  assert.match(usagePanelSource, /recentActivity\.slice\(/);
  assert.match(usagePanelSource, /aria-label=\{t\("pagination"\)\}/);
  assert.match(usagePanelSource, /t\("showing", \{ start: start \+ 1, end, total \}\)/);
});

test("recent activity uses aligned metric columns", () => {
  assert.match(
    usagePanelSource,
    /md:grid-cols-\[minmax\(13rem,1fr\)_6\.5rem_6\.5rem_6\.5rem_11rem\]/,
  );
  for (const key of ["prompt", "output", "total", "time"]) {
    assert.match(usagePanelSource, new RegExp(`role="columnheader">\\s*\\{t\\("${key}"\\)\\}`));
  }
});

test("lifetime totals use locale-aware compact notation while preserving exact values", () => {
  assert.match(usagePanelSource, /useFormatter\(\)/);
  assert.match(usagePanelSource, /format\.number\(value, \{ notation: "compact"/);
  assert.match(usagePanelSource, /notation: "compact"/);
  assert.match(usagePanelSource, /value=\{fmtCompact\(aggregate\?\.total_tokens\)\}/);
  assert.match(usagePanelSource, /title=\{exactValue\}/);
});
