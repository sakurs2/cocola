# syntax=docker/dockerfile:1
#
# cocola Route-A sandbox runtime image (see ADR-0009).
#
# This image bakes the *whole* Claude Code agent runtime into the per-user
# sandbox: Node.js + the `claude` CLI + claude-agent-sdk + a thin stdio shim,
# plus Python/uv for user workloads. The agent's brain AND hands both run here,
# inside the user's own container -- native Bash/Read/Write/Edit are naturally
# isolated to this filesystem, so there is no MCP forwarding seam.
#
# Design rules honoured by this image:
#   - The base is OpenSandbox's official `code-interpreter` image (Node 22 + uv
#     + Python already inside), reused per cocola's "prefer upstream over
#     reinventing the wheel" rule. cocola layers only what the base lacks.
#   - The CLI is PRE-BAKED, never installed at runtime (ADR-0009 sec.3). An
#     offline `npm pack` tgz under ./offline/ is used when present, so the build
#     survives networks with no registry access; otherwise it falls back to the
#     configured registry. (Base inheritance does NOT relax this: claude is
#     baked at build time, not installed on container start.)
#   - The shim is driven over STDIO (one request -> streamed NDJSON events ->
#     exit). It does NOT open a listening socket. The control-plane (agent-
#     runtime router) talks to it via `docker exec` / `kubectl exec` stdio.
#     This matches cocola's hard rule: a sandbox never binds a network port.
#     (The base image's default entrypoint would start a Jupyter server; cocola
#     OVERRIDES both ENTRYPOINT and CMD below, so that never runs.)
#   - Persistence is via mounted volumes (ADR-0008): CLAUDE_CONFIG_DIR lives on
#     the per-user volume so `~/.claude` (memory/sessions/projects) survives
#     hibernate; the session workspace is a separate mounted volume.
#
# Build:
#   # optional: vendor the CLI for offline builds
#   npm pack @anthropic-ai/claude-code --registry=https://registry.npmmirror.com
#   mv anthropic-ai-claude-code-*.tgz deploy/sandbox-runtime/offline/
#   docker build -t cocola/sandbox-runtime:dev deploy/sandbox-runtime
#
# Build args let CI pin versions and swap the npm registry / base image for
# offline mirrors. The base is OpenSandbox's official code-interpreter image,
# which already ships Node 22, uv and Python -- reused per cocola's "prefer
# upstream over reinventing" rule (see Plan + ADR-0014). Claude Code itself is
# still PRE-BAKED at build time (never installed at runtime), so ADR-0009
# sec.3 still holds.
ARG OPENSANDBOX_BASE=opensandbox/code-interpreter:v1.1.0
# NB: the base image sets ENV PYTHON_VERSION=3.14, which would shadow a same-
# named ARG at RUN expansion -- so the venv Python arg is deliberately named
# COCOLA_VENV_PYTHON to stay deterministic regardless of the base default.
ARG COCOLA_VENV_PYTHON=3.12
ARG NPM_REGISTRY=https://registry.npmjs.org
# Pin a known-good agent SDK range. >=0.2.x is not on PyPI yet; 0.1.x is.
ARG CLAUDE_AGENT_SDK_SPEC=claude-agent-sdk>=0.2.0

FROM ${OPENSANDBOX_BASE}

ARG COCOLA_VENV_PYTHON
ARG NPM_REGISTRY
ARG CLAUDE_AGENT_SDK_SPEC

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

