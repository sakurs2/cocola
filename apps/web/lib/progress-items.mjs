const MAX_PROGRESS_ITEMS = 100;

const asText = (value) => (typeof value === "string" ? value.trim() : "");

const normalizeStatus = (item) => {
  if (item?.completed === true) return "completed";

  const status = asText(item?.status)
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .replace(/[\s-]+/g, "_")
    .toLowerCase();
  if (["completed", "complete", "done", "succeeded", "success"].includes(status)) {
    return "completed";
  }
  if (["in_progress", "active", "running", "started"].includes(status)) {
    return "in_progress";
  }
  return "pending";
};

export const normalizeProgressItems = (value) => {
  if (!Array.isArray(value)) return [];

  return value.slice(0, MAX_PROGRESS_ITEMS).flatMap((item, index) => {
    const record = item && typeof item === "object" && !Array.isArray(item) ? item : undefined;
    const text =
      asText(item) ||
      asText(record?.content) ||
      asText(record?.text) ||
      asText(record?.title) ||
      asText(record?.description) ||
      asText(record?.name);
    if (!text) return [];

    return [
      {
        id: asText(record?.id) || `${index}:${text}`,
        text,
        status: normalizeStatus(record),
      },
    ];
  });
};
