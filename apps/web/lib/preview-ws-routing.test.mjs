import assert from "node:assert/strict";
import test from "node:test";

import { buildGatewayWebSocketPath, maskPreviewUpgradeFromNext } from "./preview-ws-routing.mjs";

test("preview and terminal upgrades map to authenticated Gateway paths", () => {
  assert.equal(
    buildGatewayWebSocketPath("/api/preview/session-1/39378/socket?token=one"),
    "/v1/preview/session-1/39378/socket?token=one",
  );
  assert.equal(
    buildGatewayWebSocketPath(
      "/api/conversations/session-1/terminal/123e4567-e89b-12d3-a456-426614174000/ws?since=42&takeover=1",
    ),
    "/v1/conversations/session-1/terminal/123e4567-e89b-12d3-a456-426614174000/ws?since=42&takeover=1",
  );
});

test("unowned or malformed upgrade paths remain with Next", () => {
  assert.equal(buildGatewayWebSocketPath("/_next/webpack-hmr"), null);
  assert.equal(buildGatewayWebSocketPath("/api/preview/session-1/0/socket"), null);
  assert.equal(
    buildGatewayWebSocketPath("/api/conversations/session-1/terminal/not%2Fvalid/ws"),
    null,
  );
  assert.equal(buildGatewayWebSocketPath("/api/preview/%ZZ/3000/socket"), null);
});

test("claimed preview upgrades are hidden from Next's App Router", () => {
  const request = {
    url: "/api/preview/session-1/39378/socket?reconnectionToken=token",
  };

  maskPreviewUpgradeFromNext(request);

  assert.equal(request.url, "/api/__cocola_preview_ws_passthrough__");
  assert.doesNotMatch(request.url, /^\/api\/preview\//);
});
