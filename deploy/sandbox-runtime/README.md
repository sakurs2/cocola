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
the stdio shim, the versioned Cocola Runtime contract, the egress-firewall
toolchain, and common browser/document tools. This honours cocola's "prefer
upstream over reinventing" rule while
keeping the CLI **build-time baked** (ADR-0009 sec.3): nothing is installed on
container start. Override the base with `--build-arg OPENSANDBOX_BASE=...` for a
pinned/offline mirror.

## Layout

```
deploy/sandbox-runtime/
  Dockerfile               # FROM opensandbox/code-interpreter + Claude Code CLI + SDK venv + shim
  init-firewall.sh         # iptables+ipset egress allowlist (ADR-0009)
  runtime-entrypoint.sh    # canonical lifecycle entrypoint for every provider
  runtime-manifest.json    # versioned workspace/profile/service contract
  supervisord.conf         # resident optional-service lifecycle
  cocola_sandbox.py        # guest CLI: runtime, service, workspace, browser, artifacts
  browser-runner.js        # one-shot persistent-context Playwright runner
  firewall-entrypoint.sh   # compatibility wrapper for the old entrypoint path
  code-server-launch.sh    # one-shot non-root Code Server launcher
  code-server-extensions.lock.json # exact platform extension/tool inventory
  install-code-server-extensions.sh # deterministic build-time installer
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

- The container's **main process runs as root** so `runtime-entrypoint.sh` can
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

## Runtime contract and profiles

Both the image `CMD` and OpenSandbox's custom create entrypoint converge on
`/opt/cocola/runtime-entrypoint.sh`. Provider-specific code only prepares the
Session Volume links; the shared entrypoint validates the selected profile,
creates the workspace contract, installs the firewall, and starts
`supervisord`. Supervisor owns Code Server restart/failure state, so optional
service failure does not take down Agent Exec.

Profiles are operator-level policy injected by Sandbox Manager:

| Profile   | Default resources  | Code Server | Browser   | Artifacts        |
| --------- | ------------------ | ----------- | --------- | ---------------- |
| `coding`  | `2000m` / `4096Mi` | enabled     | on-demand | workspace output |
| `minimal` | `500m` / `512Mi`   | disabled    | disabled  | workspace output |

Explicit Sandbox resources override the profile. Operators may override the
defaults with `COCOLA_OPENSANDBOX_DEFAULT_CPU/MEMORY`,
`COCOLA_CODE_SERVER_ENABLED`, and `COCOLA_BROWSER_ENABLED`; Agent requests
cannot overwrite these keys.

## Image-managed Code Server extensions

The coding profile uses a fixed, root-owned extension directory at
`/opt/cocola/code-server/extensions`. Extension versions are locked in
`code-server-extensions.lock.json`, installed while the runtime image is built,
and verified against the complete installed inventory. Sandboxes never install
or update platform extensions when Code Server starts.

The standard image includes:

- Python (`ms-python.python`, BasedPyright, Ruff)
- Go (`golang.Go`)
- Java (`redhat.java`)
- C/C++ (`vscode-clangd`)
- Bash, YAML, Markdown linting, and Markdown All in One
- Code's built-in JavaScript, TypeScript, JSON, HTML, CSS, and Markdown language
  features

Extensions that do not bundle their server use the fixed tools under
`/opt/cocola/toolchains/bin`: `gopls`, `clangd`, `shellcheck`, and `shfmt`.
The existing JDK 21 launches the Java language server. All tool versions are in
the same lock file, and the runtime selfcheck fails when a required tool is
missing.

Inside a sandbox, the stable guest CLI exposes the effective contract without
requiring access to the OpenSandbox control plane:

```bash
cocola-sandbox info
cocola-sandbox info --json
cocola-sandbox service status --json
cocola-sandbox workspace info --json
cocola-sandbox browser status --json
cocola-sandbox browser inspect https://example.com --json
cocola-sandbox browser screenshot https://example.com --output page.png --json
cocola-sandbox browser screenshot https://example.com --full-page --json
cocola-sandbox browser pdf https://example.com --output page.pdf --json
cocola-sandbox artifact status --json
cocola-sandbox artifact list --json
cocola-sandbox preview start --port 3000 --json -- npm run dev -- --hostname 0.0.0.0
cocola-sandbox preview status --port 3000 --json
cocola-sandbox preview logs --port 3000 --lines 100
cocola-sandbox preview stop --port 3000 --json
```

## Managed user-facing Preview servers

`cocola-sandbox preview start` launches a development server in a separate
process session so it remains available through the Workspace Preview proxy
after the Agent turn completes. It requires a workspace-scoped cwd, removes
run-scoped credentials from the child environment, records PID start identity
to avoid signaling a reused PID, and only reports ready after the port is
reachable on the container network. Startup waits are bounded; a failed or
loopback-only server is stopped and its bounded log remains inspectable.

The built-in `cocola-sandbox-preview` Skill instructs Agent Runtimes to use this
contract instead of Bash background jobs. Managed processes survive an Agent
turn but must be restarted after Sandbox compute loss.

## On-demand headless Browser

Browser commands launch one Playwright persistent context for the duration of
the command and close Chromium before returning. No Browser daemon, CDP port,
visual desktop, or host port is created. Cookie and LocalStorage state lives in
`/session/runtime/browser/profile`, so it survives Sandbox compute reclamation;
screenshots and PDFs default to `/workspace/outputs/browser`.

Only `http://` and `https://` navigation is accepted. Browser output paths are
resolved beneath `/workspace`, Chromium runs as the fixed non-root `cocola`
identity, and Sandbox egress policy remains the network authority. An Agent may
serve local HTML on a temporary loopback HTTP port for interactive inspection.

