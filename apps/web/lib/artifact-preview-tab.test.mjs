import assert from "node:assert/strict";
import test from "node:test";

import { artifactPreviewTabID } from "./artifact-preview-tab.mjs";

test("artifact preview tab IDs are stable within a conversation", () => {
  assert.equal(
    artifactPreviewTabID("session-1", "report.pdf"),
    artifactPreviewTabID("session-1", "report.pdf"),
  );
});

test("artifact preview tab IDs do not collide across conversations or artifacts", () => {
  assert.notEqual(
    artifactPreviewTabID("session-1", "report.pdf"),
    artifactPreviewTabID("session-2", "report.pdf"),
  );
  assert.notEqual(
    artifactPreviewTabID("session-1", "report.pdf"),
    artifactPreviewTabID("session-1", "summary.pdf"),
  );
});

test("artifact preview tab IDs encode separators and Unicode", () => {
  assert.equal(
    artifactPreviewTabID("session/1", "报告 / final.pdf"),
    "artifact:session%2F1:%E6%8A%A5%E5%91%8A%20%2F%20final.pdf",
  );
});

test("artifact preview tab IDs reject incomplete identities", () => {
  assert.throws(() => artifactPreviewTabID("", "report.pdf"), /require a session/);
  assert.throws(() => artifactPreviewTabID("session-1", ""), /require a session/);
});
