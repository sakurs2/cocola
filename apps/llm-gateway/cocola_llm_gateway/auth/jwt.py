"""Minimal, dependency-free HS256 JWT (sign + verify).

Why hand-rolled instead of PyJWT? The gateway already runs hermetic, stdlib-only
tests (network forbidden, no bound port). A signed token only needs HMAC-SHA256
over base64url(header).base64url(payload) — that is ~40 lines of stdlib and adds
no third-party surface to audit. If we later need RS256/JWKS (multi-issuer), this
is the single module to swap; nothing else imports `hmac`/`hashlib` for auth.

Tokens are compact JWS (JSON Web Signature), the standard three-segment form:
    base64url(header) "." base64url(payload) "." base64url(signature)

This module is intentionally tiny and pure: no I/O, no clock injection beyond an
optional `now` for tests. Claims we use:
    sub  -> user_id      (employee id / username)
    ten  -> tenant_id    (team/department; "" means the default tenant)
    iat  -> issued-at    (unix seconds)
    exp  -> expiry       (unix seconds; optional — omit for non-expiring tokens)
    iss  -> issuer       (defaults to "cocola")
    jti  -> token id     (per-token id; the revocation denylist key)
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import time
from dataclasses import dataclass

_ALG = "HS256"


class JWTError(Exception):
    """Raised on any malformed/invalid/expired token. Never leaks the secret."""


def _b64url_encode(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def _b64url_decode(seg: str) -> bytes:
    # JWT strips '=' padding; restore it before decoding.
    pad = "=" * (-len(seg) % 4)
    try:
        return base64.urlsafe_b64decode(seg + pad)
    except Exception as e:  # noqa: BLE001 - normalize to JWTError
        raise JWTError("malformed base64url segment") from e


def _sign(signing_input: bytes, secret: str) -> str:
    sig = hmac.new(secret.encode("utf-8"), signing_input, hashlib.sha256).digest()
    return _b64url_encode(sig)


def encode(payload: dict, secret: str) -> str:
    """Sign a claims dict into a compact JWS string."""
    if not secret:
        raise JWTError("signing secret must not be empty")
    header = {"alg": _ALG, "typ": "JWT"}
    h = _b64url_encode(json.dumps(header, separators=(",", ":")).encode("utf-8"))
    p = _b64url_encode(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
    signing_input = f"{h}.{p}".encode("ascii")
    s = _sign(signing_input, secret)
    return f"{h}.{p}.{s}"


def decode(token: str, secret: str, *, now: float | None = None) -> dict:
    """Verify signature + expiry and return the claims dict.

    Raises JWTError on any problem (bad shape, wrong alg, bad signature, expired).
    Signature comparison is constant-time.
    """
    if not secret:
        raise JWTError("verifying secret must not be empty")
    parts = token.split(".")
    if len(parts) != 3:
        raise JWTError("token must have three segments")
    h_seg, p_seg, sig_seg = parts

    try:
        header = json.loads(_b64url_decode(h_seg) or b"{}")
    except Exception as e:  # noqa: BLE001
        raise JWTError("header is not valid JSON") from e
    if not isinstance(header, dict) or header.get("alg") != _ALG:
        raise JWTError(f"unsupported alg {header.get('alg')!r}; expected {_ALG}")

    signing_input = f"{h_seg}.{p_seg}".encode("ascii")
    expected = _sign(signing_input, secret)
    if not hmac.compare_digest(expected, sig_seg):
        raise JWTError("signature mismatch")

    try:
        claims = json.loads(_b64url_decode(p_seg))
    except Exception as e:  # noqa: BLE001
        raise JWTError("payload is not valid JSON") from e
    if not isinstance(claims, dict):
        raise JWTError("payload must be a JSON object")

    exp = claims.get("exp")
    if exp is not None:
        t = time.time() if now is None else now
        if t >= float(exp):
            raise JWTError("token expired")
    return claims


@dataclass(frozen=True, slots=True)
class Identity:
    """Resolved caller identity extracted from a verified token.

    `user_id` is the billing + quota subject. `tenant_id` groups users for an
    optional second quota layer; "" means the default/unset tenant.
    """

    user_id: str
    tenant_id: str = ""
    issued_at: float = 0.0
    expires_at: float | None = None
    # token_id is the `jti` claim: the per-token id used to consult the
    # revocation denylist. "" when the token predates jti or auth is disabled.
    token_id: str = ""

    @property
    def is_authenticated(self) -> bool:
        return bool(self.user_id)
