import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { AUTH_SESSION_VERSION, authTokenUserID } from "./auth-session-policy.mjs";

const authSource = await readFile(new URL("../auth.ts", import.meta.url), "utf8");
const accountProxySource = await readFile(new URL("./account-proxy.ts", import.meta.url), "utf8");
const serverSource = await readFile(new URL("../server.mjs", import.meta.url), "utf8");

test("auth policy invalidates legacy sessions and accepts only versioned user ids", () => {
  assert.equal(authTokenUserID({ id: "user-1" }), null);
  assert.equal(authTokenUserID({ id: "user-1", authVersion: AUTH_SESSION_VERSION - 1 }), null);
  assert.equal(authTokenUserID({ id: " user-1 ", authVersion: AUTH_SESSION_VERSION }), "user-1");
});

test("Auth.js trusts the browser-facing host in self-hosted deployments", () => {
  assert.match(authSource, /NextAuth\(\{\s*trustHost: true,/);
});

test("Auth.js update refreshes from the trusted token id without reading client user fields", () => {
  const jwtCallback = authSource.slice(
    authSource.indexOf("async jwt("),
    authSource.indexOf("    session(", authSource.indexOf("async jwt(")),
  );
  assert.doesNotMatch(jwtCallback, /session\.?user/);
  assert.match(jwtCallback, /refreshAuthenticatedUser\(userID\)/);
  assert.match(authSource, /admin\/users\/\$\{encodeURIComponent\(userID\)\}/);
  assert.match(accountProxySource, /unstable_update\(\{\}\)/);
});

test("workspace WebSocket validates the session version and reloads the current account", () => {
  assert.match(serverSource, /authTokenUserID\(token\)/);
  assert.match(serverSource, /resolveCurrentAccount\(userID\)/);
  assert.match(serverSource, /mintRuntimeToken\(account\.email\)/);
  assert.doesNotMatch(serverSource, /token\?\.email/);
});
