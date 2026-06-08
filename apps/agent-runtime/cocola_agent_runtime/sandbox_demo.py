"""End-to-end demo: agent-runtime -> sandbox-manager -> Docker.

Exercises the full M1 loop from the Python side:

    create  ->  exec (echo hello)  ->  write/read file  ->  destroy

Run (sandbox-manager must be listening on :50051):

    PYTHONPATH=packages/proto/gen/python \
        python -m cocola_agent_runtime.sandbox_demo --addr localhost:50051

Exit code 0 means the whole loop succeeded.
"""

from __future__ import annotations

import argparse
import sys

from cocola_agent_runtime.sandbox_client import SandboxClient


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description="cocola M1 sandbox e2e demo")
    ap.add_argument("--addr", default="localhost:50051")
    ap.add_argument("--user", default="demo-user")
    ap.add_argument("--session", default="demo-session")
    args = ap.parse_args(argv)

    with SandboxClient(addr=args.addr) as sb:
        print("[1/4] create")
        box = sb.create(args.user, args.session)
        print(f"      -> {box.id}  {box.endpoint}")

        print("[2/4] exec: echo hello-from-cocola")
        res = sb.exec(box.id, ["sh", "-c", "echo hello-from-cocola; id -un"])
        sys.stdout.write(res.stdout.decode(errors="replace"))
        if res.error:
            print(f"      !! error: {res.error}")
            return 1
        print(f"      -> exit {res.exit_code}")

        print("[3/4] write + read file roundtrip")
        payload = b"cocola-roundtrip-42\n"
        sb.write_file(box.id, "/workspace/probe.txt", payload)
        got = sb.read_file(box.id, "/workspace/probe.txt")
        ok = got == payload
        print(f"      -> {'match' if ok else 'MISMATCH'} ({got!r})")
        if not ok:
            return 1

        print("[4/4] destroy")
        sb.destroy(box.id)
        print("      -> ok")

    print("DEMO OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
