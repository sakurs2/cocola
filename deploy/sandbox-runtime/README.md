# cocola Route-A sandbox runtime image

This is the session-scoped **sandbox runtime image** for cocola's Route A
architecture (see `docs/adr/0009-agent-runtime-in-sandbox.md`). The whole
Claude Code agent runtime -- Node.js + the `claude` CLI + `claude-agent-sdk` +
a thin stdio shim -- is **baked into this image** and runs _inside the user's
own container_. The agent's brain and hands both live here, so native
Bash/Read/Write/Edit are isolated to this filesystem by construction; there is
no MCP forwarding seam.

The image is built **`FROM opensandbox/code-interpreter`** -- OpenSandbox's
official multi-language runtime (Node 22 + uv + Python already inside), which
is also cocola's sandbox backend (see `docs/adr/0014-...`). cocola reuses it
and layers only the pieces it lacks: Claude Code CLI, `claude-agent-sdk` venv,
the stdio shim, the egress-firewall toolchain, and common browser/document
tools. This honours cocola's "prefer upstream over reinventing" rule while
keeping the CLI **build-time baked** (ADR-0009 sec.3): nothing is installed on
container start. Override the base with `--build-arg OPENSANDBOX_BASE=...` for a
pinned/offline mirror.

## Layout

```
deploy/sandbox-runtime/
  Dockerfile               # FROM opensandbox/code-interpreter + Claude Code CLI + SDK venv + shim
  init-firewall.sh         # iptables+ipset egress allowlist (ADR-0009)
  firewall-entrypoint.sh   # runs the firewall as root, then keep-alives
  offline/                 # optional: vendored `npm pack` tgz for offline builds
  shim/
    agent_shim.py          # stdio shim: one JSON request -> NDJSON event stream
    entrypoint.sh          # stable launcher path the control plane execs into
```

## Egress firewall (ADR-0009 hardening)

