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
    /createPageInstance\(createCodePage\(workspacePath, workspaceRoot\)\)/,
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
