#!/usr/bin/env python3
"""M5+ end-to-end: token revocation closed loop across Go and Python.

This proves the denylist closed loop ("A"): a token MINTED IN GO carries a `jti`
(JWT ID) that equals the admin-api TokenRecord.ID; the Python gateway extracts
that `jti` on verification and consults a revocation denylist on the hot path, so
an admin REVOKE makes a still-unexpired token stop working fleet-wide.

Steps:
  1. Mint a token in Go (admin-mint CLI, same codec the admin-api uses).
  2. Verify it in Python and extract the `jti` -> token_id (the denylist key).
  3. Build the gateway app with a denylist that does NOT contain the jti:
     POST /v1/messages succeeds (200).
  4. "Admin revokes": add the jti to the denylist (this is what the admin-api
     does on DELETE /admin/tokens/{id}). Re-issue the SAME request:
     the gateway now rejects with 401 authentication_error "token revoked",
     even though the token is still within its `exp`.
  5. Confirm /v1/usage and /v1/quota are gated the same way.

Run:  apps/llm-gateway/.venv/bin/python scripts/admin-revocation-e2e.py
"""
import asyncio
import os
import subprocess
import sys

import httpx

from cocola_llm_gateway.auth import AuthConfig, MemoryRevocationStore, Verifier
from cocola_llm_gateway.server import create_app
from tests.conftest import build_service

SECRET = "revocation-interop-secret"
REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ADMIN_API = os.path.join(REPO, "apps", "admin-api")

GOBIN = os.path.expanduser("~/.gvm/gos/go1.24/bin/go")
GOENV = {
    **os.environ,
    "GOWORK": "off",
    "GOPROXY": "off",
    "GOSUMDB": "off",
    "GOFLAGS": "-mod=mod",
}

MSG = {
    "model": "default",
    "max_tokens": 32,
    "stream": False,
    "messages": [{"role": "user", "content": "hi"}],
}


def go_mint(secret, user, tenant, issuer="cocola", ttl=3600):
    env = {**GOENV, "COCOLA_AUTH_SECRET": secret}
    out = subprocess.run(
        [GOBIN, "run", "./cmd/admin-mint",
         "-user", user, "-tenant", tenant, "-issuer", issuer, "-ttl", str(ttl)],
        cwd=ADMIN_API, env=env, capture_output=True, text=True,
    )
    if out.returncode != 0:
        raise RuntimeError("admin-mint failed: " + out.stderr.strip())
    return out.stdout.strip()


def client(app):
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://t")


async def run():
    verifier = Verifier(AuthConfig(secret=SECRET, issuer="cocola"))

    # 1) Mint in Go.
    tok = go_mint(SECRET, "emp-77", "team-r")
    print("go-mint    : minted token (%d chars)" % len(tok))

    # 2) Verify in Python; the Go-minted token must carry a jti.
    ident = verifier.verify(tok)
    jti = ident.token_id
    assert jti, "Go-minted token has no jti -> denylist key missing"
    print("interop    : verified Go token -> user=%s jti=%s" % (ident.user_id, jti))

    # 3) Denylist present but EMPTY -> request passes.
    svc, _ = build_service(reply="ok")
    deny = MemoryRevocationStore()
    app = create_app(svc, verifier=verifier, revocation=deny)
    async with client(app) as c:
        r = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
        assert r.status_code == 200, r.text
    print("pre-revoke : /v1/messages -> 200 (token valid, not revoked)")

    # 4) Admin revokes (adds the jti to the denylist) -> request now 401.
    await deny.revoke(jti)
    async with client(app) as c:
        r = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
        assert r.status_code == 401, r.text
        body = r.json()
        assert body["error"]["type"] == "authentication_error", body
        assert "revoked" in body["error"]["message"], body
    print("revoke     : /v1/messages -> 401 authentication_error 'token revoked'")

    # 5) The gate also covers the billing/quota read surfaces.
    async with client(app) as c:
        u = await c.get("/v1/usage", headers={"x-api-key": tok})
        q = await c.get("/v1/quota", headers={"x-api-key": tok})
        assert u.status_code == 401 and q.status_code == 401, (u.status_code, q.status_code)
    print("surfaces   : /v1/usage + /v1/quota also -> 401 for the revoked token")

    print("REVOCATION E2E OK")
    return 0


def main():
    return asyncio.run(run())


if __name__ == "__main__":
    sys.exit(main())
