export const WIKI_COMPOSER_CONTENT_TYPE = "application/vnd.cocola.wiki-reference+json";

export type WikiComposerReference = {
  nodeId: string;
  filename: string;
  logicalPath: string;
};

export type WikiComposerTextSegment = {
  text: string;
  reference?: WikiComposerReference;
};

type ComposerAttachmentLike = {
  id?: string;
  name?: string;
  contentType?: string;
  content?: readonly {
    type: string;
    text?: string;
  }[];
};

export function wikiComposerAttachmentID(nodeId: string, instanceId = ""): string {
  return `wiki:${nodeId}${instanceId ? `:${instanceId}` : ""}`;
}

export function createWikiComposerAttachment(reference: WikiComposerReference, instanceId = "") {
  return {
    id: wikiComposerAttachmentID(reference.nodeId, instanceId),
    type: "document" as const,
    name: reference.filename,
    contentType: WIKI_COMPOSER_CONTENT_TYPE,
    content: [
      {
        type: "text" as const,
        text: JSON.stringify(reference),
      },
    ],
  };
}

export function wikiComposerMentionText(filename: string): string {
  return `@${filename.trim().replace(/[\r\n]+/g, " ")}`;
}

export function layoutWikiComposerMentions(
  text: string,
  references: readonly WikiComposerReference[],
): {
  segments: WikiComposerTextSegment[];
  matchedReferenceIndexes: number[];
} {
  const remaining = references
    .map((reference, referenceIndex) => ({
      marker: wikiComposerMentionText(reference.filename),
      reference,
      referenceIndex,
    }))
    .filter(({ marker }) => marker.length > 1);
  const segments: WikiComposerTextSegment[] = [];
  const matchedReferenceIndexes: number[] = [];
  let cursor = 0;

  while (cursor < text.length && remaining.length > 0) {
    let nextIndex = -1;
    let nextOffset = -1;
    for (let index = 0; index < remaining.length; index += 1) {
      const candidate = remaining[index];
      if (!candidate) continue;
      const currentMatch = nextIndex >= 0 ? remaining[nextIndex] : undefined;
      const offset = text.indexOf(candidate.marker, cursor);
      if (
        offset >= 0 &&
        (nextOffset < 0 ||
          offset < nextOffset ||
          (offset === nextOffset &&
            (!currentMatch || candidate.marker.length > currentMatch.marker.length)))
      ) {
        nextIndex = index;
        nextOffset = offset;
      }
    }
    if (nextIndex < 0) break;

    if (nextOffset > cursor) {
      segments.push({ text: text.slice(cursor, nextOffset) });
    }
    const match = remaining.splice(nextIndex, 1)[0];
    if (!match) break;
    segments.push({ text: match.marker, reference: match.reference });
    matchedReferenceIndexes.push(match.referenceIndex);
    cursor = nextOffset + match.marker.length;
  }

  if (cursor < text.length) {
    segments.push({ text: text.slice(cursor) });
  }

  return { segments, matchedReferenceIndexes };
}

export function isWikiComposerAttachment(
  attachment: Pick<ComposerAttachmentLike, "id" | "contentType">,
): boolean {
  return (
    attachment.contentType === WIKI_COMPOSER_CONTENT_TYPE &&
    Boolean(attachment.id?.startsWith("wiki:"))
  );
}

export function wikiReferencesFromAttachments(
  attachments: readonly ComposerAttachmentLike[],
): WikiComposerReference[] {
  const references: WikiComposerReference[] = [];
  for (const attachment of attachments) {
    if (!isWikiComposerAttachment(attachment)) continue;
    const text = attachment.content?.find((part) => part.type === "text")?.text;
    if (!text) continue;
    try {
      const raw = JSON.parse(text) as Partial<WikiComposerReference>;
      const nodeId = typeof raw.nodeId === "string" ? raw.nodeId.trim() : "";
      const filename = typeof raw.filename === "string" ? raw.filename.trim() : "";
      const logicalPath = typeof raw.logicalPath === "string" ? raw.logicalPath.trim() : "";
      if (!nodeId || !filename) continue;
      references.push({
        nodeId,
        filename,
        logicalPath: logicalPath || filename,
      });
    } catch {
      // Ignore malformed client-only attachment metadata.
    }
  }
  return references;
}

export function mergeWikiComposerReferences(
  ...groups: readonly (readonly WikiComposerReference[])[]
): WikiComposerReference[] {
  const merged: WikiComposerReference[] = [];
  const seen = new Set<string>();
  for (const group of groups) {
    for (const reference of group) {
      const nodeId = reference.nodeId.trim();
      const filename = reference.filename.trim();
      if (!nodeId || !filename || seen.has(nodeId)) continue;
      seen.add(nodeId);
      merged.push({
        nodeId,
        filename,
        logicalPath: reference.logicalPath.trim() || filename,
      });
    }
  }
  return merged;
}

export function wikiPromptText(text: string, references: readonly WikiComposerReference[]): string {
  if (text.trim() || references.length === 0) return text;
  return references.length === 1
    ? "Use the referenced Wiki file."
    : "Use the referenced Wiki files.";
}
