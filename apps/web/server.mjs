// cocola web custom server (Route A, code-server + Preview Proxy WebSockets).
//
// Why this file exists
// --------------------
// Next.js's App Router route handlers are request->response only: they never
// see the raw HTTP `Upgrade` event, so they cannot carry a WebSocket. cocola's
// same-origin Preview Proxy hop
//
//     browser  ->  /api/preview/{id}/{port}/{...}  (this origin)
//              ->  {GATEWAY_URL}/v1/preview/{id}/{port}/{...}
//
// works for plain HTTP via app/api/preview/.../route.ts (see lib/preview-proxy.ts),
// but the resident code-server editor is 100% WebSocket-driven and so is any
// user dev server using HMR. The gateway already reverse-proxies WS natively
// (httputil.ReverseProxy, Go 1.12+); the ONLY broken hop is browser -> this
// origin, because Next's `fetch()`-based proxy can't upgrade.
//
// So we wrap Next in a thin Node HTTP server: normal requests still go straight
// to Next's request handler, but we add an `upgrade` listener that, for
// /api/preview/* only, authenticates server-side (decode the Auth.js session
// cookie -> mint a short-lived cocola runtime token from admin-api, exactly like
// lib/server-auth.ts runtimeAuthHeaders) and raw-tunnels the socket to the
// gateway with that token injected. Everything stays same-origin; the gateway
// address + credentials never reach the browser. All other upgrades (e.g. Next
// dev HMR) are delegated to Next's own upgrade handler.

import { createServer } from "node:http";
import { request as httpRequest } from "node:http";
import { parse as parseUrl } from "node:url";
import next from "next";
import { getToken } from "next-auth/jwt";

import { maskPreviewUpgradeFromNext } from "./lib/preview-ws-routing.mjs";
import { isAllowedWebSocketOrigin, parsePublicOrigins } from "./lib/public-origins.mjs";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";
const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";
const AUTH_SECRET = process.env.AUTH_SECRET;
const RUNTIME_TOKEN_TTL_SECONDS = 600;
const PUBLIC_ORIGINS = parsePublicOrigins(process.env.COCOLA_PUBLIC_ORIGINS);

if (PUBLIC_ORIGINS.size === 0) {
  console.warn(
    "[web] COCOLA_PUBLIC_ORIGINS is empty; Preview Proxy WebSocket upgrades will fail closed",
  );
}

const dev = process.env.NODE_ENV !== "production";
const hostname = process.env.HOSTNAME || "0.0.0.0";
const port = resolvePort();

function resolvePort() {
  const argIdx = process.argv.indexOf("--port");
  if (argIdx !== -1 && process.argv[argIdx + 1]) {
    const p = Number(process.argv[argIdx + 1]);
    if (Number.isInteger(p) && p > 0) return p;
  }
  const envPort = Number(process.env.PORT);
  return Number.isInteger(envPort) && envPort > 0 ? envPort : 3000;
}

const app = next({ dev, hostname, port });
const handle = app.getRequestHandler();

// Hop-by-hop / identity headers we must not forward upstream. The minted runtime
// token replaces the browser's cookie; the gateway sets its own framing. Mirror
// lib/preview-proxy.ts's STRIP_REQUEST_HEADERS (minus the WS handshake headers,
// which MUST be forwarded verbatim).
const STRIP_UPGRADE_HEADERS = new Set(["cookie", "authorization", "x-forwarded-host"]);

// Live upgrade/tunnel sockets, tracked so graceful teardown can close them.
// server.close() only stops accepting NEW connections; an established WS tunnel
// would otherwise keep the event loop alive indefinitely.
const activeSockets = new Set();

function trackSocket(socket) {
  if (!socket) return;
  activeSockets.add(socket);
  socket.once("close", () => activeSockets.delete(socket));
}

// --- session cookie -> user email ----------------------------------------
// The Auth.js JWT is encrypted (A256CBC-HS512) with AUTH_SECRET; getToken
// decrypts it. The cookie name (and thus the derived salt) differs between
// http (authjs.session-token) and https (__Secure-authjs.session-token), so we
// try both forms.
async function resolveUserEmail(req) {
  if (!AUTH_SECRET) return null;
  const cookieHeader = req.headers.cookie ?? "";
  const hasSecure = cookieHeader.includes("__Secure-authjs.session-token");
  const attempts = hasSecure ? [true, false] : [false, true];
  for (const secureCookie of attempts) {
    try {
      const token = await getToken({
        req: { headers: req.headers },
        secret: AUTH_SECRET,
        secureCookie,
      });
      if (token?.email) return String(token.email);
    } catch {
      // try the other cookie form
    }
  }
  return null;
}

