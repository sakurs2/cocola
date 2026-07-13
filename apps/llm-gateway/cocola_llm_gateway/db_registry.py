"""Dynamic model registry backed by admin-managed Postgres rows."""

from __future__ import annotations

import asyncio
import base64
import hashlib
import time
from typing import Any

from cocola_common import CocolaError, ErrorCode, get_logger
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from psycopg_pool import AsyncConnectionPool

from cocola_llm_gateway.config import _build_from_dict
from cocola_llm_gateway.registry import Registry

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
        self._fingerprint = ""
        self._references: dict[Registry, int] = {}
        self._retired: set[Registry] = set()

    async def _ready(self) -> None:
        if self._opened:
            return
        async with self._open_lock:
            if not self._opened:
                await self._pool.open()
                self._opened = True

    async def acquire_registry(self) -> Registry:
        close_after_swap: Registry | None = None
        now = time.monotonic()
        async with self._refresh_lock:
            now = time.monotonic()
            if self._cached is None or now - self._cached_at >= self._ttl_s:
                close_after_swap = await self._refresh(now)
            registry = self._cached or self._fallback
            self._references[registry] = self._references.get(registry, 0) + 1
        if close_after_swap is not None:
            await _close_registry(close_after_swap)
        return registry

    async def release_registry(self, registry: Registry) -> None:
        close = False
        async with self._refresh_lock:
            refs = self._references.get(registry, 0)
            if refs <= 1:
                self._references.pop(registry, None)
                if registry in self._retired:
                    self._retired.remove(registry)
                    close = True
            else:
                self._references[registry] = refs - 1
        if close:
            await _close_registry(registry)

    async def _refresh(self, now: float) -> Registry | None:
        try:
            fingerprint, spec = await self._load_snapshot()
            if self._cached is not None and fingerprint == self._fingerprint:
                self._cached_at = now
                return None
            registry = self._fallback if spec is None else _build_from_dict(spec)
        except Exception as exc:  # noqa: BLE001 - last-known-good keeps gateway usable
            log.warning("db model registry load failed", error=repr(exc))
            self._cached = self._cached or self._fallback
            self._cached_at = now
            return None

        previous = self._cached
        self._cached = registry
        self._cached_at = now
        self._fingerprint = fingerprint
        if previous is None or previous is registry or previous is self._fallback:
            return None
        if self._references.get(previous, 0) == 0:
            return previous
        self._retired.add(previous)
        return None

    async def _load_snapshot(self) -> tuple[str, dict[str, Any] | None]:
        await self._ready()
        query = """
            SELECT
                p.id, p.type, p.base_url, p.api_key_ciphertext,
                r.id, r.alias, r.protocol, r.real_model, r.label, r.icon_type, r.icon_slug,
                r.icon_url, r.visible, r.is_default
            FROM llm_model_routes r
            JOIN llm_providers p ON p.id = r.provider_id
            WHERE p.enabled = TRUE AND r.enabled = TRUE
            ORDER BY r.protocol, r.is_default DESC, r.sort_order, r.alias, p.id
        """
        async with self._pool.connection() as conn:
            cur = await conn.execute(query)
            rows = await cur.fetchall()
        fingerprint = hashlib.sha256(repr(rows).encode("utf-8")).hexdigest()
        if not rows:
            return fingerprint, None

        providers: dict[str, dict[str, Any]] = {}
        routes: dict[str, dict[str, Any]] = {}
        default_alias = ""
        for row in rows:
            (
                provider_id,
                provider_type,
                base_url,
                api_key_ciphertext,
                route_id,
                alias,
                protocol,
                real_model,
                label,
                icon_type,
                icon_slug,
                icon_url,
                visible,
                is_default,
            ) = row
            if provider_type == "fake":
                raise CocolaError(
                    ErrorCode.INVALID_ARGUMENT,
                    "fake model providers are only supported by tests",
                )
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
            routes[route_id] = {
                "alias": alias,
                "provider": provider_id,
                "real_model": real_model,
                "label": label or alias,
                "icon": icon,
                "enabled": True,
                "visible": visible,
                "is_default": is_default,
            }
            if is_default and not default_alias:
                default_alias = route_id
        return fingerprint, {
            "default_alias": default_alias,
            "providers": providers,
            "routes": routes,
        }

    async def aclose(self) -> None:
        registries = set(self._retired)
        if self._cached is not None:
            registries.add(self._cached)
        registries.add(self._fallback)
        self._retired.clear()
        self._references.clear()
        self._cached = None
        for registry in registries:
            await _close_registry(registry)
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


async def _close_registry(registry: Registry) -> None:
    try:
        await registry.aclose()
    except Exception as exc:  # noqa: BLE001 - cleanup must not break active requests
        log.warning("model registry close failed", error=repr(exc))
