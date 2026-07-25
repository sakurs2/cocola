import assert from "node:assert/strict";
import test from "node:test";

import { wikiProxyRequestInit } from "./wiki-proxy-request.ts";

test("Wiki uploads stream the incoming request body to the Gateway", () => {
  const request = new Request("http://cocola.local/api/wiki/files", {
    method: "POST",
    body: "file bytes",
    duplex: "half",
  });
  const headers = new Headers({ "content-type": "application/octet-stream" });

  const init = wikiProxyRequestInit(request, "POST", headers);

  assert.strictEqual(init.body, request.body);
  assert.equal(init.duplex, "half");
  assert.strictEqual(init.headers, headers);
  assert.strictEqual(init.signal, request.signal);
});

test("Wiki GET and DELETE requests never forward a request body", () => {
  const request = new Request("http://cocola.local/api/wiki/files");
  const headers = new Headers();

  for (const method of ["GET", "DELETE"]) {
    const init = wikiProxyRequestInit(request, method, headers);
    assert.equal("body" in init, false);
    assert.equal("duplex" in init, false);
  }
});
