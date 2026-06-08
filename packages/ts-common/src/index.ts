// Shared TS primitives. Mirrors go-common/errors + py-common/errors.
//
// Pinned to a tiny surface in M0: types only, no runtime deps. Real shared
// API client / fetch helpers land in M4 alongside the web↔gateway contract.

export type ErrorCode =
  | "UNKNOWN"
  | "INVALID_ARGUMENT"
  | "NOT_FOUND"
  | "PERMISSION_DENIED"
  | "UNAVAILABLE"
  | "INTERNAL";

export interface ApiError {
  code: ErrorCode;
  message: string;
  details?: Record<string, unknown>;
}

export interface AgentEvent {
  kind: "text" | "tool_use" | "tool_result" | "error" | "done";
  data: Record<string, unknown>;
}
