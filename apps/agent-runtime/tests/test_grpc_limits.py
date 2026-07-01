"""Unit tests for the gRPC message-size limit helper.

The default must exceed the 32 MiB frontend upload cap (so no attachment the UI
accepts dies on the wire with ResourceExhausted), and the env override must be
honoured while invalid/non-positive values fall back to the default.
"""

from __future__ import annotations

import pytest
from cocola_agent_runtime.grpc_limits import (
    DEFAULT_MAX_MESSAGE_BYTES,
    channel_options,
    max_message_bytes,
)


def test_default_is_above_frontend_cap():
    # 64 MiB default, comfortably above the 32 MiB frontend upload cap.
    assert DEFAULT_MAX_MESSAGE_BYTES == 64 * 1024 * 1024
    assert DEFAULT_MAX_MESSAGE_BYTES > 32 * 1024 * 1024


def test_env_override_honoured(monkeypatch):
    monkeypatch.setenv("COCOLA_GRPC_MAX_MESSAGE_BYTES", "123456")
    assert max_message_bytes() == 123456


@pytest.mark.parametrize("bad", ["", "0", "-1", "notanint", "  "])
def test_invalid_or_nonpositive_falls_back(monkeypatch, bad):
    monkeypatch.setenv("COCOLA_GRPC_MAX_MESSAGE_BYTES", bad)
    assert max_message_bytes() == DEFAULT_MAX_MESSAGE_BYTES


def test_channel_options_shape(monkeypatch):
    monkeypatch.setenv("COCOLA_GRPC_MAX_MESSAGE_BYTES", "999")
    opts = dict(channel_options())
    assert opts["grpc.max_send_message_length"] == 999
    assert opts["grpc.max_receive_message_length"] == 999
