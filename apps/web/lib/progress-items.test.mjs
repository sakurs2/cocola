import assert from "node:assert/strict";
import test from "node:test";

import { normalizeProgressItems } from "./progress-items.mjs";

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
