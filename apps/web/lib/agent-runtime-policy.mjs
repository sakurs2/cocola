export function selectAgentRuntime({
  runtimes,
  defaultRuntimeId,
  pickerEnabled,
  preferredRuntimeId,
}) {
  const configuredDefault = runtimes.find((runtime) => runtime.id === defaultRuntimeId) ?? null;
  if (!configuredDefault) return null;
  if (!pickerEnabled) return configuredDefault;
  return runtimes.find((runtime) => runtime.id === preferredRuntimeId) ?? configuredDefault;
}
