export const commandOutputDelta = (item, previous = "") => {
  if (item?.type !== "command_execution") return { current: previous, delta: "" };
  const current = String(item.aggregated_output || "");
  const delta = current.startsWith(previous) ? current.slice(previous.length) : current;
  return { current, delta };
};
