import assert from "node:assert/strict";
import test from "node:test";

import { isAllowedWebSocketOrigin, parsePublicOrigins } from "./public-origins.mjs";

test("parsePublicOrigins normalizes and deduplicates explicit origins", () => {
  assert.deepEqual(
    [
      ...parsePublicOrigins(
        "HTTPS://Cocola.Example.com:443/, http://127.0.0.1:3000,https://cocola.example.com",
      ),
    ],
    ["https://cocola.example.com", "http://127.0.0.1:3000"],
  );
});

test("parsePublicOrigins rejects wildcards and non-origin URLs", () => {
  for (const value of [
    "*",
    "https://*.example.com",
    "ftp://example.com",
    "https://user@example.com",
    "https://example.com/path",
    "https://example.com?query=1",
  ]) {
    assert.throws(() => parsePublicOrigins(value), /origin/i, value);
  }
});

test("isAllowedWebSocketOrigin is fail-closed and exact", () => {
  const allowed = parsePublicOrigins("https://cocola.example.com,http://localhost:3000");

  assert.equal(isAllowedWebSocketOrigin("https://cocola.example.com", allowed), true);
  assert.equal(isAllowedWebSocketOrigin("https://cocola.example.com:443", allowed), true);
  assert.equal(isAllowedWebSocketOrigin("http://localhost:3000", allowed), true);
  assert.equal(isAllowedWebSocketOrigin("https://evil.example.com", allowed), false);
  assert.equal(isAllowedWebSocketOrigin("https://sub.cocola.example.com", allowed), false);
  assert.equal(isAllowedWebSocketOrigin("https://cocola.example.com/path", allowed), false);
  assert.equal(isAllowedWebSocketOrigin("https://user@cocola.example.com", allowed), false);
  assert.equal(isAllowedWebSocketOrigin(undefined, allowed), false);
  assert.equal(isAllowedWebSocketOrigin("https://cocola.example.com", new Set()), false);
});
