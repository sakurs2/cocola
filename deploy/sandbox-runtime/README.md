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

The sandbox can optionally lock outbound network access down to a
**default-deny + allowlist** posture (the same iptables+ipset shape as
Anthropic's Claude Code devcontainer `init-firewall.sh`). Public access is open
by default; setting `COCOLA_SANDBOX_EGRESS_ALLOWLIST` opts into this policy:

- The container's **main process runs as root** so `firewall-entrypoint.sh` can
  install the rules (needs `NET_ADMIN`) _before_ any `exec` lands. User/agent
  code never runs as that main process -- it arrives via `docker exec` /
  `kubectl exec`, which sandbox-manager pins to the non-root `cocola` user
  (uid 10001) without `NET_ADMIN`, so it cannot alter the rules.
- **Restricted-mode baseline:** loopback, established/related, DNS, plus the
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

The provider mounts one per-session volume at `/session`. Startup creates the
following layout and links the runtime paths into it:

| Volume directory          | Runtime path           | Content                            |
| ------------------------- | ---------------------- | ---------------------------------- |
| `/session/workspace`      | `/workspace`           | project, dependencies and output   |
| `/session/runtime/claude` | `/home/cocola/.claude` | Claude session state               |
| `/session/runtime/codex`  | `/home/cocola/.codex`  | Codex session state                |
| `/session/runtime/cocola` | `/home/cocola/.cocola` | Skills and future browser/IDE data |
| `/session/home/local`     | `/home/cocola/.local`  | user-installed tools               |

`/cache` remains ephemeral so package downloads do not consume the default
`2Gi` local-path PVC request. Shared and Personal Skill bundles are reconciled
into the Session Volume's unified Skill Set; `/data/plugins` is not required for
Session recovery. Secrets, rootfs files and the rest of `$HOME` are never copied
into the Session Volume.

Destroying a sandbox preserves the volume. A later run is scheduled back to
the volume's node and mounts the same claim; no MinIO checkpoint is involved.
Claude and Codex share the reconciled Skill Set through symlinks under
`/home/cocola/.cocola/skillsets/agents-skill-v1`.

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

Formal deployment uses the fixed Cocola release version selected at install:

```bash
cocola install --version v0.1.0
```

The CLI applies the same registry and version to every Cocola service image,
including `cocola-sandbox-runtime`, so a rollout cannot accidentally combine
components from different releases.

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
scripts/sandbox-runtime-verify.sh

# full run incl. a live model turn through the gateway
ANTHROPIC_BASE_URL=http://host.docker.internal:8081 \
ANTHROPIC_AUTH_TOKEN=<cocola-token> \
scripts/sandbox-runtime-verify.sh

# gVisor acceptance gate, once a Linux+gVisor host exists (ADR-0008 sec.4)
DOCKER_RUNTIME=runsc scripts/sandbox-runtime-verify.sh
```
