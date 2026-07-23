export function canDiscardPendingProjectTask({
  hasHint,
  hasActiveRequest,
  hasRunCursor,
  isPersisted,
}) {
  return hasHint && !hasActiveRequest && !hasRunCursor && !isPersisted;
}

export function shouldOpenProjectTask({
  projectId,
  preparedProjectId,
  activeSessionId,
  preparedSessionId,
  serverAccepted,
}) {
  return preparedProjectId === projectId && preparedSessionId === activeSessionId && serverAccepted;
}

export function nextProjectCreateIntent(current, payload, createRequestID) {
  const fingerprint = JSON.stringify(payload);
  if (current?.fingerprint === fingerprint) return current;
  return { fingerprint, requestID: createRequestID() };
}
