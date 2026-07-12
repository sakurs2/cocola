from cocola_llm_gateway import db_registry
from cocola_llm_gateway.db_registry import PostgresRegistrySource


class FakeRegistry:
    def __init__(self, name):
        self.name = name
        self.closed = 0

    async def aclose(self):
        self.closed += 1


async def test_refresh_retires_registry_only_after_active_lease_releases(monkeypatch):
    fallback = FakeRegistry("fallback")
    source = PostgresRegistrySource(
        "postgres://unused",
        fallback,
        secret="test-secret",
        ttl_s=0,
    )
    snapshots = iter(
        [
            ("first", {"name": "first"}),
            ("second", {"name": "second"}),
        ]
    )

    async def load_snapshot():
        return next(snapshots)

    monkeypatch.setattr(source, "_load_snapshot", load_snapshot)
    monkeypatch.setattr(
        db_registry,
        "_build_from_dict",
        lambda spec: FakeRegistry(spec["name"]),
    )

    first = await source.acquire_registry()
    second = await source.acquire_registry()
    assert first.name == "first"
    assert second.name == "second"
    assert first.closed == 0

    await source.release_registry(second)
    assert second.closed == 0
    await source.release_registry(first)
    assert first.closed == 1

    await source.aclose()
    assert second.closed == 1
    assert fallback.closed == 1
