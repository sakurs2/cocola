export const REASONING_PRESETS = ["auto", "fast", "deep", "max"];

const EFFORT_PREFERENCES = {
  auto: [],
  fast: ["low"],
  deep: ["high", "medium"],
  max: ["max", "xhigh"],
};

/**
 * Resolve one product preset to the strongest matching model-native value.
 * An empty result means the preset is unavailable for that route.
 */
export const resolveReasoningEffort = (preset, supportedEfforts) => {
  if (preset === "auto") return "";
  const supported = new Set(Array.isArray(supportedEfforts) ? supportedEfforts : []);
  return (EFFORT_PREFERENCES[preset] ?? []).find((effort) => supported.has(effort)) ?? "";
};

export const reasoningPresetOptions = (supportedEfforts) =>
  REASONING_PRESETS.map((id) => ({
    id,
    available: id === "auto" || Boolean(resolveReasoningEffort(id, supportedEfforts)),
  }));

export const reasoningPresetForEffort = (effort) => {
  if (effort === "low") return "fast";
  if (effort === "medium" || effort === "high") return "deep";
  if (effort === "xhigh" || effort === "max") return "max";
  return "auto";
};
