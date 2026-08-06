import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const chatPageSource = await readFile(new URL("../app/page.tsx", import.meta.url), "utf8");
const statusPanelSource = await readFile(
  new URL("../components/assistant-ui/session-status-panel.tsx", import.meta.url),
  "utf8",
);
const shellPageSource = await readFile(
  new URL("../components/assistant-ui/shell-page.tsx", import.meta.url),
  "utf8",
);

test("session status fits its content until expanded details need an internal scroll", () => {
  assert.match(
    chatPageSource,
    /max-h-\[calc\(100svh-4rem\)\][^\n]*md:max-h-\[min\(36rem,calc\(100svh-5rem\)\)\]/,
  );
  assert.match(chatPageSource, /md:w-80 md:shrink-0 md:self-start/);
  assert.doesNotMatch(chatPageSource, /md:self-stretch/);
  assert.match(statusPanelSource, /flex max-h-\[inherit\] flex-col/);
  assert.match(statusPanelSource, /min-h-0 flex-1 overflow-y-auto/);
});

test("the session panel cannot reserve welcome-screen width without an active conversation", () => {
  assert.match(
    chatPageSource,
    /if \(!hasMessages \|\| !activeSessionId \|\| !environmentStatus \|\| selectedArtifact\) return;/,
  );
  assert.match(
    chatPageSource,
    /hasMessages &&\s+activeSessionId &&\s+environmentStatus &&\s+statusOpen &&\s+dockView === "status"/,
  );
});

test("the shell leaves a stalled WebSocket handshake with an actionable error", () => {
  assert.match(shellPageSource, /const TERMINAL_CONNECT_TIMEOUT_MS = 15 \* 1000/);
  assert.match(shellPageSource, /setError\("The terminal connection timed out"\)/);
  assert.match(shellPageSource, /clearConnectTimeout\(\);\n\s+setStatus\("ready"\)/);
});
