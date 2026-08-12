import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { dockPageInstanceID, dockPageInstanceLabel } from "./workspace-dock-tabs.mjs";

const workspaceDockSource = await readFile(
  new URL("../components/assistant-ui/workspace-panel.tsx", import.meta.url),
  "utf8",
);

test("dock page instances keep repeated panel types independent", () => {
  assert.equal(dockPageInstanceID("shell", 1), "dock:shell:1");
  assert.equal(dockPageInstanceID("shell", 2), "dock:shell:2");
  assert.notEqual(dockPageInstanceID("shell", 2), dockPageInstanceID("preview", 2));
  assert.equal(dockPageInstanceLabel("Shell", 1), "Shell");
  assert.equal(dockPageInstanceLabel("Shell", 2), "Shell 2");
});

test("workspace launcher, add menu, and code actions create fresh instances", () => {
  assert.match(workspaceDockSource, /const addablePages = basePages/);
  assert.match(workspaceDockSource, /const instance = createPageInstance\(page\)/);
  assert.match(
    workspaceDockSource,
    /createPageInstance\(\s*createCodePage\(workspacePath, workspaceRoot, \{/,
  );
  assert.doesNotMatch(
    workspaceDockSource,
    /current\.some\(\(candidate\) => candidate\.id === page\.id\)/,
  );
});

test("dock page instance helpers reject invalid identity and ordinals", () => {
  assert.throws(() => dockPageInstanceID("", 1), /template id is required/);
  assert.throws(() => dockPageInstanceID("shell", 0), /positive integer/);
  assert.throws(() => dockPageInstanceLabel("Shell", 1.5), /positive integer/);
});

test("Git actions use a compact aligned hierarchy and centered confirmation", () => {
  assert.match(workspaceDockSource, /const GIT_ACTION_BUTTON_CLASS/);
  assert.match(workspaceDockSource, /variant="outline"[\s\S]*t\("actions\.refresh"\)/);
  assert.match(workspaceDockSource, /<ActionConfirmDialog[\s\S]*title=\{t\("refreshTitle"\)\}/);
  assert.match(workspaceDockSource, /showHint=\{false\}/);
  assert.doesNotMatch(workspaceDockSource, /placement="right"[\s\S]*Refresh Git status/);
});

test("Git commit history keeps metadata in a compact two-line row", () => {
  assert.match(workspaceDockSource, /grid-cols-\[12px_18px_minmax\(0,1fr\)_14px\]/);
  assert.match(workspaceDockSource, /commit\.author_name \|\| t\("unknownAuthor"\)/);
  assert.match(
    workspaceDockSource,
    /formatGitRelativeTime\(commit\.authored_at, Date\.now\(\), locale, t\("unknownTime"\)\)/,
  );
  assert.match(workspaceDockSource, /commit\.sha\.slice\(0, 7\)/);
});
