#!/usr/bin/env python3
"""Trim trailing whitespace + ensure a single final newline.

A tiny, dependency-free replacement for the pre-commit.com remote hooks
(everything stays local to survive the corporate TLS proxy).
"""

import sys


def fix(path: str) -> bool:
    try:
        with open(path, "rb") as f:
            raw = f.read()
    except (OSError, ValueError):
        return False
    if b"\x00" in raw:  # skip binary
        return False
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        return False
    lines = [ln.rstrip() for ln in text.splitlines()]
    new = "\n".join(lines)
    if new and not new.endswith("\n"):
        new += "\n"
    if new != text:
        with open(path, "w", encoding="utf-8") as f:
            f.write(new)
        return True
    return False


def main(argv: list[str]) -> int:
    changed = [p for p in argv[1:] if fix(p)]
    for p in changed:
        print(f"fixed whitespace: {p}")
    return 1 if changed else 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