The sandbox runs untrusted user/agent code, so its outbound network is
locked down to a **default-deny + allowlist** posture (the same iptables+ipset
shape as Anthropic's Claude Code devcontainer `init-firewall.sh`):

- The container's **main process runs as root** so `firewall-entrypoint.sh` can
  install the rules (needs `NET_ADMIN`) _before_ any `exec` lands. User/agent
  code never runs as that main process -- it arrives via `docker exec` /
  `kubectl exec`, which sandbox-manager pins to the non-root `cocola` user
  (uid 10001) without `NET_ADMIN`, so it cannot alter the rules.
- **Baseline (always allowed):** loopback, established/related, DNS, plus the
  llm-gateway host (Route A's lifeline; folded in by the orchestrator from
  `COCOLA_SANDBOX_LLM_BASE_URL`).
- **Allowlist:** `COCOLA_EGRESS_ALLOWLIST` (comma/space-separated domains/CIDRs)
  is resolved and added on top. Everything else is dropped.
- The firewall is only installed when sandbox-manager configures an egress
  policy (it passes `CAP_NET_ADMIN` + the env). The legacy alpine demo image,
  with no policy, stays on the plain keep-alive command.

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

## Preinstalled tools

The runtime includes a small, high-frequency toolbelt so agents can inspect
files, build simple projects, render HTML, take screenshots, and process common
document/media outputs without installing dependencies at sandbox start:

- Basics: `wget`, `ripgrep`, `fd`, `jq`, `yq`, `tree`, `file`, `less`,
  `procps`, `psmisc`, `unzip`, `zip`, `tar`, `gzip`, `zstd`.
- Build helpers: `make`, `build-essential`, `pkg-config`, `sqlite3`.
- Node/web: `pnpm`, `yarn`, global `playwright`, Playwright-managed
  `chromium`.
- Documents/media: `poppler-utils`, `imagemagick`, `librsvg2-bin`.
- Fonts: Noto core, CJK, and color emoji fonts.

Playwright uses a build-time downloaded Chromium under `/ms-playwright`, exposed
through `/usr/local/bin/chromium`. The image sets
`NODE_PATH=/usr/local/lib/node_modules`, `PLAYWRIGHT_BROWSERS_PATH`,
`CHROME_BIN`, `PUPPETEER_EXECUTABLE_PATH`, and
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` so simple Node scripts can locate the
global package and browser. A typical screenshot probe can launch with:

```js
const { chromium } = require("playwright");
const browser = await chromium.launch({
  executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH,
  args: ["--no-sandbox"],
});
```

## Persistence

Two volumes are mounted by the provider:

| Mount in container     | Volume            | Survives                          |
| ---------------------- | ----------------- | --------------------------------- |
| `/workspace`           | per-session, RW   | hibernate, cleaned at session end |
| `/home/cocola/.claude` | per-session, RW   | hibernate, cleaned at session end |
| `/data/plugins`        | shared, read-only | platform-managed                  |

For host-backed storage, the provider maps them under
`<COCOLA_SANDBOX_ROOT>/users/<user>/sessions/<session>/{workspace,claude}`.
Point `COCOLA_SANDBOX_ROOT` at an NFS/NAS mount to share session storage across
nodes without changing the in-container contract.

`CLAUDE_CONFIG_DIR=/home/cocola/.claude` so Claude Code memory/sessions/projects
are isolated per cocola session without appearing in `/workspace` file listings.
`--resume <session_id>` rebuilds the brain from that on-disk session (no RAM
snapshot needed).

## Build

```bash
# optional: vendor the CLI so the build needs no npm registry
npm pack @anthropic-ai/claude-code --registry=https://registry.npmmirror.com
mv anthropic-ai-claude-code-*.tgz deploy/sandbox-runtime/offline/

docker build -t cocola/sandbox-runtime:dev deploy/sandbox-runtime
```

## Publish

Production images are published as OCI images to GHCR. The GitHub Actions
workflow `.github/workflows/sandbox-runtime-image.yml` builds a multi-arch
manifest for `linux/amd64` and `linux/arm64`, pushes immutable `sha-<commit>`
tags, keeps `latest` and `dev` moving on the default branch, adds semver tags
for `v*` releases, and prints the digest-pinned reference in the workflow
summary.

For local publishing after changing the image:

```bash
# gh auth must include write:packages, then log in to GHCR:
gh auth refresh -h github.com -s write:packages
gh auth token | docker login ghcr.io -u <github-user> --password-stdin

# build, selfcheck, push latest + dev + sha-<commit> for linux/amd64
scripts/sandbox-runtime-publish.sh

# build and push a multi-arch manifest for linux/amd64 + linux/arm64
PLATFORMS=linux/amd64,linux/arm64 scripts/sandbox-runtime-publish.sh

# release-style tags, e.g. v0.1.0 and 0.1
VERSION_TAG=v0.1.0 scripts/sandbox-runtime-publish.sh
```

Use digest-pinned references for production rollout:

```bash
COCOLA_SANDBOX_IMAGE=ghcr.io/sakurs2/cocola-sandbox-runtime:sha-<commit>@sha256:<digest-from-ci> \
  make prod
```

For quick single-server deployments that intentionally track the newest
successful master build, `latest` is available as a convenience tag:

```bash
docker pull ghcr.io/sakurs2/cocola-sandbox-runtime:latest
```

`latest` is mutable by design. It is multi-arch, so Docker pulls the matching
`linux/amd64` or `linux/arm64` image automatically on ordinary Linux servers
and Apple Silicon Macs. Keep `sha-<commit>` or `vX.Y.Z@sha256:<digest>` for
reproducible production rollouts and rollback targets.

## Verify (local Docker / runc; same body is the future gVisor spike)

```bash
# build + offline selfcheck + session-workspace persistence (no gateway needed)
SKIP_QUERY=1 scripts/sandbox-runtime-verify.sh

# full run incl. a live model turn through the gateway
ANTHROPIC_BASE_URL=http://host.docker.internal:8081 \
ANTHROPIC_AUTH_TOKEN=<cocola-token> \
scripts/sandbox-runtime-verify.sh

# gVisor acceptance gate, once a Linux+gVisor host exists (ADR-0008 sec.4)
DOCKER_RUNTIME=runsc scripts/sandbox-runtime-verify.sh
```
