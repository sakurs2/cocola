"""Token issuer + verifier: turn employees into signed tokens and back.

This is the auth seam the rest of the gateway depends on. The HTTP layer calls
`Verifier.verify(raw_token)` to turn the bearer credential the Claude Agent SDK
sends (as ANTHROPIC_API_KEY) into an `Identity`; ops calls `Issuer.issue(...)`
(via the `issue-token` CLI) to mint a token for an employee.

Design notes:
- The token *is* the API key. cocola injects it into the SDK subprocess env as
  ANTHROPIC_API_KEY (see ClaudeAgentSDKProvider). There is no separate key store;
  identity, expiry, and tenant live inside the signed JWT.
- Verification is offline (shared HS256 secret) — no network call per request,
  which keeps the hot path fast and the gateway horizontally scalable.
- A `dev_allow_anonymous` escape hatch lets local/fake boots work without minting
  a token (mirrors the old mock-header behavior), but it is OFF unless explicitly
  enabled, so production never silently accepts anonymous callers.
"""

from __future__ import annotations

import secrets
import time
from dataclasses import dataclass

from cocola_common import get_logger

from cocola_llm_gateway.auth import jwt as _jwt
from cocola_llm_gateway.auth.jwt import Identity, JWTError

log = get_logger("cocola.llm-gateway.auth")

# A stable, obviously-fake identity used only when dev_allow_anonymous is on.
DEV_IDENTITY = Identity(user_id="dev-user", tenant_id="dev-tenant")


@dataclass(frozen=True)
class AuthConfig:
    secret: str = ""
    issuer: str = "cocola"
    # Default token lifetime when issuing (seconds). 0 => non-expiring.
    default_ttl_s: int = 30 * 24 * 3600
    # If True and no/blank token is presented, fall back to DEV_IDENTITY instead
    # of rejecting. Never enable in production.
    dev_allow_anonymous: bool = False

    @property
    def enabled(self) -> bool:
        """Auth is enforced when a secret is configured."""
        return bool(self.secret)


class Issuer:
    """Mints signed tokens for employees. Used by the issue-token CLI/admin path."""

    def __init__(self, config: AuthConfig):
        if not config.secret:
            raise JWTError("cannot issue tokens without a signing secret")
        self._cfg = config

    def issue(
        self,
        user_id: str,
        *,
        tenant_id: str = "",
        ttl_s: int | None = None,
        now: float | None = None,
    ) -> str:
        if not user_id:
            raise JWTError("user_id is required to issue a token")
        iat = int(time.time() if now is None else now)
        ttl = self._cfg.default_ttl_s if ttl_s is None else ttl_s
        claims: dict = {
            "sub": user_id,
            "ten": tenant_id,
            "iat": iat,
            "iss": self._cfg.issuer,
            "jti": _new_jti(),
        }
        if ttl and ttl > 0:
            claims["exp"] = iat + ttl
        return _jwt.encode(claims, self._cfg.secret)


class Verifier:
    """Verifies bearer tokens into Identity objects. Used by the HTTP layer."""

    def __init__(self, config: AuthConfig):
        self._cfg = config

    @property
    def config(self) -> AuthConfig:
        return self._cfg

    def verify(self, raw_token: str | None, *, now: float | None = None) -> Identity:
        """Resolve a raw bearer credential to an Identity.

        Raises JWTError if auth is enabled and the token is missing/invalid,
        UNLESS dev_allow_anonymous is set (then a blank token yields DEV_IDENTITY).
        When auth is disabled entirely (no secret), always returns DEV_IDENTITY.
        """
        token = _strip_bearer(raw_token)

        if not self._cfg.enabled:
            # No secret configured: auth disabled, everyone is the dev identity.
            return DEV_IDENTITY

        if not token:
            if self._cfg.dev_allow_anonymous:
                return DEV_IDENTITY
            raise JWTError("missing bearer token")

        claims = _jwt.decode(token, self._cfg.secret, now=now)
        # If an issuer is configured, the token MUST carry a matching `iss`.
        # A missing `iss` is rejected too: no bare-/foreign-token exemption.
        if self._cfg.issuer and claims.get("iss") != self._cfg.issuer:
            raise JWTError("issuer mismatch")
        sub = claims.get("sub") or ""
        if not sub:
            raise JWTError("token has no subject (sub)")
        return Identity(
            user_id=str(sub),
            tenant_id=str(claims.get("ten") or ""),
            issued_at=float(claims.get("iat") or 0.0),
            expires_at=(
                float(claims["exp"]) if claims.get("exp") is not None else None
            ),
            token_id=str(claims.get("jti") or ""),
        )


def _strip_bearer(raw: str | None) -> str:
    if not raw:
        return ""
    s = raw.strip()
    # Authorization: Bearer <tok>  /  x-api-key: <tok>  /  ANTHROPIC_API_KEY=<tok>
    if s.lower().startswith("bearer "):
        return s[7:].strip()
    return s


def _new_jti() -> str:
    """Mint a random token id for the `jti` claim.

    12 random bytes rendered as 24 hex chars, byte-compatible with the Go
    issuer's newJTI so a token minted by either side carries the same shape.
    This id is also persisted as the admin-api TokenRecord.ID (the denylist key).
    """
    return secrets.token_hex(12)
