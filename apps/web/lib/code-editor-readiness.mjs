/**
 * Classify the Code panel without letting a read-only preview request acquire a
 * sandbox. A 502 while a turn is preparing is transient; the same response for
 * an idle historical conversation means its sandbox has been reclaimed.
 *
 * @param {{
 *   hasMessages: boolean;
 *   environmentPreparing: boolean;
 *   responseStatus?: number | null;
 *   networkFailed?: boolean;
 * }} input
 * @returns {{
 *   kind: "not-started" | "checking" | "waiting" | "ready" | "reclaimed" | "error";
 *   retry: boolean;
 * }}
 */
export function classifyCodeEditorProbe({
  hasMessages,
  environmentPreparing,
  responseStatus = null,
  networkFailed = false,
}) {
  if (!hasMessages) return { kind: "not-started", retry: false };

  if (responseStatus == null && !networkFailed) {
    return {
      kind: environmentPreparing ? "waiting" : "checking",
      retry: false,
    };
  }

  if (responseStatus != null && responseStatus >= 200 && responseStatus < 400) {
    return { kind: "ready", retry: false };
  }

  if (responseStatus === 502) {
    return environmentPreparing
      ? { kind: "waiting", retry: true }
      : { kind: "reclaimed", retry: false };
  }

  if (networkFailed && environmentPreparing) {
    return { kind: "waiting", retry: true };
  }

  return { kind: "error", retry: false };
}

/** @param {number} attempt */
export function codeEditorRetryDelay(attempt) {
  const safeAttempt = Number.isFinite(attempt) ? Math.max(0, Math.floor(attempt)) : 0;
  return Math.min(1000 * 2 ** safeAttempt, 5000);
}
