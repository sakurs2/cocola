import assert from "node:assert/strict";
import test from "node:test";

import { maskPreviewUpgradeFromNext } from "./preview-ws-routing.mjs";

test("claimed preview upgrades are hidden from Next's App Router", () => {
  const request = {
    url: "/api/preview/session-1/39378/socket?reconnectionToken=token",
  };

  maskPreviewUpgradeFromNext(request);

  assert.equal(request.url, "/api/__cocola_preview_ws_passthrough__");
  assert.doesNotMatch(request.url, /^\/api\/preview\//);
});