### Built-in Agent Skill

The image ships versioned, root-owned `cocola-sandbox-browser` and
`cocola-sandbox-artifacts` Skills under `/opt/cocola/skills`. They teach the
Agent when and how to use the stable guest CLI; neither Skill adds a second
execution implementation.

At the beginning of a Run, Agent Runtime inspects the image's platform Skill
manifest and atomically reconciles platform Skills with the effective Admin and
Personal Skill catalog in the Session Volume. The same snapshot is exposed at
`/home/cocola/.claude/skills` and `/home/cocola/.agents/skills`. Platform Skill
IDs are reserved and cannot be shadowed by catalog entries. Because the image
reports its actual inventory, a rolling rollout tolerates old images with no
platform Skills and automatically rebuilds the snapshot when a new image or
built-in Skill version appears.

## Artifact publication and active HTML preview

Changed regular files written beneath `/workspace/outputs` are uploaded by
Agent Runtime after a successful turn and emitted as authenticated,
conversation-owned Artifacts. Symbolic links, linked directories, sockets and
other non-regular entries are ignored. `cocola-sandbox artifact status/list`
lets the Agent inspect the same output contract before it finishes.

Downloads retain attachment and no-store response headers. The Web UI fetches
preview bytes and passes HTML through to a sandboxed iframe that permits scripts,
forms, external resources, popups, modals and downloads. Agent-authored HTML can
therefore load CDN dependencies and execute interactive JavaScript.

Phase 3 intentionally does not add Jupyter, a visual desktop, per-Sandbox
observe endpoints, automatic live-site tabs, or a Sandbox MCP server.

## How the control plane drives the Agent

Agent execution does not expose a server: the control-plane router invokes the
shim over stdio. The optional Code Server binds only its in-container port and
is reachable through Cocola's authenticated OpenSandbox server-proxy; it is
never published as a host port.

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

| Volume directory           | Runtime path           | Content                             |
| -------------------------- | ---------------------- | ----------------------------------- |
| `/session/workspace`       | `/workspace`           | platform files and Project worktree |
| `/session/runtime/claude`  | `/home/cocola/.claude` | Claude session state                |
| `/session/runtime/codex`   | `/home/cocola/.codex`  | Codex session state                 |
| `/session/runtime/cocola`  | `/home/cocola/.cocola` | Cocola Skills and runtime state     |
| `/session/runtime/browser` | internal               | persistent Browser profile/state    |
| `/session/home/local`      | `/home/cocola/.local`  | user-installed tools                |

`/cache` remains ephemeral so package downloads do not consume the default
`2Gi` local-path PVC request. Root-owned platform Skills plus Shared and Personal
Skill bundles are reconciled into the Session Volume's unified Skill Set;
`/data/plugins` is not required for Session recovery. Platform entries remain
links to immutable image assets. Secrets, other rootfs files and the rest of
`$HOME` are never copied into the Session Volume.

Every start also guarantees `/workspace/outputs`,
`/workspace/outputs/browser`, `/workspace/uploads`, and `/workspace/downloads`.
Project runs create their Git worktree separately at `/workspace/project`.
These paths are part of the manifest contract and survive Sandbox compute
reclamation with the workspace.

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