# ---- extra tools the base image lacks ------------------------------------
# The base already provides Node 22 + npm, uv, Python 3.12 (+ venv), git, curl.
# cocola only needs to add the pieces the base omits: tini (PID-1 reaper), the
# egress-firewall toolchain (iptables/ipset) and the agent's search helpers
# (ripgrep/jq). Node/uv/python install steps are GONE -- inherited from base.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates tini iptables ipset ripgrep jq \
    && rm -rf /var/lib/apt/lists/* \
    && npm config set registry "${NPM_REGISTRY}"

# ---- Claude Code CLI: offline tgz first, registry fallback --------------
# Anything matching offline/*claude-code*.tgz is installed without the network;
# if absent, install from the configured registry. Either way the CLI is baked
# into the image -- a sandbox never installs it at runtime.
COPY offline/ /tmp/offline/
RUN set -eux; \
    tgz="$(ls /tmp/offline/*claude-code*.tgz 2>/dev/null | head -n1 || true)"; \
    if [ -n "$tgz" ]; then \
        echo "installing Claude Code CLI from offline tgz: $tgz"; \
        npm install -g "$tgz"; \
    else \
        echo "no offline tgz; installing Claude Code CLI from ${NPM_REGISTRY}"; \
        npm install -g @anthropic-ai/claude-code; \
    fi; \
    rm -rf /tmp/offline; \
    claude --version

# ---- Python agent SDK in a uv-managed venv (matches the shim interpreter) -
# Install into a dedicated venv (uv-managed, per project convention) so the SDK
# never depends on system Python / pip skew. The shim's entrypoint execs this
# venv's python directly, so its version is independent of the base image's.
RUN uv venv --python "${COCOLA_VENV_PYTHON}" /opt/cocola/venv \
    && uv pip install --python /opt/cocola/venv/bin/python --no-cache "${CLAUDE_AGENT_SDK_SPEC}"

# ---- egress firewall (ADR-0009 hardening) --------------------------------
# init-firewall.sh installs an iptables+ipset default-DROP egress allowlist; the
# container entrypoint runs it as root at start, BEFORE any user exec lands.
# User code execs as the non-root cocola user (no NET_ADMIN), so it cannot alter
# the rules. Requires the container to be started with CAP_NET_ADMIN, which the
# Docker provider grants only when an egress policy is configured.
COPY init-firewall.sh /opt/cocola/init-firewall.sh
COPY firewall-entrypoint.sh /opt/cocola/firewall-entrypoint.sh
RUN chmod +x /opt/cocola/init-firewall.sh /opt/cocola/firewall-entrypoint.sh

# ---- the stdio shim ------------------------------------------------------
COPY shim/ /opt/cocola/shim/
RUN chmod +x /opt/cocola/shim/entrypoint.sh

# ---- non-root user + mount points (ADR-0008 dual volume) -----------------
# /home/cocola/.claude  -> bound onto the PER-USER volume (T2, cross-session)
# /workspace            -> bound onto the SESSION volume (T1b, hibernate-safe)
RUN useradd -m -u 10001 -s /bin/bash cocola \
    && mkdir -p /home/cocola/.claude /workspace \
    && chown -R cocola:cocola /home/cocola /workspace /opt/cocola

# NOTE: the container's main process runs as ROOT so firewall-entrypoint.sh can
# install the egress firewall (needs NET_ADMIN). This is NOT a privilege
# regression: user/agent code never runs as the container's main process -- it
# arrives via exec (docker exec / kubectl exec), which the sandbox-manager pins
# to the non-root cocola user (uid 10001). The keep-alive entrypoint does
# nothing exploitable.
WORKDIR /workspace

# CLAUDE_CONFIG_DIR points at the per-user volume so memory/sessions persist.
ENV CLAUDE_CONFIG_DIR=/home/cocola/.claude \
    ANTHROPIC_CONFIG_DIR=/home/cocola/.claude \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
    CLAUDE_CODE_MAX_RETRIES=3 \
    COCOLA_WORKSPACE=/workspace

# tini reaps the `claude` CLI subprocesses the SDK spawns. Default command keeps
# the container alive (session-lived per ADR-0009); the shim is invoked on
# demand via `docker exec ... /opt/cocola/shim/entrypoint.sh`. Both ENTRYPOINT
# and CMD override the base image's Jupyter launcher, so no port is ever bound.
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/opt/cocola/firewall-entrypoint.sh"]
