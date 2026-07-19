import assert from "node:assert/strict";
import test from "node:test";

import { classifyCodeEditorProbe, codeEditorRetryDelay } from "./code-editor-readiness.mjs";

test("a blank conversation does not probe or load code-server", () => {
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: false,
      environmentPreparing: false,
      responseStatus: 502,
    }),
    { kind: "not-started", retry: false },
  );
});

test("a preparing conversation waits and retries transient failures", () => {
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: true,
    }),
    { kind: "waiting", retry: false },
  );
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: true,
      responseStatus: 502,
    }),
    { kind: "waiting", retry: true },
  );
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: true,
      networkFailed: true,
    }),
    { kind: "waiting", retry: true },
  );
});

test("an idle historical conversation reports a reclaimed sandbox", () => {
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: false,
      responseStatus: 502,
    }),
    { kind: "reclaimed", retry: false },
  );
});

test("successful and unexpected responses are classified explicitly", () => {
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: false,
      responseStatus: 200,
    }),
    { kind: "ready", retry: false },
  );
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: false,
      responseStatus: 401,
    }),
    { kind: "error", retry: false },
  );
  assert.deepEqual(
    classifyCodeEditorProbe({
      hasMessages: true,
      environmentPreparing: false,
      networkFailed: true,
    }),
    { kind: "error", retry: false },
  );
});

test("retry delay backs off and caps at five seconds", () => {
  assert.equal(codeEditorRetryDelay(0), 1000);
  assert.equal(codeEditorRetryDelay(1), 2000);
  assert.equal(codeEditorRetryDelay(2), 4000);
  assert.equal(codeEditorRetryDelay(3), 5000);
  assert.equal(codeEditorRetryDelay(20), 5000);
});
