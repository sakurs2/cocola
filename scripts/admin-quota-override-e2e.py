#!/usr/bin/env python3
"""M5+ end-to-end: dynamic quota override consumption across Go and Python.

This proves the second half of the dynamic-quota follow-up ("A"): the admin-api
owns the override source of truth (PUT /admin/quotas) and the Python gateway now
CONSUMES per-subject overrides on the quota path. A token minted in Go is
attributed to a subject; with no static cap the subject is unlimited; an admin
override then caps that one subject, supersedes a static default, and a 0
override marks the subject explicitly unlimited again.

Semantics under test (mirrors the Go QuotaOverride contract):
  - no override -> None -> fall back to the static policy cap
  - override N  -> N    -> per-subject cap (0 means explicitly unlimited)

Steps:
  1. Mint a token in Go (admin-mint CLI, the same codec the admin-api uses).
  2. Verify it in Python and attribute calls to the subject.
  3. No static cap, no override -> repeated calls all pass (unlimited).
  4. Admin sets override {user, emp-vip, 5} -> first call 200, next 429.
  5. Admin raises override to 100000 -> calls pass and /v1/quota reports it.
  6. Admin sets override 0 -> subject is explicitly unlimited again.

Run:  apps/llm-gateway/.venv/bin/python scripts/admin-quota-override-e2e.py
"""
import asyncio
import os
import subprocess
import sys

import httpx

from cocola_llm_gateway.auth import AuthConfig, Verifier
from cocola_llm_gateway.quota import Enforcer, MemoryOverrideStore, MemoryQuotaStore
from cocola_llm_gateway.quota.policy import QuotaPolicy
from cocola_llm_gateway.server import create_app
from tests.conftest import build_service

SECRET = "quota-override-interop-secret"
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
    tok = go_mint(SECRET, "emp-vip", "team-r")
    print("go-mint    : minted token (%d chars)" % len(tok))

    # 2) Verify in Python; attribute to the subject.
    ident = verifier.verify(tok)
    assert ident.user_id == "emp-vip", ident.user_id
    print("interop    : verified Go token -> user=%s" % ident.user_id)

    # 3) No static cap, override table present but empty -> unlimited.
    overrides = MemoryOverrideStore()
    enf = Enforcer(QuotaPolicy(user_daily_tokens=0, tenant_monthly_tokens=0), MemoryQuotaStore(), overrides=overrides)
    svc, _ = build_service(reply="well over five tokens of reply text here", enforcer=enf)
    app = create_app(svc, verifier=verifier)
    async with client(app) as c:
        for _ in range(3):
            r = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
            assert r.status_code == 200, r.text
    print("baseline   : no override -> repeated calls 200 (unlimited)")

    # 4) Admin sets a tiny per-subject override -> first call 200, next 429.
    await overrides.set("user", "emp-vip", 5)
    async with client(app) as c:
        r1 = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
        assert r1.status_code == 200, r1.text
        r2 = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
        assert r2.status_code == 429, r2.text
        assert r2.json()["error"]["scope"] == "user", r2.json()
    print("override-5 : admin caps emp-vip at 5 -> 200 then 429 rate_limit_error")

    # 5) Admin raises the cap; calls pass and /v1/quota reports the override.
    await overrides.set("user", "emp-vip", 100000)
    async with client(app) as c:
        for _ in range(3):
            r = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
            assert r.status_code == 200, r.text
        q = await c.get("/v1/quota", headers={"x-api-key": tok})
        scopes = {s["scope"]: s for s in q.json()["scopes"]}
        assert scopes["user"]["limit"] == 100000, scopes
    print("override-up: admin raises cap to 100000 -> calls pass, /v1/quota=100000")

    # 6) Override 0 marks the subject explicitly unlimited again.
    await overrides.set("user", "emp-vip", 0)
    async with client(app) as c:
        for _ in range(5):
            r = await c.post("/v1/messages", json=MSG, headers={"x-api-key": tok})
            assert r.status_code == 200, r.text
    print("override-0 : admin sets 0 -> subject explicitly unlimited again")

    print("QUOTA OVERRIDE E2E OK")
    return 0


def main():
    return asyncio.run(run())


if __name__ == "__main__":
    sys.exit(main())
