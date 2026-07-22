import assert from "node:assert/strict";
import test from "node:test";

import { findLatestProgressItems, normalizeProgressItems } from "./progress-items.mjs";

test("normalizes Claude Code TodoWrite items", () => {
  assert.deepEqual(
    normalizeProgressItems([
      { content: "Inspect the project", status: "completed" },
      { content: "Implement the change", status: "in_progress" },
      { content: "Run tests", status: "pending" },
    ]),
    [
      { id: "0:Inspect the project", text: "Inspect the project", status: "completed" },
      { id: "1:Implement the change", text: "Implement the change", status: "in_progress" },
      { id: "2:Run tests", text: "Run tests", status: "pending" },
    ],
  );
});

test("normalizes Codex todo list items", () => {
  assert.deepEqual(
    normalizeProgressItems([
      { id: "one", text: "Read files", completed: true },
      { id: "two", text: "Edit code", status: "inProgress" },
      { id: "three", text: "Verify", completed: false },
    ]),
    [
      { id: "one", text: "Read files", status: "completed" },
      { id: "two", text: "Edit code", status: "in_progress" },
      { id: "three", text: "Verify", status: "pending" },
    ],
  );
});

test("filters malformed items and supports simple string lists", () => {
  assert.deepEqual(normalizeProgressItems([" First ", null, {}, 42, ""]), [
    { id: "0:First", text: "First", status: "pending" },
  ]);
  assert.deepEqual(normalizeProgressItems("not-a-list"), []);
});

test("finds the latest plan snapshot in live assistant-ui parts", () => {
  const first = [{ id: "one", text: "Inspect", status: "in_progress" }];
  const latest = [
    { id: "one", text: "Inspect", status: "completed" },
    { id: "two", text: "Implement", status: "in_progress" },
  ];
  assert.equal(
    findLatestProgressItems([
      { type: "data", name: "progress", data: { items: first } },
      { type: "tool-call", toolName: "Read" },
      { type: "data", name: "progress", data: { items: latest } },
      { type: "tool-call", toolName: "Edit" },
    ]),
    latest,
  );
});

test("finds persisted progress and ignores malformed snapshots", () => {
  const items = [{ text: "Verify", status: "pending" }];
  assert.equal(
    findLatestProgressItems([
      { type: "progress", items },
      { type: "data", name: "progress", data: { items: "invalid" } },
    ]),
    items,
  );
  assert.equal(findLatestProgressItems([{ type: "text", text: "Done" }]), undefined);
  assert.equal(findLatestProgressItems(undefined), undefined);
});
