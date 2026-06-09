#!/usr/bin/env python3
"""M5 end-to-end acceptance: cross-language identity interop + admin control plane.

This proves the M5 claim that the Go admin-api and the Python llm-gateway speak
the SAME identity language. A token MINTED IN GO (by the admin-mint CLI, the same
codec the admin-api HTTP endpoint uses) is VERIFIED IN PYTHON by the gateway's
Verifier - byte-for-byte HS256 interop over a shared secret, no network, no
third-party JWT library on either side.

Steps:
  1. Build/run the Go admin-mint CLI (offline toolchain).
  2. Mint a token in Go with the shared secret.
  3. Verify it in Python: subject/tenant/issuer resolve correctly.
  4. Negative: a token minted with a DIFFERENT secret is rejected.
  5. Negative: a token with a foreign issuer is rejected.

Run:  apps/llm-gateway/.venv/bin/python scripts/admin-m5-e2e.py
"""
import os
import subprocess
import sys

from cocola_llm_gateway.auth import AuthConfig, JWTError, Verifier

SECRET = "m5-interop-secret"
REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ADMIN_API = os.path.join(REPO, "apps", "admin-api")

# The offline Go toolchain recipe (the local homebrew go has a broken GOROOT).
GOBIN = os.path.expanduser("~/.gvm/gos/go1.24/bin/go")
GOENV = {
    **os.environ,
    "GOWORK": "off",
    "GOPROXY": "off",
    "GOSUMDB": "off",
    "GOFLAGS": "-mod=mod",
}


def go_mint(secret, user, tenant, issuer="cocola", ttl=3600):
    """Run the Go admin-mint CLI and return the compact JWS it prints."""
    env = {**GOENV, "COCOLA_AUTH_SECRET": secret}
    out = subprocess.run(
        [GOBIN, "run", "./cmd/admin-mint",
         "-user", user, "-tenant", tenant, "-issuer", issuer, "-ttl", str(ttl)],
        cwd=ADMIN_API, env=env, capture_output=True, text=True,
    )
    if out.returncode != 0:
        raise RuntimeError("admin-mint failed: " + out.stderr.strip())
    return out.stdout.strip()


def main():
    verifier = Verifier(AuthConfig(secret=SECRET, issuer="cocola"))

    # 1+2) Mint in Go.
    tok = go_mint(SECRET, "emp-42", "team-a")
    assert tok.count(".") == 2, "not a compact JWS: " + repr(tok)
    print("go-mint    : minted token (%d chars, 3 segments)" % len(tok))

    # 3) Verify in Python - the cross-language proof.
    ident = verifier.verify(tok)
    assert ident.user_id == "emp-42", ident
    assert ident.tenant_id == "team-a", ident
    print("interop    : Go token verified in Python -> user=%s tenant=%s"
          % (ident.user_id, ident.tenant_id))

    # 4) Wrong secret -> rejected.
    bad = go_mint("a-different-secret", "emp-42", "team-a")
    try:
        verifier.verify(bad)
        print("FAIL: token signed with wrong secret was accepted")
        return 1
    except JWTError:
        print("tamper     : token signed with a different secret correctly rejected")

    # 5) Foreign issuer -> rejected.
    foreign = go_mint(SECRET, "emp-42", "team-a", issuer="evilcorp")
    try:
        verifier.verify(foreign)
        print("FAIL: foreign-issuer token was accepted")
        return 1
    except JWTError:
        print("issuer     : foreign-issuer token correctly rejected")

    print("M5 E2E OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
