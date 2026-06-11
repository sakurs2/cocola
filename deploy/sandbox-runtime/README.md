# cocola Route-A sandbox runtime image

This is the per-user **sandbox runtime image** for cocola's Route A
architecture (see `docs/adr/0009-agent-runtime-in-sandbox.md`). The whole
Claude Code agent runtime -- Node.js + the `claude` CLI + `claude-agent-sdk` +
a thin stdio shim -- is **baked into this image** and runs *inside the user's
own container*. The agent's brain and hands both live here, so native
Bash/Read/Write/Edit are isolated to this filesystem by construction; there is
no MCP forwarding seam.

## Layout

```
deploy/sandbox-runtime/
  Dockerfile            # Node 22 + Python 3.11/uv + Claude Code CLI + shim
  offline/              # optional: vendored `npm pack` tgz for offline builds
  shim/
    agent_shim.py       # stdio shim: one JSON request -> NDJSON event stream
    entrypoint.sh       # stable launcher path the control plane execs into
```

## How the control plane drives it (STDIO, never a port)

A sandbox must **never bind a network port** (cocola hard rule). The
control-plane router invokes the shim over stdio:

```
# local Docker (M1 / runc)
docker exec -i <ctr> /opt/cocola/shim/entrypoint.sh   < request.json

# K8s + gVisor (later)
kubectl exec -i  <pod> -- /opt/cocola/shim/entrypoint.sh   < request.json
```

- **stdin**: exactly one JSON request (`prompt`, optional `system_prompt`,
  `max_turns`, `resume`, `cwd`, `permission_mode`), then EOF.
- **stdout**: NDJSON event stream (`start` / `text` / `thinking` / `tool_use` /
  `tool_result` / `result` / `done`), one compact JSON object per line.
- Auth/routing (`ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`,
  `CLAUDE_CONFIG_DIR`) come from the container ENV the provider injects -- never
  from the request, so credentials never transit the prompt channel.

`--selfcheck` runs an offline probe (no SDK call) and prints one JSON line of
runtime facts; used by the verification script.

## Persistence (ADR-0008 dual volume)

Two volumes are mounted by the provider:

| Mount in container     | Volume          | Tier | Survives                |
|------------------------|-----------------|------|-------------------------|
| `/home/cocola/.claude` | per-user (T2)   | T2   | cross-session, hibernate|
| `/workspace`           | per-session(T1b)| T1b  | hibernate, cleaned at session end |

`CLAUDE_CONFIG_DIR=/home/cocola/.claude` so memory/sessions/projects persist on
the per-user volume; `--resume <session_id>` rebuilds the brain from that
on-disk session (no RAM snapshot needed).

## Build

```bash
# optional: vendor the CLI so the build needs no npm registry
npm pack @anthropic-ai/claude-code --registry=https://registry.npmmirror.com
mv anthropic-ai-claude-code-*.tgz deploy/sandbox-runtime/offline/

docker build -t cocola/sandbox-runtime:dev deploy/sandbox-runtime
```

## Verify (local Docker / runc; same body is the future gVisor spike)

```bash
# build + offline selfcheck + dual-volume persistence (no gateway needed)
SKIP_QUERY=1 scripts/sandbox-runtime-verify.sh

# full run incl. a live model turn through the gateway
ANTHROPIC_BASE_URL=http://host.docker.internal:8081 \
ANTHROPIC_AUTH_TOKEN=<cocola-token> \
scripts/sandbox-runtime-verify.sh

# gVisor acceptance gate, once a Linux+gVisor host exists (ADR-0008 sec.4)
DOCKER_RUNTIME=runsc scripts/sandbox-runtime-verify.sh
```
