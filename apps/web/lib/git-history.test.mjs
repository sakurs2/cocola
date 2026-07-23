import assert from "node:assert/strict";
import test from "node:test";

import {
  formatGitRelativeTime,
  gitChangeCode,
  gitCommitBadges,
  gitCommitDescription,
  gitDiffGutterWidth,
} from "./git-history.mjs";

test("formats recent and historical commit times", () => {
  const now = Date.parse("2026-07-22T12:00:00Z");
  assert.equal(formatGitRelativeTime("2026-07-22T11:59:15Z", now), "45s ago");
  assert.equal(formatGitRelativeTime("2026-07-22T10:00:00Z", now), "2h ago");
  assert.equal(formatGitRelativeTime("not-a-date", now), "Unknown time");
});

test("deduplicates head, base, branch and decorated refs", () => {
  const badges = gitCommitBadges(
    {
      sha: "a".repeat(40),
      refs: ["HEAD -> refs/heads/main", "refs/heads/main", "refs/tags/v1.0.0"],
    },
    { head_sha: "a".repeat(40), base_sha: "a".repeat(40), branch: "main" },
  );
  assert.deepEqual(badges, [
    { label: "HEAD", tone: "head" },
    { label: "BASE", tone: "base" },
    { label: "v1.0.0", tone: "tag" },
  ]);
});

test("normalizes porcelain and name-status change codes", () => {
  assert.equal(gitChangeCode(".M"), "M");
  assert.equal(gitChangeCode("R100"), "R");
  assert.equal(gitChangeCode("?"), "A");
});

test("sizes diff gutters from the largest visible line number", () => {
  assert.equal(gitDiffGutterWidth(0), "4.5ch");
  assert.equal(gitDiffGutterWidth(999), "4.5ch");
  assert.equal(gitDiffGutterWidth(1_000), "5.5ch");
  assert.equal(gitDiffGutterWidth(100_000), "7.5ch");
  assert.equal(gitDiffGutterWidth(Number.NaN), "4.5ch");
});

test("removes a repeated subject from the commit body", () => {
  assert.equal(
    gitCommitDescription({ subject: "Add feature", body: "Add feature\n\nMore details." }),
    "More details.",
  );
  assert.equal(gitCommitDescription({ subject: "Add feature", body: "Add feature" }), "");
});
