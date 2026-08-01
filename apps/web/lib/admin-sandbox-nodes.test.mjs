import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const sandboxNodesPageSource = await readFile(
  new URL("../app/admin/sandbox-nodes/page.tsx", import.meta.url),
  "utf8",
);

test("add node dialog presents the unfinished feature as coming soon", () => {
  assert.match(sandboxNodesPageSource, /Node onboarding is coming soon/);
  assert.match(sandboxNodesPageSource, /This\s*feature is not available yet/);
  assert.doesNotMatch(sandboxNodesPageSource, /sandbox-nodes\/join-command/);
  assert.doesNotMatch(sandboxNodesPageSource, /Copy command/);
});
