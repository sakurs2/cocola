import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeTerminalOutput,
  encodeTerminalInput,
  terminalReconnectDelay,
  terminalWebSocketURL,
} from "./terminal-protocol.mjs";

test("terminal stdin uses the OpenSandbox binary frame prefix", () => {
  assert.deepEqual([...encodeTerminalInput("ls\r")], [0, 108, 115, 13]);
});

test("terminal output decodes live and replay frames", () => {
  assert.deepEqual(decodeTerminalOutput(Uint8Array.from([1, 111, 107])), {
    kind: "stdout",
    data: Uint8Array.from([111, 107]),
    offset: null,
  });

  const replay = new Uint8Array(11);
  replay[0] = 3;
  new DataView(replay.buffer).setBigUint64(1, 42n, false);
  replay.set([111, 107], 9);
  assert.deepEqual(decodeTerminalOutput(replay), {
    kind: "replay",
    data: Uint8Array.from([111, 107]),
    offset: 42,
  });
});

test("terminal WebSocket URL preserves replay and takeover state", () => {
  assert.equal(
    terminalWebSocketURL("会话 one", "pty-1", 42, true, {
      protocol: "https:",
      host: "cocola.example",
    }),
    "wss://cocola.example/api/conversations/%E4%BC%9A%E8%AF%9D%20one/terminal/pty-1/ws?since=42&takeover=1",
  );
});

test("terminal reconnect delay is bounded", () => {
  assert.deepEqual([0, 1, 2, 3, 9].map(terminalReconnectDelay), [500, 1000, 2000, 4000, 5000]);
});
