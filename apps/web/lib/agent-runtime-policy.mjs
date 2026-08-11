export function selectAgentRuntime({ runtimes, defaultRuntimeId }) {
  return runtimes.find((runtime) => runtime.id === defaultRuntimeId) ?? null;
}
