// Base64 attachment adapter for cocola's P0 inline file upload.
//
// assistant-ui ships two built-in adapters, but neither yields a uniform,
// machine-readable payload we can push to the sandbox:
//   - SimpleTextAttachmentAdapter inlines the file's text wrapped in an
//     <attachment name=...> envelope (great for LLM context, useless as a
//     file to write to disk).
//   - SimpleImageAttachmentAdapter emits a `data:` base64 URL.
// They are heterogeneous. For the "backend pre-provision (push)" model we need
// every attachment — text, code, or image alike — to arrive as a single
// FileMessagePart carrying the RAW base64 bytes + filename + mime, so onNew can
// forward {filename, content_b64, mime} verbatim over the wire.
//
// P0 scope (docs/plan/web-file-upload.md §3.3): inline only, single-file size
// cap, accept whitelist. Binary-large / OSS-backed uploads are P1.

import type {
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from "@assistant-ui/react";

// 1 MB inline cap (base64 inflates ~33%, still well within a JSON POST body).
const MAX_BYTES = 1024 * 1024;

// Text / code / common images. Kept permissive but explicit; binary blobs are
// rejected at add() time in P0.
const ACCEPT = [
  "text/*",
  "image/*",
  ".md",
  ".txt",
  ".py",
  ".ts",
  ".tsx",
  ".js",
  ".jsx",
  ".json",
  ".csv",
  ".tsv",
  ".yaml",
  ".yml",
  ".toml",
  ".go",
  ".rs",
  ".java",
  ".c",
  ".cc",
  ".cpp",
  ".h",
  ".hpp",
  ".sh",
  ".sql",
  ".html",
  ".css",
].join(",");

// Browser File -> raw base64 (no `data:` prefix), chunked to avoid blowing the
// call stack on String.fromCharCode(...bigArray).
async function fileToBase64(file: File): Promise<string> {
  const buf = await file.arrayBuffer();
  const bytes = new Uint8Array(buf);
  let binary = "";
  const CHUNK = 0x8000;
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }
  return btoa(binary);
}

export class Base64AttachmentAdapter implements AttachmentAdapter {
  accept = ACCEPT;

  async add(state: { file: File }): Promise<PendingAttachment> {
    const { file } = state;
    if (file.size > MAX_BYTES) {
      throw new Error(
        `File "${file.name}" is ${(file.size / 1024 / 1024).toFixed(1)} MB; the ${(
          MAX_BYTES /
          1024 /
          1024
        ).toFixed(0)} MB inline limit is exceeded.`,
      );
    }
    return {
      id: file.name,
      type: file.type.startsWith("image/") ? "image" : "file",
      name: file.name,
      contentType: file.type,
      file,
      status: { type: "requires-action", reason: "composer-send" },
    };
  }

  async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
    const data = await fileToBase64(attachment.file);
    const mimeType = attachment.contentType || attachment.file.type || "application/octet-stream";
    return {
      ...attachment,
      status: { type: "complete" },
      // Uniform: always a FileMessagePart carrying RAW base64 in `data`.
      // onNew reads this back into {filename, content_b64, mime}.
      content: [
        {
          type: "file",
          filename: attachment.name,
          data,
          mimeType,
        },
      ],
    };
  }

  async remove(): Promise<void> {
    // Inline attachments hold no server-side resource; nothing to clean up.
  }
}
