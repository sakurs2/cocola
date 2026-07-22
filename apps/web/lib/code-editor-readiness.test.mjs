import assert from "node:assert/strict";
import test from "node:test";

import {
  buildCodeEditorURL,
  classifyCodeEditorProbe,
  codeEditorRetryDelay,
  codeEditorTabID,
  codeEditorWaitExpired,
  normalizeCodeEditorWorkspacePath,
  probeCodeEditorStatus,
} from "./code-editor-readiness.mjs";

test("code editor URLs target the selected workspace directory", () => {
  assert.equal(
    buildCodeEditorURL("session-1"),
    "/api/preview/session-1/39378/?folder=%2Fworkspace",
  );
  assert.equal(
    buildCodeEditorURL("session/with spaces", "src/web client/组件"),
    "/api/preview/session%2Fwith%20spaces/39378/?folder=%2Fworkspace%2Fsrc%2Fweb+client%2F%E7%BB%84%E4%BB%B6",
  );
  assert.equal(
    buildCodeEditorURL("project-session", "project"),
    "/api/preview/project-session/39378/?folder=%2Fworkspace%2Fproject",
  );
});

test("code editor tab IDs are stable per normalized directory", () => {
  assert.equal(normalizeCodeEditorWorkspacePath("/src/./components/"), "src/components");
  assert.equal(codeEditorTabID("src/components"), codeEditorTabID("/src/./components/"));
  assert.notEqual(codeEditorTabID("src/components"), codeEditorTabID("src/server"));
  assert.throws(() => codeEditorTabID("../etc"), /stay within \/workspace/);
});

test("code editor preparation has a finite wait budget", () => {
  assert.equal(codeEditorWaitExpired(1_000, 240_999), false);
  assert.equal(codeEditorWaitExpired(1_000, 241_000), true);
  assert.equal(codeEditorWaitExpired(Number.NaN, 1_000), true);
});

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

test("the editor probe uses GET and cancels the response body", async () => {
  const controller = new AbortController();
  let requestURL;
  let requestInit;
  let bodyCancelled = false;
  const status = await probeCodeEditorStatus(
    "/api/preview/session/39378/",
    controller.signal,
    async (url, init) => {
      requestURL = url;
      requestInit = init;
      return {
        status: 302,
        body: {
          async cancel() {
            bodyCancelled = true;
          },
        },
      };
    },
  );

  assert.equal(requestURL, "/api/preview/session/39378/");
  assert.equal(requestInit.method, "GET");
  assert.equal(requestInit.cache, "no-store");
  assert.equal(requestInit.signal, controller.signal);
  assert.equal(bodyCancelled, true);
  assert.equal(status, 302);
});

test("retry delay backs off and caps at five seconds", () => {
  assert.equal(codeEditorRetryDelay(0), 1000);
  assert.equal(codeEditorRetryDelay(1), 2000);
  assert.equal(codeEditorRetryDelay(2), 4000);
  assert.equal(codeEditorRetryDelay(3), 5000);
  assert.equal(codeEditorRetryDelay(20), 5000);
});
