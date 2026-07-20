/**
 * Build the stable dock tab ID for an agent-generated artifact.
 *
 * Artifact IDs are scoped to a conversation, so both values are required to
 * prevent tabs from different conversations from colliding.
 *
 * @param {string} sessionID
 * @param {string} artifactID
 */
export function artifactPreviewTabID(sessionID, artifactID) {
  if (!sessionID || !artifactID) {
    throw new TypeError("artifact preview tabs require a session and artifact ID");
  }
  return `artifact:${encodeURIComponent(sessionID)}:${encodeURIComponent(artifactID)}`;
}
