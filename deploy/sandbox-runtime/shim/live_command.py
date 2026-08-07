#!/usr/bin/env python3
"""Run one Bash command while mirroring stdout/stderr to a Cocola FIFO."""

from __future__ import annotations

import argparse
import os
import selectors
import subprocess
import sys
from pathlib import Path


def _write_all(descriptor: int, data: bytes) -> None:
    remaining = memoryview(data)
    while remaining:
        written = os.write(descriptor, remaining)
        if written <= 0:
            raise OSError("output write made no progress")
        remaining = remaining[written:]


def _parse() -> tuple[Path, str]:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--fifo", required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = list(args.command)
    if command[:1] == ["--"]:
        command = command[1:]
    if len(command) != 1:
        parser.error("exactly one shell command is required after --")
    return Path(args.fifo), command[0]


def main() -> int:
    fifo, command = _parse()
    process = subprocess.Popen(
        ["/bin/bash", "-c", command],
        stdin=sys.stdin,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    assert process.stdout is not None
    assert process.stderr is not None
    fifo_descriptor: int | None = os.open(fifo, os.O_WRONLY)
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ, sys.stdout.buffer)
    selector.register(process.stderr, selectors.EVENT_READ, sys.stderr.buffer)
    try:
        while selector.get_map():
            for key, _mask in selector.select():
                chunk = os.read(key.fileobj.fileno(), 4096)
                if not chunk:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                destination = key.data
                destination.write(chunk)
                destination.flush()
                if fifo_descriptor is not None:
                    try:
                        _write_all(fifo_descriptor, chunk)
                    except OSError:
                        # Live output is best-effort. The command must keep
                        # running if the relay disappears during cancellation.
                        os.close(fifo_descriptor)
                        fifo_descriptor = None
    finally:
        selector.close()
        if fifo_descriptor is not None:
            os.close(fifo_descriptor)
    return process.wait()


if __name__ == "__main__":
    raise SystemExit(main())
