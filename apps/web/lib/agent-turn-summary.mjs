const PROCESS_PART_TYPES = new Set([
  "environment",
  "reasoning",
  "tool-call",
  "progress",
  "scm-approval",
]);

const isFilePart = (part) => part?.type === "file";
const isHiddenMemoryMiss = (part) =>
  (part?.type === "memory-recall" && part?.status === "miss") ||
  (part?.type === "data" && part?.name === "memory-recall" && part?.data?.status === "miss");
const isProcessPart = (part) =>
  !isHiddenMemoryMiss(part) &&
  (PROCESS_PART_TYPES.has(part?.type) ||
    part?.type === "memory-recall" ||
    (part?.type === "data" &&
      (part?.name === "progress" ||
        part?.name === "memory-recall" ||
        part?.name === "scm-approval")));

/**
 * Splits one assistant turn into the collapsible process and the always-visible
 * final output. Files stay visible even if they were emitted before the final
 * tool call.
 */
export const splitAgentTurnParts = (parts, hasExternalEnvironment = false) => {
  const safeParts = Array.isArray(parts) ? parts : [];
  let lastProcessIndex = -1;

  safeParts.forEach((part, index) => {
    if (isProcessPart(part)) lastProcessIndex = index;
  });

  const processIndices = [];
  const outputIndices = [];

  safeParts.forEach((part, index) => {
    if (index <= lastProcessIndex && !isFilePart(part)) {
      processIndices.push(index);
    } else {
      outputIndices.push(index);
    }
  });

  return {
    processIndices,
    outputIndices,
    hasProcess: hasExternalEnvironment || lastProcessIndex >= 0,
  };
};

/**
 * Keeps the final-output renderer stable while a turn moves from streaming to
 * complete. Only process parts change presentation at completion: they move
 * from the expanded timeline into the collapsed summary. Output indices remain
 * in one dedicated render path in both states, so the final text is never
 * mounted by both MessagePrimitive.Parts and PartByIndex during the hand-off.
 */
export const buildAgentTurnRenderPlan = (
  parts,
  hasExternalEnvironment = false,
  streaming = false,
) => {
  const split = splitAgentTurnParts(parts, hasExternalEnvironment);
  return {
    ...split,
    expandedProcessIndices: streaming ? split.processIndices : [],
    summaryProcessIndices: !streaming && split.hasProcess ? split.processIndices : [],
    showProcessSummary: !streaming && split.hasProcess,
  };
};

const finiteNonNegativeNumber = (value) => {
  if (typeof value === "string" && value.trim() !== "") value = Number(value);
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : undefined;
};

export const formatAgentDuration = (durationMs) => {
  const duration = finiteNonNegativeNumber(durationMs);
  if (duration === undefined) return "";

  const totalSeconds = duration > 0 ? Math.max(1, Math.floor(duration / 1_000)) : 0;
  if (totalSeconds < 60) return `${totalSeconds}s`;

  const totalMinutes = Math.floor(totalSeconds / 60);
  if (totalMinutes < 60) return `${totalMinutes}m ${totalSeconds % 60}s`;

  return `${Math.floor(totalMinutes / 60)}h ${totalMinutes % 60}m`;
};

const timestampMs = (value) => {
  if (value instanceof Date) return Number.isFinite(value.getTime()) ? value.getTime() : undefined;
  if (typeof value === "number") return Number.isFinite(value) ? value : undefined;
  if (typeof value !== "string" || value.trim() === "") return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
};

export const inferAgentDurationMs = (metadataDuration, userCreatedAt, assistantCreatedAt) => {
  const explicit = finiteNonNegativeNumber(metadataDuration);
  if (explicit !== undefined) return explicit;

  const startedAt = timestampMs(userCreatedAt);
  const completedAt = timestampMs(assistantCreatedAt);
  if (startedAt === undefined || completedAt === undefined || completedAt < startedAt)
    return undefined;
  return completedAt - startedAt;
};

const fileReference = (part) => {
  const filename = typeof part?.filename === "string" && part.filename ? part.filename : "file";
  let url = "";
  if (typeof part?.downloadUrl === "string") url = part.downloadUrl;
  if (!url && typeof part?.download_url === "string") url = part.download_url;
  if (!url && typeof part?.data === "string") {
    try {
      const parsed = JSON.parse(part.data);
      url = typeof parsed?.url === "string" ? parsed.url : "";
    } catch {
      url = part.data;
    }
  }
  return `[file] ${filename}${url ? ` ${url}` : ""}`;
};

export const finalAgentOutputText = (parts, outputIndices) => {
  const safeParts = Array.isArray(parts) ? parts : [];
  const indices = Array.isArray(outputIndices)
    ? outputIndices
    : splitAgentTurnParts(safeParts).outputIndices;

  return indices
    .map((index) => {
      const part = safeParts[index];
      if (part?.type === "text" && typeof part.text === "string") return part.text.trim();
      if (part?.type === "file") return fileReference(part);
      return "";
    })
    .filter(Boolean)
    .join("\n\n");
};
