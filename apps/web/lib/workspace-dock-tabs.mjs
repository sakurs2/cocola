export function dockPageInstanceID(templateID, ordinal) {
  const normalizedID = String(templateID ?? "").trim();
  if (!normalizedID) throw new Error("dock page template id is required");
  if (!Number.isInteger(ordinal) || ordinal < 1) {
    throw new Error("dock page instance ordinal must be a positive integer");
  }
  return `dock:${encodeURIComponent(normalizedID)}:${ordinal}`;
}

export function dockPageInstanceLabel(baseLabel, ordinal) {
  const normalizedLabel = String(baseLabel ?? "").trim() || "Panel";
  if (!Number.isInteger(ordinal) || ordinal < 1) {
    throw new Error("dock page instance ordinal must be a positive integer");
  }
  return ordinal === 1 ? normalizedLabel : `${normalizedLabel} ${ordinal}`;
}
