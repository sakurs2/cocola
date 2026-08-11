import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const sandboxNodesPageSource = await readFile(
  new URL("../app/admin/sandbox-nodes/page.tsx", import.meta.url),
  "utf8",
);

test("nodes page omits the unfinished add-node affordance", () => {
  assert.match(sandboxNodesPageSource, /<AdminRefreshButton/);
  assert.doesNotMatch(sandboxNodesPageSource, /Add node/);
  assert.doesNotMatch(sandboxNodesPageSource, /Node onboarding is coming soon/);
  assert.doesNotMatch(sandboxNodesPageSource, /sandbox-nodes\/join-command/);
  assert.doesNotMatch(sandboxNodesPageSource, /Copy command/);
});
