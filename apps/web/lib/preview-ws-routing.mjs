// NextCustomServer lazily appends its own `upgrade` listener to the same HTTP
// server used by cocola. A preview URL also matches the App Router's HTTP route,
// so Next treats the WebSocket as a matched output and closes the socket. The
// cocola listener runs first and masks only upgrades it has already claimed.
const NEXT_UPGRADE_PASSTHROUGH_PATH = "/api/__cocola_preview_ws_passthrough__";

/**
 * Translate the two same-origin WebSocket surfaces owned by Cocola's custom
 * server into their authenticated Gateway paths. Returning null leaves the
 * upgrade to Next (notably its development HMR socket).
 *
 * @param {string} requestURL
 */
export function buildGatewayWebSocketPath(requestURL) {
  let parsed;
  try {
    parsed = new URL(requestURL, "http://cocola.local");
  } catch {
    return null;
  }

  const preview = /^\/api\/preview\/([^/]+)\/([^/]+)(?:\/(.*))?$/.exec(parsed.pathname);
  if (preview) {
    const sessionID = decodePathSegment(preview[1]);
    if (sessionID == null) return null;
    const port = Number(preview[2]);
    if (!Number.isInteger(port) || port <= 0 || port > 65535) return null;
    const rest = preview[3] ?? "";
    return `/v1/preview/${encodeURIComponent(sessionID)}/${port}/${rest}${parsed.search}`;
  }

  const terminal = /^\/api\/conversations\/([^/]+)\/terminal\/([^/]+)\/ws$/.exec(parsed.pathname);
  if (terminal) {
    const conversationID = decodePathSegment(terminal[1]);
    const terminalID = decodePathSegment(terminal[2]);
    if (
      conversationID == null ||
      terminalID == null ||
      !/^[A-Za-z0-9_-]{1,128}$/.test(terminalID)
    ) {
      return null;
    }
    return (
      `/v1/conversations/${encodeURIComponent(conversationID)}/terminal/` +
      `${encodeURIComponent(terminalID)}/ws${parsed.search}`
    );
  }

  return null;
}

function decodePathSegment(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return null;
  }
}

/** @param {{ url?: string }} request */
export function maskPreviewUpgradeFromNext(request) {
  request.url = NEXT_UPGRADE_PASSTHROUGH_PATH;
}
