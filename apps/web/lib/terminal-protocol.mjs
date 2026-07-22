export const TERMINAL_BIN_STDIN = 0x00;
export const TERMINAL_BIN_STDOUT = 0x01;
export const TERMINAL_BIN_STDERR = 0x02;
export const TERMINAL_BIN_REPLAY = 0x03;

/** @param {string} text */
export function encodeTerminalInput(text) {
  const data = new TextEncoder().encode(text);
  const frame = new Uint8Array(data.length + 1);
  frame[0] = TERMINAL_BIN_STDIN;
  frame.set(data, 1);
  return frame;
}

/**
 * @param {ArrayBuffer | Uint8Array} value
 * @returns {{ kind: "stdout" | "stderr" | "replay"; data: Uint8Array; offset: number | null } | null}
 */
export function decodeTerminalOutput(value) {
  const frame = value instanceof Uint8Array ? value : new Uint8Array(value);
  if (frame.length < 1) return null;
  if (frame[0] === TERMINAL_BIN_STDOUT || frame[0] === TERMINAL_BIN_STDERR) {
    return {
      kind: frame[0] === TERMINAL_BIN_STDOUT ? "stdout" : "stderr",
      data: frame.slice(1),
      offset: null,
    };
  }
  if (frame[0] !== TERMINAL_BIN_REPLAY || frame.length < 9) return null;
  const offset = new DataView(frame.buffer, frame.byteOffset + 1, 8).getBigUint64(0, false);
  if (offset > BigInt(Number.MAX_SAFE_INTEGER)) return null;
  return { kind: "replay", data: frame.slice(9), offset: Number(offset) };
}

/**
 * @param {string} conversationID
 * @param {string} terminalID
 * @param {number} offset
 * @param {boolean} takeover
 * @param {{ protocol: string; host: string }} location
 */
export function terminalWebSocketURL(
  conversationID,
  terminalID,
  offset = 0,
  takeover = false,
  location = globalThis.location,
) {
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const path =
    `/api/conversations/${encodeURIComponent(conversationID)}/terminal/` +
    `${encodeURIComponent(terminalID)}/ws`;
  const query = new URLSearchParams();
  if (offset > 0) query.set("since", String(Math.floor(offset)));
  if (takeover) query.set("takeover", "1");
  return `${protocol}//${location.host}${path}${query.size > 0 ? `?${query}` : ""}`;
}

/** @param {number} attempt */
export function terminalReconnectDelay(attempt) {
  const normalized = Number.isFinite(attempt) ? Math.max(0, Math.floor(attempt)) : 0;
  return Math.min(500 * 2 ** normalized, 5000);
}
