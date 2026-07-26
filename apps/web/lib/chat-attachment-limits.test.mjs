import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  MAX_CHAT_ATTACHMENTS,
  MAX_CHAT_ATTACHMENTS_TOTAL_BYTES,
  decodedBase64Size,
  validateChatAttachments,
} from "./chat-attachment-limits.mjs";

const chatProxySource = await readFile(
  new URL("../app/api/chat/route.ts", import.meta.url),
  "utf8",
);

test("chat attachment preflight accepts the supported boundary", () => {
  const attachments = Array.from({ length: MAX_CHAT_ATTACHMENTS }, (_, index) => ({
    filename: `${index}.txt`,
    size_bytes: MAX_CHAT_ATTACHMENTS_TOTAL_BYTES / MAX_CHAT_ATTACHMENTS,
  }));
  assert.doesNotThrow(() => validateChatAttachments(attachments));
  assert.equal(decodedBase64Size("aGVsbG8="), 5);
});

test("chat attachment preflight rejects count, per-file, and total limits", () => {
  assert.throws(
    () =>
      validateChatAttachments(
        Array.from({ length: MAX_CHAT_ATTACHMENTS + 1 }, (_, index) => ({
          filename: `${index}.txt`,
          size_bytes: 1,
        })),
      ),
    /at most 8 files/,
  );
  assert.throws(
    () =>
      validateChatAttachments([
        { filename: "large.bin", size_bytes: MAX_CHAT_ATTACHMENTS_TOTAL_BYTES + 1 },
      ]),
    /32 MB attachment limit/,
  );
  assert.throws(
    () =>
      validateChatAttachments([
        { filename: "one.bin", size_bytes: MAX_CHAT_ATTACHMENTS_TOTAL_BYTES / 2 + 1 },
        { filename: "two.bin", size_bytes: MAX_CHAT_ATTACHMENTS_TOTAL_BYTES / 2 },
      ]),
    /32 MB total limit/,
  );
});

test("chat proxy forwards the request stream without buffering it as text", () => {
  assert.doesNotMatch(chatProxySource, /req\.text\(\)/);
  assert.match(chatProxySource, /body:\s*req\.body/);
  assert.match(chatProxySource, /duplex:\s*"half"/);
});
