# fix: prevent generated binary artifacts from overflowing the agent transport

- Change time: 2026-08-11 19:36 (+08:00)

## Reason

When an Agent generated a PNG and then used the built-in `Read` tool to preview
the finished file, Claude Code encoded the binary result into its JSON protocol.
The message exceeded the SDK's 1 MiB decoder limit, so an otherwise successful
generation turn ended with an opaque JSON decoding error and no published file.

## Changes

- `apps/agent-runtime/cocola_agent_runtime/server.py`: make the Artifact delivery
  contract explicit in every artifact-enabled execute turn, including safe
  verification and final inventory requirements.
- `deploy/sandbox-runtime/shim/agent_shim.py`: reject `Read` before execution for
  binary files under the Artifact output directory and for binary files larger
  than 256 KiB, while preserving text reads and small input-image inspection.
- `deploy/sandbox-runtime/skills/cocola-sandbox-artifacts/SKILL.md`: document the
  same binary-delivery rule in the built-in Artifact skill.
- `apps/agent-runtime/tests`: cover prompt injection, blocked generated/large
  binary reads, and unaffected ordinary reads.

The guard stays local to the existing sandbox shim and introduces no service,
queue, provider, or new image-generation capability.
