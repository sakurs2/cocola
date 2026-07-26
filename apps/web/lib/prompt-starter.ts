export type PromptStarterFileSlot = {
  key: string;
  label: string;
  accept: readonly string[];
  required?: boolean;
};

export type PromptStarterSlotBinding = {
  slotKey: string;
  attachmentId: string;
  filename: string;
};

export type PromptStarterSlotBindings = Readonly<
  Record<string, PromptStarterSlotBinding | undefined>
>;

export type PromptStarterTextSegment = {
  text: string;
  slot?: PromptStarterFileSlot;
  binding?: PromptStarterSlotBinding;
};

export function promptStarterSlotMarker(slot: PromptStarterFileSlot): string {
  return `[${slot.label}]`;
}

export function promptStarterBoundFileMarker(filename: string): string {
  return `@${filename.trim().replace(/[\r\n]+/g, " ")}`;
}

export function layoutPromptStarterSlots(
  text: string,
  slots: readonly PromptStarterFileSlot[],
  bindings: PromptStarterSlotBindings,
): {
  segments: PromptStarterTextSegment[];
  matchedSlotKeys: string[];
} {
  const remaining = slots.map((slot) => {
    const binding = bindings[slot.key];
    const marker = promptStarterSlotMarker(slot);
    const values = binding ? [promptStarterBoundFileMarker(binding.filename), marker] : [marker];
    return { slot, binding, values };
  });
  const segments: PromptStarterTextSegment[] = [];
  const matchedSlotKeys: string[] = [];
  let cursor = 0;

  while (cursor < text.length && remaining.length > 0) {
    let nextSlotIndex = -1;
    let nextOffset = -1;
    let nextValue = "";

    for (let slotIndex = 0; slotIndex < remaining.length; slotIndex += 1) {
      const candidate = remaining[slotIndex];
      if (!candidate) continue;
      for (const value of candidate.values) {
        const offset = text.indexOf(value, cursor);
        if (
          offset >= 0 &&
          (nextOffset < 0 ||
            offset < nextOffset ||
            (offset === nextOffset && value.length > nextValue.length))
        ) {
          nextSlotIndex = slotIndex;
          nextOffset = offset;
          nextValue = value;
        }
      }
    }

    if (nextSlotIndex < 0) break;
    if (nextOffset > cursor) {
      segments.push({ text: text.slice(cursor, nextOffset) });
    }
    const match = remaining.splice(nextSlotIndex, 1)[0];
    if (!match) break;
    segments.push({
      text: nextValue,
      slot: match.slot,
      binding: match.binding,
    });
    matchedSlotKeys.push(match.slot.key);
    cursor = nextOffset + nextValue.length;
  }

  if (cursor < text.length) {
    segments.push({ text: text.slice(cursor) });
  }

  return { segments, matchedSlotKeys };
}

export function replacePromptStarterSlotValue(
  text: string,
  slot: PromptStarterFileSlot,
  binding: PromptStarterSlotBinding | undefined,
  filename: string,
): string | null {
  const safeFilename = filename.trim().replace(/[\r\n]+/g, " ");
  if (!safeFilename) return null;

  const candidates = binding?.filename
    ? [promptStarterBoundFileMarker(binding.filename), promptStarterSlotMarker(slot)]
    : [promptStarterSlotMarker(slot)];
  for (const candidate of candidates) {
    const offset = text.indexOf(candidate);
    if (offset < 0) continue;
    return `${text.slice(0, offset)}${promptStarterBoundFileMarker(safeFilename)}${text.slice(
      offset + candidate.length,
    )}`;
  }
  return null;
}

export function restorePromptStarterSlotValue(
  text: string,
  slot: PromptStarterFileSlot,
  binding: PromptStarterSlotBinding,
): string | null {
  const marker = promptStarterBoundFileMarker(binding.filename);
  const offset = text.indexOf(marker);
  if (offset < 0) return null;
  return `${text.slice(0, offset)}${promptStarterSlotMarker(slot)}${text.slice(
    offset + marker.length,
  )}`;
}

export function fileMatchesPromptStarterSlot(file: File, slot: PromptStarterFileSlot): boolean {
  const filename = file.name.toLowerCase();
  const contentType = file.type.toLowerCase();
  return slot.accept.some((rawAccept) => {
    const accept = rawAccept.trim().toLowerCase();
    if (!accept) return false;
    if (accept.startsWith(".")) return filename.endsWith(accept);
    if (accept.endsWith("/*")) return contentType.startsWith(accept.slice(0, -1));
    return contentType === accept;
  });
}

export function firstMissingPromptStarterSlot(
  slots: readonly PromptStarterFileSlot[],
  bindings: PromptStarterSlotBindings,
  attachmentExists: (attachmentId: string) => boolean = () => true,
): PromptStarterFileSlot | undefined {
  return slots.find((slot) => {
    if (slot.required === false) return false;
    const binding = bindings[slot.key];
    return !binding || !attachmentExists(binding.attachmentId);
  });
}
