import assert from "node:assert/strict";
import test from "node:test";

import { canDiscardPendingProjectTask, nextProjectCreateIntent } from "./project-task-intent.mjs";

test("reuses the request id while retrying the same project payload", () => {
  let sequence = 0;
  const createRequestID = () => `request-${++sequence}`;
  const payload = { mode: "create", name: "demo", repository_name: "demo" };

  const first = nextProjectCreateIntent(null, payload, createRequestID);
  const retry = nextProjectCreateIntent(first, { ...payload }, createRequestID);

  assert.equal(first.requestID, "request-1");
  assert.equal(retry, first);
  assert.equal(sequence, 1);
});

test("rotates the request id when the project payload changes", () => {
  let sequence = 0;
  const createRequestID = () => `request-${++sequence}`;
  const first = nextProjectCreateIntent(null, { mode: "create", name: "demo" }, createRequestID);
  const changed = nextProjectCreateIntent(
    first,
    { mode: "create", name: "renamed" },
    createRequestID,
  );

  assert.equal(changed.requestID, "request-2");
});

test("only discards a project task that has not started or persisted", () => {
  const pending = {
    hasHint: true,
    hasActiveRequest: false,
    hasRunCursor: false,
    isPersisted: false,
  };

  assert.equal(canDiscardPendingProjectTask(pending), true);
  assert.equal(canDiscardPendingProjectTask({ ...pending, hasActiveRequest: true }), false);
  assert.equal(canDiscardPendingProjectTask({ ...pending, hasRunCursor: true }), false);
  assert.equal(canDiscardPendingProjectTask({ ...pending, isPersisted: true }), false);
  assert.equal(canDiscardPendingProjectTask({ ...pending, hasHint: false }), false);
});
