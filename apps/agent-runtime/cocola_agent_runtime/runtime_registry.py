"""Built-in Agent Runtime catalog and provider dispatch."""

from __future__ import annotations

from dataclasses import dataclass

from cocola_agent_runtime.agent_provider import AgentProvider


@dataclass(frozen=True)
class RuntimeDescriptor:
    id: str
    label: str
    model_protocol: str
    is_default: bool = False


@dataclass(frozen=True)
class RuntimeEntry:
    descriptor: RuntimeDescriptor
    provider: AgentProvider


class RuntimeRegistry:
    """Immutable runtime catalog; constructed once at process startup."""

    def __init__(self, entries: list[RuntimeEntry]):
        if not entries:
            raise ValueError("at least one Agent Runtime is required")
        self._entries: dict[str, RuntimeEntry] = {}
        defaults = 0
        for entry in entries:
            runtime_id = entry.descriptor.id.strip()
            if not runtime_id or runtime_id in self._entries:
                raise ValueError(f"invalid or duplicate Agent Runtime: {runtime_id!r}")
            if not entry.descriptor.label.strip() or not entry.descriptor.model_protocol.strip():
                raise ValueError(f"incomplete Agent Runtime descriptor: {runtime_id}")
            self._entries[runtime_id] = entry
            defaults += int(entry.descriptor.is_default)
        if defaults != 1:
            raise ValueError("exactly one Agent Runtime must be the default")

    @property
    def descriptors(self) -> tuple[RuntimeDescriptor, ...]:
        return tuple(entry.descriptor for entry in self._entries.values())

    @property
    def default(self) -> RuntimeEntry:
        return next(entry for entry in self._entries.values() if entry.descriptor.is_default)

    def resolve(self, runtime_id: str | None) -> RuntimeEntry:
        requested = (runtime_id or "").strip()
        if not requested:
            return self.default
        try:
            return self._entries[requested]
        except KeyError as exc:
            raise KeyError(f"unsupported Agent Runtime: {requested}") from exc


def built_in_registry(provider: AgentProvider) -> RuntimeRegistry:
    """Cocola ships both runtimes in one sandbox image and one provider seam."""
    return RuntimeRegistry(
        [
            RuntimeEntry(
                RuntimeDescriptor(
                    id="claude-code",
                    label="Claude Code",
                    model_protocol="anthropic-messages",
                    is_default=True,
                ),
                provider,
            ),
            RuntimeEntry(
                RuntimeDescriptor(
                    id="codex",
                    label="Codex",
                    model_protocol="openai-responses",
                ),
                provider,
            ),
        ]
    )
