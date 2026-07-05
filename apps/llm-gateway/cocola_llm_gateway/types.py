"""Normalized, provider-agnostic domain types for the LLM gateway.

These types are the gateway's *internal* lingua franca. Every concrete
upstream (Anthropic passthrough, OpenAI-compatible translation, Fake) maps the
wire format it speaks to/from these structures, so the service layer, billing,
routing and middleware never depend on a vendor schema.

Design rules:
- Pydantic for validation at the HTTP edge; plain dataclasses for the hot
  streaming path (cheap to allocate, no validation overhead per chunk).
- The stream is a sequence of `StreamEvent`s. We deliberately mirror the
  Anthropic event taxonomy (message_start / content_block_delta / message_delta
  / message_stop) because that is the contract our front-end (`/v1/messages`)
  must emit for the Claude Agent SDK, and keeping one taxonomy end-to-end avoids
  a lossy translation in the middle.
- `Usage` is the billing source of truth. Upstreams populate it from whatever
  the vendor reports; the metering hook reads it at stream end.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import StrEnum
from typing import Any, Literal

from pydantic import BaseModel, Field

# --------------------------------------------------------------------------- #
# Request side (normalized)
# --------------------------------------------------------------------------- #

Role = Literal["system", "user", "assistant"]


class ChatMessage(BaseModel):
    """A single conversational turn.

    `content` is kept as a string for M3. The Anthropic content-block array
    (text/image/tool_use/tool_result) is flattened to text on the way in and
    re-expanded on the way out by the Anthropic front-end codec; richer block
    passthrough is an additive follow-up.
    """

    role: Role
    content: str
    # ADR-0010: when the inbound message carried a non-text Anthropic content
    # block array (tool_use / tool_result / image), the *raw* array is preserved
    # here verbatim for loss-free passthrough. `content` still holds the
    # text-flattened form for billing word-count and text-only providers.
    content_blocks: list[dict[str, Any]] | None = None


class ChatParams(BaseModel):
    """Inference knobs, vendor-neutral. Unknown vendor params are not accepted
    here on purpose — providers translate from this fixed set."""

    max_tokens: int = Field(default=1024, ge=1)
    temperature: float | None = Field(default=None, ge=0.0, le=2.0)
    top_p: float | None = Field(default=None, ge=0.0, le=1.0)
    stop: list[str] = Field(default_factory=list)
    stream: bool = Field(default=True)
    # ADR-0010: opaque Anthropic tool definitions / choice, forwarded verbatim.
    # Default empty/None keeps text-only providers and existing tests unchanged.
    tools: list[dict[str, Any]] = Field(default_factory=list)
    tool_choice: dict[str, Any] | None = None


class ChatRequest(BaseModel):
    """What the service layer hands to an UpstreamProvider.

    `model` is the *resolved real* model id (router has already mapped the
    caller-facing alias to a concrete upstream model). `user_id` / `session_id`
    are mocked until M4 but threaded through now so billing is correct from day
    one.
    """

    model: str
    messages: list[ChatMessage]
    params: ChatParams = Field(default_factory=ChatParams)
    user_id: str = ""
    session_id: str = ""
    # Free-form metadata for hooks (e.g. caller alias, request id). Never sent
    # upstream verbatim.
    metadata: dict[str, str] = Field(default_factory=dict)


# --------------------------------------------------------------------------- #
# Response side (streaming) — dataclasses on the hot path
# --------------------------------------------------------------------------- #


class StreamEventType(StrEnum):
    MESSAGE_START = "message_start"
    CONTENT_DELTA = "content_block_delta"
    MESSAGE_DELTA = "message_delta"
    MESSAGE_STOP = "message_stop"
    ERROR = "error"
    # ADR-0010: relay an upstream Anthropic content-block frame verbatim
    # (content_block_start / _delta incl. input_json_delta / _stop) so tool_use
    # blocks survive end to end. `extra["frame"]` holds the raw Anthropic JSON.
    PASSTHROUGH = "passthrough"


@dataclass(slots=True)
class Usage:
    """Token accounting. Billing reads this; defaults are zero so a provider
    that never reports usage simply bills nothing rather than crashing."""

    prompt_tokens: int = 0
    completion_tokens: int = 0

    @property
    def total_tokens(self) -> int:
        return self.prompt_tokens + self.completion_tokens

    def merge(self, other: Usage) -> None:
        """Anthropic reports input tokens in message_start and output tokens
        incrementally in message_delta; we accumulate into one Usage."""
        self.prompt_tokens = max(self.prompt_tokens, other.prompt_tokens)
        self.completion_tokens += other.completion_tokens


@dataclass(slots=True)
class StreamEvent:
    """One unit of a streamed response.

    - MESSAGE_START: carries initial Usage (prompt tokens), `model`.
    - CONTENT_DELTA: carries `text` (an incremental token/segment).
    - MESSAGE_DELTA: carries incremental `usage` (output tokens) and/or
      `finish_reason`.
    - MESSAGE_STOP: terminal marker; no payload required.
    - ERROR: carries `error` (normalized message) and `code`.
    """

    type: StreamEventType
    text: str = ""
    usage: Usage | None = None
    model: str = ""
    finish_reason: str = ""
    error: str = ""
    code: str = ""
    # Escape hatch for vendor-specific fields a codec may want to preserve.
    extra: dict[str, Any] = field(default_factory=dict)