// --- mint a short-lived runtime token (mirrors server-auth.runtimeAuthHeaders)
async function mintRuntimeToken(email) {
  const headers = { "x-cocola-admin": email, "content-type": "application/json" };
  const adminKey = process.env.COCOLA_ADMIN_KEY;
  if (adminKey) headers.authorization = `Bearer ${adminKey}`;
  const res = await fetch(`${ADMIN_URL}/admin/runtime-token`, {
    method: "POST",
    cache: "no-store",
    headers,
    body: JSON.stringify({ email, ttl_seconds: RUNTIME_TOKEN_TTL_SECONDS }),
  });
  if (!res.ok) {
    throw new Error(`admin-api runtime-token ${res.status}: ${await res.text()}`);
  }
  const body = await res.json();
  if (!body?.token) throw new Error("runtime token missing from admin-api response");
  return String(body.token);
}

// --- /api/preview/{id}/{port}/{...rest} -> gateway /v1/preview/... -----------
// Returns the upstream path (with query) or null if the URL is not a valid
// preview target.
function buildPreviewPath(reqUrl) {
  const { pathname, search } = parseUrl(reqUrl);
  const m = /^\/api\/preview\/([^/]+)\/([^/]+)(?:\/(.*))?$/.exec(pathname ?? "");
  if (!m) return null;
  const sessionID = decodeURIComponent(m[1]);
  const portNum = Number(m[2]);
  if (!Number.isInteger(portNum) || portNum <= 0 || portNum > 65535) return null;
  const rest = m[3] ?? "";
  // rest is already URL-encoded in the incoming path; forward verbatim.
  return `/v1/preview/${encodeURIComponent(sessionID)}/${portNum}/${rest}${search ?? ""}`;
}

function writeUpgradeError(socket, status, message) {
  try {
    socket.write(`HTTP/1.1 ${status} ${message}\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`);
  } catch {
    // socket may already be gone
  }
  socket.destroy();
}

// Raw TCP tunnel: reissue the WS handshake to the gateway with the runtime
// token, then splice the two sockets. We do NOT re-terminate the WebSocket, so
// subprotocols and binary frames pass through untouched.
function tunnelToGateway(req, clientSocket, head, upstreamPath, runtimeToken) {
  const gw = new URL(GATEWAY_URL);

  const outHeaders = {};
  for (const [name, value] of Object.entries(req.headers)) {
    if (!STRIP_UPGRADE_HEADERS.has(name.toLowerCase())) outHeaders[name] = value;
  }
  outHeaders.host = gw.host;
  outHeaders.authorization = `Bearer ${runtimeToken}`;

  const proxyReq = httpRequest({
    protocol: gw.protocol,
    hostname: gw.hostname,
    port: gw.port || (gw.protocol === "https:" ? 443 : 80),
    method: req.method,
    path: upstreamPath,
    headers: outHeaders,
    // A WebSocket handshake must ride a brand-new socket. Node's global agent
    // defaults to keepAlive:true (Node 19+), so the socket from any earlier
    // non-101 gateway response (302/404/502 for a stale or unbound session)
    // gets returned to the pool. The next `Upgrade` then grabs that pooled
    // socket -- which the gateway has since idle-closed on its end -- and the
    // client `request` layer does NOT retry an upgrade on a stale connection:
    // our bytes vanish into a half-closed socket and the browser sees a bare
    // FIN (close code 1006). Long-lived dev servers accumulate these poisoned
    // sockets, so the tunnel works on a fresh process and then permanently
    // breaks. `agent: false` forces a fresh, unpooled connection every time.
    agent: false,
  });

  // Whether the request reached a terminal handler (upgrade/response/error). If
  // proxyReq closes without any of them firing, the WS handshake was silently
  // dropped -- the browser then sees a bare FIN (close code 1006) with no log.
  // That exact failure mode bit us on a long-lived dev server, so make it
  // observable instead of letting the socket die quietly.
  let settled = false;

  proxyReq.on("error", (err) => {
    settled = true;
    console.error("[web] preview WS gateway error:", err.message);
    clientSocket.destroy();
  });

  proxyReq.on("close", () => {
    if (settled) return;
    settled = true;
    console.error(
      "[web] preview WS tunnel closed before upgrade/response " +
        `(silent FIN) path=${upstreamPath}`,
    );
    clientSocket.destroy();
  });

  proxyReq.on("upgrade", (proxyRes, proxySocket, proxyHead) => {
    settled = true;
    trackSocket(proxySocket);
    proxySocket.on("error", () => clientSocket.destroy());
    clientSocket.on("error", () => proxySocket.destroy());
    proxySocket.setTimeout(0);
    proxySocket.setNoDelay(true);
    proxySocket.setKeepAlive(true, 0);

    if (proxyHead && proxyHead.length) proxySocket.unshift(proxyHead);

    // Relay the gateway's 101 handshake verbatim to the browser.
    let handshake = "HTTP/1.1 101 Switching Protocols\r\n";
    for (let i = 0; i < proxyRes.rawHeaders.length; i += 2) {
      handshake += `${proxyRes.rawHeaders[i]}: ${proxyRes.rawHeaders[i + 1]}\r\n`;
    }
    handshake += "\r\n";
    clientSocket.write(handshake);

    proxySocket.pipe(clientSocket).pipe(proxySocket);
  });

  // Non-101 responses (e.g. 401/502 from the gateway) never fire `upgrade`.
  proxyReq.on("response", (proxyRes) => {
    settled = true;
    writeUpgradeError(
      clientSocket,
      proxyRes.statusCode || 502,
      proxyRes.statusMessage || "Bad Gateway",
    );
  });

  if (head && head.length) clientSocket.unshift(head);
  proxyReq.end();
}

