"""Object-store fetcher: pulls attachment bytes on the model's behalf.

Attachment delivery is always push (ADR-0017 P1a): the gateway pre-uploads every
file to the object store (source of truth) and hands agent-runtime either the
inline bytes (small files) or just the object key (large files). This module is
the "given a key, fetch the bytes" side used to materialize the key-only ones
before they are provisioned into ./uploads/.

The gateway owns uploads; agent-runtime only reads. `Fetcher` is a tiny Protocol
so the servicer depends on an abstraction (real MinIO in prod, a fake in tests)
rather than the minio SDK directly -- the same composition-root pattern the rest
of the runtime uses.

Configuration is env-driven (COCOLA_MINIO_*), mirroring the gateway. When the
endpoint/bucket are unset the fetcher is not built; a key-only attachment then
surfaces as a clean provisioning error rather than a silent drop.
"""

from __future__ import annotations

import os
from typing import Protocol

from cocola_common import get_logger

log = get_logger("cocola.agent-runtime.objstore")


class Fetcher(Protocol):
    """Fetches an object's raw bytes by key."""

    def get(self, key: str) -> bytes: ...


class MinioFetcher:
    """minio-SDK-backed Fetcher. Reads one object fully into memory (attachments
    are size-capped upstream, so a full read is acceptable)."""

    def __init__(self, client, bucket: str) -> None:
        self._client = client
        self._bucket = bucket

    def get(self, key: str) -> bytes:
        resp = None
        try:
            resp = self._client.get_object(self._bucket, key)
            return resp.read()
        finally:
            if resp is not None:
                resp.close()
                resp.release_conn()


def fetcher_from_env() -> Fetcher | None:
    """Build a MinioFetcher from COCOLA_MINIO_* env, or None when unconfigured.

    Secret key honours the "_FILE" indirection (ADR-0008): if
    COCOLA_MINIO_SECRET_KEY_FILE points at a readable file, its contents (minus a
    trailing newline) are used; otherwise COCOLA_MINIO_SECRET_KEY applies.
    """
    endpoint = os.getenv("COCOLA_MINIO_ENDPOINT", "").strip()
    bucket = os.getenv("COCOLA_MINIO_BUCKET", "").strip()
    if not endpoint or not bucket:
        return None

    # Imported lazily so the dependency is only needed when object storage is
    # actually configured (keeps zero-config local boots import-light).
    from minio import Minio

    access_key = os.getenv("COCOLA_MINIO_ACCESS_KEY", "").strip()
    secret_key = _secret_from_env("COCOLA_MINIO_SECRET_KEY")
    secure = os.getenv("COCOLA_MINIO_USE_SSL", "") == "1"

    client = Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure)
    log.info("attachment object-store fetcher enabled", bucket=bucket, endpoint=endpoint)
    return MinioFetcher(client, bucket)


def _secret_from_env(name: str) -> str:
    path = os.getenv(name + "_FILE", "").strip()
    if path:
        try:
            with open(path, encoding="utf-8") as fh:
                return fh.read().rstrip("\r\n")
        except OSError:
            pass
    return os.getenv(name, "")
