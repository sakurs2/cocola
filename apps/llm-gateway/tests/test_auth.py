"""Auth: JWT encode/decode + Issuer/Verifier semantics."""
import time

import pytest

from cocola_llm_gateway.auth import AuthConfig, Issuer, JWTError, Verifier
from cocola_llm_gateway.auth import jwt as _jwt


def test_jwt_roundtrip():
    claims = {"sub": "emp-1", "ten": "team-a", "iat": 1000}
    tok = _jwt.encode(claims, "s")
    assert tok.count(".") == 2
    back = _jwt.decode(tok, "s")
    assert back["sub"] == "emp-1" and back["ten"] == "team-a"


def test_jwt_wrong_secret_rejected():
    tok = _jwt.encode({"sub": "x"}, "right")
    with pytest.raises(JWTError):
        _jwt.decode(tok, "wrong")


def test_jwt_tamper_rejected():
    tok = _jwt.encode({"sub": "x"}, "s")
    h, p, sig = tok.split(".")
    # flip the last char of the payload segment
    bad = f"{h}.{p[:-1]}{'A' if p[-1] != 'A' else 'B'}.{sig}"
    with pytest.raises(JWTError):
        _jwt.decode(bad, "s")


def test_jwt_expiry_enforced():
    tok = _jwt.encode({"sub": "x", "exp": 100}, "s")
    with pytest.raises(JWTError):
        _jwt.decode(tok, "s", now=101)
    assert _jwt.decode(tok, "s", now=99)["sub"] == "x"


def test_jwt_bad_shape_rejected():
    with pytest.raises(JWTError):
        _jwt.decode("not-a-jwt", "s")


def test_issuer_verifier_roundtrip():
    iss, vrf = _pair()
    tok = iss.issue("emp-7", tenant_id="team-x", ttl_s=3600)
    ident = vrf.verify(tok)
    assert ident.user_id == "emp-7"
    assert ident.tenant_id == "team-x"
    assert ident.is_authenticated
    assert ident.expires_at is not None


def test_verify_strips_bearer_prefix():
    iss, vrf = _pair()
    tok = iss.issue("emp-7")
    assert vrf.verify(f"Bearer {tok}").user_id == "emp-7"
    assert vrf.verify(f"bearer {tok}").user_id == "emp-7"


def test_verify_missing_token_rejected_when_enabled():
    _, vrf = _pair()
    with pytest.raises(JWTError):
        vrf.verify(None)
    with pytest.raises(JWTError):
        vrf.verify("")


def test_dev_anonymous_allows_blank_token():
    cfg = AuthConfig(secret="s", dev_allow_anonymous=True)
    vrf = Verifier(cfg)
    assert vrf.verify(None).user_id == "dev-user"
    # but a present-yet-invalid token still fails
    with pytest.raises(JWTError):
        vrf.verify("garbage.token.here")


def test_auth_disabled_returns_dev_identity():
    vrf = Verifier(AuthConfig())  # no secret => disabled
    assert not vrf.config.enabled
    assert vrf.verify(None).user_id == "dev-user"
    assert vrf.verify("anything").user_id == "dev-user"


def test_issuer_requires_secret():
    with pytest.raises(JWTError):
        Issuer(AuthConfig())


def test_issuer_requires_user():
    iss, _ = _pair()
    with pytest.raises(JWTError):
        iss.issue("")


def test_issuer_mismatch_rejected():
    iss = Issuer(AuthConfig(secret="s", issuer="cocola"))
    vrf = Verifier(AuthConfig(secret="s", issuer="other"))
    tok = iss.issue("emp-1")
    with pytest.raises(JWTError):
        vrf.verify(tok)


def test_missing_issuer_claim_rejected_when_issuer_configured():
    # A validly-signed token with NO `iss` claim must be rejected when the
    # verifier is configured with an issuer (no bare-token exemption).
    vrf = Verifier(AuthConfig(secret="s", issuer="cocola"))
    tok = _jwt.encode({"sub": "emp-1", "iat": 1000}, "s")  # note: no `iss`
    with pytest.raises(JWTError):
        vrf.verify(tok)


def test_no_issuer_configured_accepts_any_iss():
    # When the verifier has no issuer configured, the iss check is skipped.
    vrf = Verifier(AuthConfig(secret="s", issuer=""))
    tok = _jwt.encode({"sub": "emp-1", "iss": "whoever"}, "s")
    assert vrf.verify(tok).user_id == "emp-1"


def test_non_expiring_token():
    iss, vrf = _pair()
    tok = iss.issue("emp-1", ttl_s=0)
    ident = vrf.verify(tok, now=time.time() + 10**9)
    assert ident.user_id == "emp-1"
    assert ident.expires_at is None


def _pair(secret="test-secret"):
    cfg = AuthConfig(secret=secret, issuer="cocola", default_ttl_s=3600)
    return Issuer(cfg), Verifier(cfg)
