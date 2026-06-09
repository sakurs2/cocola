"""CLI to mint a cocola token for an employee.

The minted token IS the value you set as ANTHROPIC_API_KEY for the Claude Agent
SDK (or pass as `Authorization: Bearer` / `x-api-key` to /v1/messages). It must
be signed with the same COCOLA_AUTH_SECRET the gateway verifies with.

Usage:
    COCOLA_AUTH_SECRET=... python -m cocola_llm_gateway.issue_token \
        --user emp-12345 [--tenant team-platform] [--ttl-days 30] [--quiet]

Exit codes: 0 ok, 2 usage/secret error.

This is the M4 issuance path. A future admin-api HTTP endpoint (Go) will wrap the
same Issuer for self-service token minting (see roadmap follow-ups).
"""
from __future__ import annotations

import argparse
import sys

from cocola_llm_gateway.auth import Issuer, JWTError
from cocola_llm_gateway.config import auth_config_from_env


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="cocola-issue-token", description=__doc__)
    parser.add_argument("--user", required=True, help="employee id / username (the `sub` claim)")
    parser.add_argument("--tenant", default="", help="team/department id (the `ten` claim)")
    parser.add_argument(
        "--ttl-days",
        type=int,
        default=None,
        help="token lifetime in days; 0 = non-expiring; omit to use COCOLA_AUTH_TOKEN_TTL_SECS",
    )
    parser.add_argument(
        "--quiet", action="store_true", help="print only the token (for piping into env)"
    )
    args = parser.parse_args(argv)

    cfg = auth_config_from_env()
    if not cfg.secret:
        print(
            "error: COCOLA_AUTH_SECRET is not set; cannot sign a token.",
            file=sys.stderr,
        )
        return 2

    ttl_s = None if args.ttl_days is None else max(0, args.ttl_days) * 24 * 3600
    try:
        token = Issuer(cfg).issue(args.user, tenant_id=args.tenant, ttl_s=ttl_s)
    except JWTError as e:
        print(f"error: {e}", file=sys.stderr)
        return 2

    if args.quiet:
        print(token)
    else:
        print(f"user:   {args.user}")
        print(f"tenant: {args.tenant or '(default)'}")
        if ttl_s == 0:
            print("expiry: never")
        elif ttl_s is None:
            print(f"expiry: in {cfg.default_ttl_s // 86400} day(s) (default)")
        else:
            print(f"expiry: in {ttl_s // 86400} day(s)")
        print()
        print("ANTHROPIC_API_KEY (set this for the Claude Agent SDK):")
        print(token)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
