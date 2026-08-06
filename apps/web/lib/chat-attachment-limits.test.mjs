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
const attachmentAdapterSource = await readFile(
  new URL("./base64-attachment-adapter.ts", import.meta.url),
  "utf8",
);
const runtimeProviderSource = await readFile(
  new URL("../app/runtime-provider.tsx", import.meta.url),
  "utf8",
);
const threadSource = await readFile(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
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

test("attachments are prepared before submit and remain visible in user history", () => {
  assert.match(attachmentAdapterSource, /async \*add\(/);
  assert.match(
    attachmentAdapterSource,
    /yield attachment;[\s\S]*?const prepared = await preparation/,
  );
  assert.match(attachmentAdapterSource, /status: \{ type: "complete" \}/);
  assert.match(runtimeProviderSource, /message\.role === "user"[\s\S]*?attachments/);
  assert.match(runtimeProviderSource, /const attachmentParts = \(message\.attachments \?\? \[\]\)/);
  assert.match(
    runtimeProviderSource,
    /parts: \[[\s\S]*?\{ type: "text", text \},[\s\S]*?\.\.\.attachmentParts/,
  );
});

test("composer and historical attachment chips use Cocola file-type icons", () => {
  assert.match(threadSource, /import \{ resolveFileType \} from "@\/lib\/file-type"/);
  assert.match(threadSource, /import \{ MaterialFileIcon \} from "@\/lib\/material-file-icons"/);
  assert.match(
    threadSource,
    /const AttachmentFileTypeIcon:[\s\S]*?resolveFileType\(name, contentType \?\? ""\)\.icon/,
  );
  assert.equal(threadSource.match(/<AttachmentFileTypeIcon \/>/g)?.length, 2);
});
