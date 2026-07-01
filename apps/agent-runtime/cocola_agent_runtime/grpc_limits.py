"""Shared gRPC message-size limit for the agent-runtime's gRPC surfaces.

Attachment bytes ride gRPC on two hops the agent-runtime owns:

    gateway --(Query, may carry inline attachment bytes)--> agent-runtime server
    agent-runtime --(WriteFile, carries the file bytes)--> sandbox-manager

gRPC's *default* single-message ceiling is 4 MiB. That is smaller than both the
ADR-0017 inline split threshold (16 MiB) and the frontend upload cap (32 MiB),
so without an explicit override any attachment over ~4 MB dies on the wire with
``ResourceExhausted: Received message larger than max``. We raise the ceiling on
both the server we host and the client we dial.

This is a *transport* safety cap, distinct from the ADR-0017 inline/backend-pull
split threshold: the split decides delivery strategy, this only bounds a single
gRPC frame. Configurable via ``COCOLA_GRPC_MAX_MESSAGE_BYTES`` so it is not baked
in; a non-positive/invalid value falls back to 64 MiB (comfortably above the
32 MiB frontend cap, with headroom for base64 / proto framing overhead).
"""

from __future__ import annotations

import os

# 64 MiB. Above the 32 MiB frontend cap so both delivery paths (inline push and
# the sandbox WriteFile that always carries real bytes) fit with headroom.
DEFAULT_MAX_MESSAGE_BYTES = 64 * 1024 * 1024


def max_message_bytes() -> int:
    """Resolve the configured gRPC single-message ceiling (bytes)."""
    raw = os.getenv("COCOLA_GRPC_MAX_MESSAGE_BYTES", "").strip()
    if raw:
        try:
            n = int(raw)
            if n > 0:
                return n
        except ValueError:
            pass
    return DEFAULT_MAX_MESSAGE_BYTES


def channel_options() -> list[tuple[str, int]]:
    """gRPC channel/server options raising both send and receive ceilings."""
    n = max_message_bytes()
    return [
        ("grpc.max_send_message_length", n),
        ("grpc.max_receive_message_length", n),
    ]
