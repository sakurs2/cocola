"""Unit tests for the object-store fetcher builder (ADR-0017 P1a).

We do not stand up a real MinIO here; we assert the env-driven construction
contract: unconfigured => None, _FILE secret indirection, and that MinioFetcher
reads/closes the SDK response correctly against a fake client.
"""

from cocola_agent_runtime import objstore


def test_fetcher_from_env_none_when_unconfigured(monkeypatch):
    monkeypatch.delenv("COCOLA_MINIO_ENDPOINT", raising=False)
    monkeypatch.delenv("COCOLA_MINIO_BUCKET", raising=False)
    assert objstore.fetcher_from_env() is None


def test_fetcher_from_env_none_when_bucket_missing(monkeypatch):
    monkeypatch.setenv("COCOLA_MINIO_ENDPOINT", "127.0.0.1:9000")
    monkeypatch.delenv("COCOLA_MINIO_BUCKET", raising=False)
    assert objstore.fetcher_from_env() is None


def test_fetcher_from_env_builds_when_configured(monkeypatch):
    monkeypatch.setenv("COCOLA_MINIO_ENDPOINT", "127.0.0.1:9000")
    monkeypatch.setenv("COCOLA_MINIO_BUCKET", "cocola")
    monkeypatch.setenv("COCOLA_MINIO_ACCESS_KEY", "cocola")
    monkeypatch.setenv("COCOLA_MINIO_SECRET_KEY", "cocola_dev_pw")
    monkeypatch.delenv("COCOLA_MINIO_USE_SSL", raising=False)
    f = objstore.fetcher_from_env()
    assert isinstance(f, objstore.MinioFetcher)
    assert f._bucket == "cocola"


def test_secret_from_env_file_indirection(tmp_path, monkeypatch):
    p = tmp_path / "secret"
    p.write_text("from-file\n")
    monkeypatch.setenv("COCOLA_MINIO_SECRET_KEY", "from-env")
    monkeypatch.setenv("COCOLA_MINIO_SECRET_KEY_FILE", str(p))
    assert objstore._secret_from_env("COCOLA_MINIO_SECRET_KEY") == "from-file"


def test_secret_from_env_falls_back_to_plain(monkeypatch):
    monkeypatch.delenv("COCOLA_MINIO_SECRET_KEY_FILE", raising=False)
    monkeypatch.setenv("COCOLA_MINIO_SECRET_KEY", "plain")
    assert objstore._secret_from_env("COCOLA_MINIO_SECRET_KEY") == "plain"


class _FakeResp:
    def __init__(self, data):
        self._data = data
        self.closed = False
        self.released = False

    def read(self):
        return self._data

    def close(self):
        self.closed = True

    def release_conn(self):
        self.released = True


class _FakeClient:
    def __init__(self, data):
        self._resp = _FakeResp(data)
        self.calls = []
        self.presign_calls = []

    def get_object(self, bucket, key):
        self.calls.append((bucket, key))
        return self._resp

    def presigned_put_object(self, bucket, key, *, expires):
        self.presign_calls.append((bucket, key, expires.total_seconds()))
        return f"http://minio.local/{bucket}/{key}?sig=test"


def test_minio_fetcher_reads_and_closes():
    client = _FakeClient(b"payload")
    f = objstore.MinioFetcher(client, "cocola")
    assert f.get("attachments/s/uuid-a.bin") == b"payload"
    assert client.calls == [("cocola", "attachments/s/uuid-a.bin")]
    # Connection is always closed + released after read (no leak).
    assert client._resp.closed and client._resp.released


def test_minio_fetcher_presigns_put_url():
    client = _FakeClient(b"")
    f = objstore.MinioFetcher(client, "cocola")

    url = f.presigned_put_url("checkpoints/u/s/archive.tar.zst", expires_seconds=60)

    assert url == "http://minio.local/cocola/checkpoints/u/s/archive.tar.zst?sig=test"
    assert client.presign_calls == [("cocola", "checkpoints/u/s/archive.tar.zst", 60.0)]
