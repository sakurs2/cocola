import pytest
from cocola_agent_runtime.agent_provider import AgentEvent
from cocola_agent_runtime.runtime_registry import (
    RuntimeDescriptor,
    RuntimeEntry,
    RuntimeRegistry,
)


class Provider:
    async def query(self, prompt, options):
        yield AgentEvent(kind="done", data={})


def _entry(runtime_id: str, *, default: bool = False) -> RuntimeEntry:
    return RuntimeEntry(
        RuntimeDescriptor(
            id=runtime_id,
            label=runtime_id,
            model_protocol="anthropic-messages",
            is_default=default,
        ),
        Provider(),
    )


def test_registry_requires_one_default_and_unique_ids():
    with pytest.raises(ValueError, match="exactly one"):
        RuntimeRegistry([_entry("claude-code")])
    with pytest.raises(ValueError, match="duplicate"):
        RuntimeRegistry([_entry("claude-code", default=True), _entry("claude-code")])


def test_registry_resolves_default_and_rejects_unknown_runtime():
    registry = RuntimeRegistry([_entry("claude-code", default=True), _entry("codex")])

    assert registry.resolve("").descriptor.id == "claude-code"
    assert registry.resolve("codex").descriptor.id == "codex"
    with pytest.raises(KeyError, match="unsupported"):
        registry.resolve("other")
