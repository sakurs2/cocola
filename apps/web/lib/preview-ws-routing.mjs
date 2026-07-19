// NextCustomServer lazily appends its own `upgrade` listener to the same HTTP
// server used by cocola. A preview URL also matches the App Router's HTTP route,
// so Next treats the WebSocket as a matched output and closes the socket. The
// cocola listener runs first and masks only upgrades it has already claimed.
const NEXT_UPGRADE_PASSTHROUGH_PATH = "/api/__cocola_preview_ws_passthrough__";

/** @param {{ url?: string }} request */
export function maskPreviewUpgradeFromNext(request) {
  request.url = NEXT_UPGRADE_PASSTHROUGH_PATH;
}
