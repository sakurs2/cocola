const CODEX_REASONING_EFFORTS = new Set(["minimal", "low", "medium", "high", "xhigh"]);

export const withCodexReasoningEffort = (threadOptions, rawEffort) => {
  const effort = String(rawEffort || "").trim();
  if (!effort) return threadOptions;
  if (!CODEX_REASONING_EFFORTS.has(effort)) {
    throw new Error(`unsupported Codex reasoning effort: ${effort}`);
  }
  return { ...threadOptions, modelReasoningEffort: effort };
};
