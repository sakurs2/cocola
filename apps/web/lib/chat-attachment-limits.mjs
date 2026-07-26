export const MAX_CHAT_ATTACHMENTS = 8;
export const MAX_CHAT_ATTACHMENT_BYTES = 32 * 1024 * 1024;
export const MAX_CHAT_ATTACHMENTS_TOTAL_BYTES = 32 * 1024 * 1024;

export function decodedBase64Size(value) {
  if (typeof value !== "string" || value.length === 0) return 0;
  const compact = value.replace(/[\r\n]/g, "");
  const padding = compact.endsWith("==") ? 2 : compact.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((compact.length * 3) / 4) - padding);
}

export function validateChatAttachments(attachments) {
  if (attachments.length > MAX_CHAT_ATTACHMENTS) {
    throw new Error(`You can attach at most ${MAX_CHAT_ATTACHMENTS} files per message.`);
  }

  let totalBytes = 0;
  for (const attachment of attachments) {
    const size =
      typeof attachment.size_bytes === "number"
        ? attachment.size_bytes
        : decodedBase64Size(attachment.content_b64);
    if (size > MAX_CHAT_ATTACHMENT_BYTES) {
      throw new Error(`File "${attachment.filename}" exceeds the 32 MB attachment limit.`);
    }
    totalBytes += size;
    if (totalBytes > MAX_CHAT_ATTACHMENTS_TOTAL_BYTES) {
      throw new Error("Attachments exceed the 32 MB total limit per message.");
    }
  }
}
