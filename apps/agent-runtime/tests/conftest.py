"""Pytest bootstrap: put the generated proto stubs on sys.path.

The agent gRPC stubs (cocola.agent.v1) live in packages/proto/gen/python, which
production wires via PYTHONPATH (see the Makefile / sandbox_demo). Tests that
import server.py need the same path, so we prepend it here once.
"""

import sys
from pathlib import Path

_PROTO_GEN = Path(__file__).resolve().parents[3] / "packages" / "proto" / "gen" / "python"
if _PROTO_GEN.is_dir() and str(_PROTO_GEN) not in sys.path:
    sys.path.insert(0, str(_PROTO_GEN))
