"""read_secret_env: the Vault-ready "_FILE" indirection seam (ADR-0008 §5).

Hermetic: no Vault, no network. We assert file-over-env precedence, env
fallback (no file / unreadable file), trailing-newline trimming, and that the
config readers (upstream api_key via _resolve_secret, COCOLA_AUTH_SECRET via
auth_config_from_env) actually route through the seam.
"""

from __future__ import annotations

from cocola_llm_gateway.config import (
    _resolve_secret,
    auth_config_from_env,
    read_secret_env,
)


def test_file_preferred_over_env(tmp_path, monkeypatch):
    p = tmp_path / "auth_secret"
    p.write_text("from-file\n")
    monkeypatch.setenv("COCOLA_AUTH_SECRET", "from-env")
    monkeypatch.setenv("COCOLA_AUTH_SECRET_FILE", str(p))
    # file wins; trailing newline trimmed
    assert read_secret_env("COCOLA_AUTH_SECRET") == "from-file"


def test_env_fallback_when_no_file(monkeypatch):
    monkeypatch.setenv("COCOLA_AUTH_SECRET", "from-env")
    monkeypatch.delenv("COCOLA_AUTH_SECRET_FILE", raising=False)
    assert read_secret_env("COCOLA_AUTH_SECRET") == "from-env"


def test_env_fallback_when_file_unreadable(monkeypatch):
    monkeypatch.setenv("COCOLA_AUTH_SECRET", "from-env")
    monkeypatch.setenv("COCOLA_AUTH_SECRET_FILE", "/nonexistent/cocola/secret")
    assert read_secret_env("COCOLA_AUTH_SECRET") == "from-env"


def test_empty_when_neither_set(monkeypatch):
    monkeypatch.delenv("COCOLA_AUTH_SECRET", raising=False)
    monkeypatch.delenv("COCOLA_AUTH_SECRET_FILE", raising=False)
    assert read_secret_env("COCOLA_AUTH_SECRET") == ""


def test_trims_trailing_newline_only(tmp_path, monkeypatch):
    p = tmp_path / "k"
    p.write_text("  pa ss \r\n")
    monkeypatch.setenv("COCOLA_X_FILE", str(p))
    assert read_secret_env("COCOLA_X") == "  pa ss "


def test_resolve_secret_routes_through_file(tmp_path, monkeypatch):
    p = tmp_path / "upstream_key"
    p.write_text("sk-from-file\n")
    monkeypatch.setenv("TEST_UPSTREAM_API_KEY_FILE", str(p))
    monkeypatch.delenv("TEST_UPSTREAM_API_KEY", raising=False)
    cfg = {"api_key_env": "TEST_UPSTREAM_API_KEY"}
    assert _resolve_secret(cfg, "api_key", "api_key_env") == "sk-from-file"


def test_auth_config_reads_secret_from_file(tmp_path, monkeypatch):
    p = tmp_path / "auth_secret"
    p.write_text("hs256-from-file\n")
    monkeypatch.setenv("COCOLA_AUTH_SECRET_FILE", str(p))
    monkeypatch.delenv("COCOLA_AUTH_SECRET", raising=False)
    cfg = auth_config_from_env()
    assert cfg.secret == "hs256-from-file"
