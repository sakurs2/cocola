const COMMAND_TOOL_NAMES = new Set(["bash", "command_execution"]);
const TOOL_OUTCOMES = new Set(["success", "permission_denied", "unavailable", "failed", "timeout"]);

export const isCommandTool = (rawName) =>
  COMMAND_TOOL_NAMES.has(String(rawName || "").toLowerCase());

export const normalizeToolOutcome = (rawOutcome, isError) => {
  const outcome = String(rawOutcome || "");
  if (!isError) return "success";
  if (TOOL_OUTCOMES.has(outcome) && outcome !== "success") return outcome;
  return "failed";
};

export const toolOutcomeFromArtifact = (artifact, isError) => {
  if (!artifact || typeof artifact !== "object" || Array.isArray(artifact)) {
    return normalizeToolOutcome("", isError);
  }
  return normalizeToolOutcome(artifact.cocolaToolOutcome, isError);
};

export const toolOutcomeLabel = (rawName, rawOutcome, isError) => {
  const command = isCommandTool(rawName);
  const outcome = normalizeToolOutcome(rawOutcome, isError);
  if (outcome === "success") return command ? "Command completed" : "Tool call completed";
  if (outcome === "permission_denied") {
    return command ? "Command blocked in Plan mode" : "Tool call blocked in Plan mode";
  }
  if (outcome === "unavailable") {
    return command ? "Command is unavailable" : "Tool is unavailable";
  }
  if (outcome === "timeout") {
    return command ? "Command timed out" : "Tool call timed out";
  }
  return command ? "Command failed" : "Tool call failed";
};
