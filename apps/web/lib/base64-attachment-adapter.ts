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
// Scope (ADR-0017): the client still inlines every attachment as base64 in the
// POST body; the gateway is the one that uploads to MinIO (source of truth) and
// splits small/large by COCOLA_ATTACHMENT_INLINE_MAX_BYTES. So this cap is NOT
// the small/large split threshold -- it is the hard ceiling on what a single
// upload may carry over the client->gateway JSON hop. Client-side presigned
// direct-to-OSS upload (which would lift this ceiling) is P1b/TODO.

import type {
  Attachment,
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from "@assistant-ui/react";

// Hard upload ceiling, configurable so it is not baked in (mirrors the
// gateway's own configurable threshold, ADR-0017). NEXT_PUBLIC_ so it reaches
// this browser-side adapter; a non-positive/invalid value falls back to 32 MiB
// (comfortably above the 16 MiB default split so both delivery paths -- inline
// and backend-pull -- are reachable from the UI).
const DEFAULT_MAX_BYTES = 32 * 1024 * 1024;
function resolveMaxBytes(): number {
  const raw = Number(process.env.NEXT_PUBLIC_ATTACHMENT_MAX_BYTES);
  return Number.isFinite(raw) && raw > 0 ? raw : DEFAULT_MAX_BYTES;
}
const MAX_BYTES = resolveMaxBytes();

// Text / code / common images, plus the spreadsheet format used by the Excel
// analysis Prompt Starter. Kept permissive but explicit.
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
  ".xlsx",
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

type PreparedAttachment = {
  data: string;
  mimeType: string;
};

// Browser File -> raw base64 (no `data:` prefix). FileReader lets the browser
// perform the expensive conversion outside the submit path; the arrayBuffer
// fallback keeps the adapter usable in non-browser tests and SSR tooling.
async function fileToBase64(file: File): Promise<string> {
  if (typeof FileReader !== "undefined") {
    return new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const value = typeof reader.result === "string" ? reader.result : "";
        const separator = value.indexOf(",");
        resolve(separator >= 0 ? value.slice(separator + 1) : value);
      };
      reader.onerror = () => reject(reader.error ?? new Error("Could not read attachment."));
      reader.readAsDataURL(file);
    });
  }

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
  private readonly prepared = new Map<string, Promise<PreparedAttachment>>();

  async *add(state: { file: File }): AsyncGenerator<PendingAttachment, void> {
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
    const attachment: PendingAttachment = {
      id: crypto.randomUUID(),
      type: file.type.startsWith("image/") ? "image" : "file",
      name: file.name,
      contentType: file.type,
      file,
      status: { type: "requires-action", reason: "composer-send" },
    };
    const preparation = fileToBase64(file).then((data) => ({
      data,
      mimeType: file.type || "application/octet-stream",
    }));
    this.prepared.set(attachment.id, preparation);

    // Make the chip visible immediately, then replace it with a complete
    // attachment as soon as the browser finishes reading the file. This moves
    // base64 work out of the send click, so opening a new conversation is not
    // held behind file preparation.
    yield attachment;
    const prepared = await preparation;
    yield {
      ...attachment,
      status: { type: "complete" },
      content: [
        {
          type: "file",
          filename: attachment.name,
          data: prepared.data,
          mimeType: prepared.mimeType,
        },
      ],
    } as unknown as PendingAttachment;
    this.prepared.delete(attachment.id);
  }

  async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
    const prepared = await (this.prepared.get(attachment.id) ??
      fileToBase64(attachment.file).then((data) => ({
        data,
        mimeType: attachment.contentType || attachment.file.type || "application/octet-stream",
      })));
    return {
      ...attachment,
      status: { type: "complete" },
      // Uniform: always a FileMessagePart carrying RAW base64 in `data`.
      // onNew reads this back into {filename, content_b64, mime}.
      content: [
        {
          type: "file",
          filename: attachment.name,
          data: prepared.data,
          mimeType: prepared.mimeType,
        },
      ],
    };
  }

  async remove(attachment: Attachment): Promise<void> {
    this.prepared.delete(attachment.id);
    // Inline attachments hold no server-side resource; nothing to clean up.
  }
}
