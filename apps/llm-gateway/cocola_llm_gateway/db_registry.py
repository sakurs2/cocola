"""Dynamic model registry backed by admin-managed Postgres rows."""

from __future__ import annotations

import asyncio
import base64
import hashlib
import os
import time
from typing import Any

from cocola_common import CocolaError, ErrorCode, get_logger
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from psycopg_pool import AsyncConnectionPool

from cocola_llm_gateway.config import _build_from_dict
from cocola_llm_gateway.registry import Registry
from cocola_llm_gateway.service import RegistrySource

log = get_logger("cocola.llm-gateway.db-registry")


class PostgresRegistrySource:
    def __init__(self, dsn: str, fallback: Registry, *, secret: str, ttl_s: float = 2.0):
        self._dsn = dsn
        self._fallback = fallback
        self._secret = secret.strip()
        self._ttl_s = ttl_s
        self._pool = AsyncConnectionPool(dsn, open=False, kwargs={"autocommit": True})
        self._open_lock = asyncio.Lock()
        self._refresh_lock = asyncio.Lock()
        self._opened = False
        self._cached: Registry | None = None
        self._cached_at = 0.0

    async def _ready(self) -> None:
        if self._opened:
            return
        async with self._open_lock:
            if not self._opened:
                await self._pool.open()
                self._opened = True

    async def get_registry(self) -> Registry:
        now = time.monotonic()
        if self._cached is not None and now - self._cached_at < self._ttl_s:
            return self._cached
        async with self._refresh_lock:
            now = time.monotonic()
            if self._cached is not None and now - self._cached_at < self._ttl_s:
                return self._cached
            try:
                reg = await self._load()
            except Exception as exc:  # noqa: BLE001 - fallback keeps gateway usable
                log.warning("db model registry load failed", error=repr(exc))
                reg = self._cached or self._fallback
            self._cached = reg
            self._cached_at = now
            return reg

    async def _load(self) -> Registry:
        await self._ready()
        query = """
            SELECT
                p.id, p.type, p.base_url, p.api_key_ciphertext,
                r.alias, r.real_model, r.runtime, r.label, r.icon_type, r.icon_slug,
                r.icon_url, r.is_default
            FROM llm_model_routes r
            JOIN llm_providers p ON p.id = r.provider_id
            WHERE p.enabled = TRUE AND r.enabled = TRUE
            ORDER BY r.is_default DESC, r.sort_order, r.alias
        """
        async with self._pool.connection() as conn:
            cur = await conn.execute(query)
            rows = await cur.fetchall()
        if not rows:
            return self._fallback

        providers: dict[str, dict[str, Any]] = {}
        routes: dict[str, dict[str, Any]] = {}
        default_alias = ""
        for row in rows:
            (
                provider_id,
                provider_type,
                base_url,
                api_key_ciphertext,
                alias,
                real_model,
                runtime,
                label,
                icon_type,
                icon_slug,
                icon_url,
                is_default,
            ) = row
            if provider_id not in providers:
                providers[provider_id] = {
                    "type": provider_type,
                    "base_url": base_url,
                    "api_key": decrypt_secret(self._secret, api_key_ciphertext)
                    if api_key_ciphertext
                    else "",
                }
            icon: dict[str, str] = {"type": icon_type or "simple-icons"}
            if icon["type"] == "image":
                icon["src"] = icon_url or ""
            else:
                icon["slug"] = icon_slug or ""
            routes[alias] = {
                "provider": provider_id,
                "real_model": real_model,
                "runtime": runtime or "claude-code",
                "label": label or alias,
                "icon": icon,
                "enabled": True,
                "visible": True,
            }
            if is_default and not default_alias:
                default_alias = alias
        return _build_from_dict(
            {
                "default_alias": default_alias,
                "providers": providers,
                "routes": routes,
            }
        )

    async def aclose(self) -> None:
        if self._cached is not None and self._cached is not self._fallback:
            await self._cached.aclose()
            self._cached = None
        await self._fallback.aclose()
        if self._opened:
            await self._pool.close()
            self._opened = False


def decrypt_secret(secret: str, ciphertext: str) -> str:
    if not ciphertext:
        return ""
    if not secret.strip():
        raise CocolaError(ErrorCode.INVALID_ARGUMENT, "COCOLA_MODEL_SECRET_KEY is required")
    if not ciphertext.startswith("v1:"):
        raise CocolaError(ErrorCode.INVALID_ARGUMENT, "unsupported model secret ciphertext")
    raw = base64.b64decode(ciphertext[3:])
    if len(raw) <= 12:
        raise CocolaError(ErrorCode.INVALID_ARGUMENT, "invalid model secret ciphertext")
    key = hashlib.sha256(secret.encode("utf-8")).digest()
    return AESGCM(key).decrypt(raw[:12], raw[12:], None).decode("utf-8")


def registry_source_from_env(fallback: Registry) -> RegistrySource:
    dsn = os.getenv("COCOLA_PG_DSN", "").strip()
    if not dsn:
        return _Static(fallback)
    ttl_s = float(os.getenv("COCOLA_LLM_REGISTRY_CACHE_TTL_SECS", "2"))
    return PostgresRegistrySource(
        dsn,
        fallback,
        secret=os.getenv("COCOLA_MODEL_SECRET_KEY", ""),
        ttl_s=ttl_s,
    )


class _Static:
    def __init__(self, registry: Registry):
        self._registry = registry

    async def get_registry(self) -> Registry:
        return self._registry

    async def aclose(self) -> None:
        await self._registry.aclose()