app
  .prepare()
  .then(() => {
    const server = createServer((req, res) => {
      handle(req, res).catch((err) => {
        console.error("[web] request handler error:", err);
        res.statusCode = 500;
        res.end("internal server error");
      });
    });

    server.on("upgrade", (req, socket, head) => {
      trackSocket(socket);
      const upstreamPath = buildPreviewPath(req.url ?? "");
      if (!upstreamPath) {
        // Next lazily appends its own Upgrade listener after the first HTTP
        // request; it remains the sole owner of dev HMR and other upgrades.
        return;
      }

      // Next's listener sees this same EventEmitter event after ours. The real
      // URL matches the App Router's HTTP preview route, so Next would call
      // socket.end() while our async authentication is still running. Hide the
      // claimed upgrade behind an intentionally nonexistent API path; the real
      // Gateway path was already captured above.
      maskPreviewUpgradeFromNext(req);
      socket.pause();

      (async () => {
        // The browser-facing cookie boundary owns CSWSH protection. Validate the
        // full external Origin before reading the Auth.js cookie or minting a
        // runtime token; code-server independently checks the same explicit host
        // allowlist inside the sandbox. Preserve the accepted Origin downstream
        // because arbitrary Preview dev servers may rely on it.
        if (!isAllowedWebSocketOrigin(req.headers.origin, PUBLIC_ORIGINS)) {
          writeUpgradeError(socket, 403, "Forbidden");
          return;
        }
        const email = await resolveUserEmail(req);
        if (!email) {
          writeUpgradeError(socket, 401, "Unauthorized");
          return;
        }
        let runtimeToken;
        try {
          runtimeToken = await mintRuntimeToken(email);
        } catch (err) {
          console.error("[web] preview WS auth failed:", err.message);
          writeUpgradeError(socket, 502, "Bad Gateway");
          return;
        }
        tunnelToGateway(req, socket, head, upstreamPath, runtimeToken);
      })().catch((err) => {
        console.error("[web] preview WS upgrade error:", err);
        socket.destroy();
      });
    });

    server.listen(port, hostname, () => {
      console.log(`[web] ready on http://${hostname}:${port} (dev=${dev})`);
    });

    // --- graceful teardown -----------------------------------------------------
    // On SIGTERM/SIGINT: stop accepting new connections, proactively tear down
    // live WS tunnels (they'd otherwise keep the loop alive), close Next, quit.
    let closing = false;
    const gracefulClose = (signal) => {
      if (closing) return;
      closing = true;
      console.log(`[web] ${signal} received, closing gracefully...`);

      const forceQuit = setTimeout(() => {
        console.error("[web] graceful close timed out, forcing quit");
        process.exit(1);
      }, 10000);
      forceQuit.unref();

      server.close((err) => {
        if (err) console.error("[web] server.close error:", err.message);
        Promise.resolve(app.close?.())
          .catch((e) => console.error("[web] next close error:", e?.message))
          .finally(() => {
            clearTimeout(forceQuit);
            console.log("[web] graceful close complete");
            process.exit(0);
          });
      });

      // Destroy in-flight WS/upgrade sockets so keep-alive connections don't
      // block server.close() from ever completing.
      for (const socket of activeSockets) socket.destroy();
      activeSockets.clear();
    };

    for (const signal of ["SIGTERM", "SIGINT"]) {
      process.on(signal, () => gracefulClose(signal));
    }
  })
  .catch((err) => {
    console.error("[web] failed to start:", err);
    process.exit(1);
  });
