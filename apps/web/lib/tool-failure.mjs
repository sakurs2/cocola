const COMMAND_TOOL_NAMES = new Set(["bash", "command_execution"]);

export const isCommandTool = (rawName) =>
  COMMAND_TOOL_NAMES.has(String(rawName || "").toLowerCase());
