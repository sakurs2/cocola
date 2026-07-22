# syntax=docker/dockerfile:1
#
# cocola Route-A sandbox runtime image (see ADR-0009).
#
# This image bakes both supported Agent Runtimes into the per-user sandbox:
# Claude Code, Codex, their SDKs, and a thin stdio dispatcher, plus Python/uv
# for user workloads. The selected agent's brain and tools run inside the
# user's own container, so there is no MCP forwarding seam.
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
#   - Persistence is via mounted volumes: /workspace holds user-visible files;
#     /home/cocola/.claude and /home/cocola/.codex hold runtime-native session
#     state on the same session storage unit without appearing in workspace.
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
# Resident in-sandbox editor. Pinned to a known-good standalone release (bundles
# its own Node, so no native build). Offline builds can drop the matching
# code-server-<ver>-linux-<arch>.tar.gz under ./offline/ (see the install step).
ARG CODE_SERVER_VERSION=4.117.0
ARG GH_VERSION=2.94.0

FROM ${OPENSANDBOX_BASE}

ARG COCOLA_VENV_PYTHON
ARG NPM_REGISTRY
ARG CLAUDE_AGENT_SDK_SPEC
ARG CODE_SERVER_VERSION
ARG GH_VERSION

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

# ---- extra tools the base image lacks ------------------------------------
# The base already provides Node 22 + npm, uv, Python 3.12 (+ venv), git, curl.
# cocola adds the pieces agents use frequently: PID-1 reaping, egress firewall
# tooling, search/JSON/YAML helpers, archive/build utilities, Playwright's
# Chromium for HTML preview/screenshot automation, fonts, and common
# document/media CLIs.
# Node/uv/python install steps are GONE -- inherited from base.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates tini supervisor \
        iptables ipset \
        ripgrep fd-find jq yq tree file less procps psmisc \
        wget unzip zip tar gzip zstd make build-essential pkg-config sqlite3 \
        poppler-utils imagemagick librsvg2-bin \
        fonts-noto fonts-noto-cjk fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
    # The base image ships Python only as `python3` (no bare `python`), so agents
    # that invoke `python ...` hit command-not-found and have to retry `python3`.
    # Symlink `python` -> the real python3 on PATH (same idiom as `fd`/`chromium`
    # above) so both spellings just work. We resolve python3 rather than hardcode
    # a path, and assert it exists, so the build fails loudly if the base ever
    # stops providing it instead of baking a dangling link.
    && py3="$(command -v python3)" \
    && test -n "$py3" \
    && ln -sf "$py3" /usr/local/bin/python \
    && npm config set registry "${NPM_REGISTRY}" \
    && npm install -g pnpm@10 yarn playwright \
    && PLAYWRIGHT_BROWSERS_PATH=/ms-playwright playwright install --with-deps chromium \
    && chromium_path="$(find /ms-playwright -type f \( -path '*/chrome-linux/chrome' -o -path '*/chrome-linux64/chrome' \) | head -n1)" \
    && test -n "$chromium_path" \
    && printf '#!/usr/bin/env sh\nexec "%s" "$@"\n' "$chromium_path" > /usr/local/bin/chromium \
    && chmod +x /usr/local/bin/chromium \
    && chmod -R a+rX /ms-playwright

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

# ---- Codex CLI + SDK: exact, lockfile-backed versions --------------------
COPY package.json package-lock.json /opt/cocola/
RUN npm ci --prefix /opt/cocola --omit=dev \
    && ln -sf /opt/cocola/node_modules/.bin/codex /usr/local/bin/codex \
    && codex --version

# ---- GitHub CLI: pinned official binary behind Cocola's auth wrapper ------
# The real binary is immutable and never performs persistent `gh auth login`.
# /usr/local/bin/gh delegates authenticated commands to the run-scoped Cocola
# broker; `gh --version` remains available outside GitHub Project runs.
COPY gh-wrapper.sh /opt/cocola/gh-wrapper.sh
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) gh_arch=amd64; gh_sha=a757f1ba6db18f4de8cbadb244843a5f89bc75b5e7c6fc127d2bd77fbd12ed62 ;; \
        arm64) gh_arch=arm64; gh_sha=705a23b70b0f1b7ba4c302fdcef392ce3edaacfa7ce8e85e4d93d72ea800a538 ;; \
        *) echo "unsupported architecture for gh: $arch" >&2; exit 1 ;; \
    esac; \
    archive="/tmp/gh_${GH_VERSION}_linux_${gh_arch}.tar.gz"; \
    curl -fSL -o "$archive" \
        "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${gh_arch}.tar.gz"; \
    echo "$gh_sha  $archive" | sha256sum -c -; \
    mkdir -p "/opt/cocola/gh/${GH_VERSION}"; \
    tar -C "/opt/cocola/gh/${GH_VERSION}" --strip-components=1 -xzf "$archive"; \
    ln -sfn "/opt/cocola/gh/${GH_VERSION}" /opt/cocola/gh/current; \
    chmod 0755 /opt/cocola/gh-wrapper.sh; \
    ln -sf /opt/cocola/gh-wrapper.sh /usr/local/bin/gh; \
    rm -f "$archive"; \
    /opt/cocola/gh/current/bin/gh --version

# ---- resident code-server (in-sandbox VS Code) ---------------------------
# The browser-facing editor for the Code dock tab. Reached ONLY via the
# OpenSandbox server-proxy (like execd), never published to the host, so it does
# not break "a sandbox never binds a HOST port" (ADR-0009); see
# code-server-launch.sh for the full reconciliation. Installed as a standalone
# release (self-contained Node) to avoid native rebuilds. Offline builds use
# offline/code-server-<ver>-linux-<arch>.tar.gz when present; otherwise the
# release is fetched from GitHub at build time (never at container runtime).
COPY offline/ /tmp/cs-offline/
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) cs_arch=amd64 ;; \
        arm64) cs_arch=arm64 ;; \
        *) echo "unsupported arch for code-server: $arch" >&2; exit 1 ;; \
    esac; \
    tgz="$(ls /tmp/cs-offline/code-server-*-linux-${cs_arch}.tar.gz 2>/dev/null | head -n1 || true)"; \
    if [ -z "$tgz" ]; then \
        echo "no offline code-server tgz; fetching v${CODE_SERVER_VERSION} from GitHub"; \
        tgz=/tmp/code-server.tar.gz; \
        curl -fSL -o "$tgz" \
            "https://github.com/coder/code-server/releases/download/v${CODE_SERVER_VERSION}/code-server-${CODE_SERVER_VERSION}-linux-${cs_arch}.tar.gz"; \
    else \
        echo "installing code-server from offline tgz: $tgz"; \
    fi; \
    mkdir -p /opt/code-server; \
    tar -C /opt/code-server --strip-components=1 -xzf "$tgz"; \
    ln -sf /opt/code-server/bin/code-server /usr/local/bin/code-server; \
    rm -rf /tmp/cs-offline /tmp/code-server.tar.gz; \
    code-server --version

# ---- platform Code Server extensions + language tooling -----------------
# Extensions are exact-version image assets. Universal VSIX files are fetched
# from their pinned Open VSX URLs and checksum-verified; Ruff is selected by
# Open VSX for the build platform because it bundles a native executable. The
# installer verifies the final inventory exactly, so dependency packs or new
# transitive extensions cannot silently enter the image.
#
# Language servers that are not bundled by their extension are installed next
# to the platform extensions. Nothing is downloaded when a sandbox starts.
COPY code-server-extensions.lock.json /opt/cocola/code-server-extensions.lock.json
RUN set -eux; \
    lock=/opt/cocola/code-server-extensions.lock.json; \
    clangd_package="$(jq -er '.language_tools[] | select(.name == "clangd") | .package' "$lock")"; \
    clangd_package_version="$(jq -er '.language_tools[] | select(.name == "clangd") | .package_version' "$lock")"; \
    shellcheck_package="$(jq -er '.language_tools[] | select(.name == "shellcheck") | .package' "$lock")"; \
    shellcheck_package_version="$(jq -er '.language_tools[] | select(.name == "shellcheck") | .package_version' "$lock")"; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        "${clangd_package}=${clangd_package_version}" \
        "${shellcheck_package}=${shellcheck_package_version}"; \
    rm -rf /var/lib/apt/lists/*; \
    mkdir -p /opt/cocola/toolchains/bin /tmp/cocola-go-cache /tmp/cocola-go-mod; \
    gopls_module="$(jq -er '.language_tools[] | select(.name == "gopls") | .module' "$lock")"; \
    gopls_version="$(jq -er '.language_tools[] | select(.name == "gopls") | .version' "$lock")"; \
    shfmt_module="$(jq -er '.language_tools[] | select(.name == "shfmt") | .module' "$lock")"; \
    shfmt_version="$(jq -er '.language_tools[] | select(.name == "shfmt") | .version' "$lock")"; \
    java_version="$(jq -er '.language_tools[] | select(.name == "java") | .version' "$lock")"; \
    GOBIN=/opt/cocola/toolchains/bin \
        GOCACHE=/tmp/cocola-go-cache \
        GOMODCACHE=/tmp/cocola-go-mod \
        GOTOOLCHAIN=auto \
        go install "${gopls_module}@${gopls_version}"; \
    GOBIN=/opt/cocola/toolchains/bin \
        GOCACHE=/tmp/cocola-go-cache \
        GOMODCACHE=/tmp/cocola-go-mod \
        GOTOOLCHAIN=auto \
        go install "${shfmt_module}@${shfmt_version}"; \
    rm -rf /tmp/cocola-go-cache /tmp/cocola-go-mod; \
    ln -sfn "$(command -v clangd-18)" /opt/cocola/toolchains/bin/clangd; \
    ln -sfn "$(command -v shellcheck)" /opt/cocola/toolchains/bin/shellcheck; \
    java_binary="$(readlink -f "$(command -v java)")"; \
    ln -sfn "$(dirname "$(dirname "$java_binary")")" /opt/cocola/toolchains/java; \
    /opt/cocola/toolchains/bin/gopls version; \
    /opt/cocola/toolchains/bin/clangd --version; \
    /opt/cocola/toolchains/bin/shellcheck --version; \
    /opt/cocola/toolchains/bin/shfmt --version; \
    /opt/cocola/toolchains/java/bin/java -version 2>&1 | grep -F "openjdk version \"${java_version}\""; \
    chmod -R u=rwX,go=rX /opt/cocola/toolchains

COPY install-code-server-extensions.sh /opt/cocola/install-code-server-extensions.sh
RUN chmod 0755 /opt/cocola/install-code-server-extensions.sh \
    && /opt/cocola/install-code-server-extensions.sh

# ---- Python agent SDK in a uv-managed venv (matches the shim interpreter) -
# Install into a dedicated venv (uv-managed, per project convention) so the SDK
# never depends on system Python / pip skew. The shim's entrypoint execs this
# venv's python directly, so its version is independent of the base image's.
RUN uv venv --python "${COCOLA_VENV_PYTHON}" /opt/cocola/venv \
    && uv pip install --python /opt/cocola/venv/bin/python --no-cache \
        "${CLAUDE_AGENT_SDK_SPEC}"

# The upstream base installs global npm packages under its versioned Node root
# (/opt/node/<version>/lib/node_modules), while Cocola deliberately exposes the
# stable NODE_PATH below. Resolve the installed Playwright package at build time
# and bridge only that package into the stable path; do not hardcode Node's
# versioned directory in the Browser runner.
RUN playwright_cli="$(readlink -f "$(command -v playwright)")" \
    && test -f "$playwright_cli" \
    && mkdir -p /usr/local/lib/node_modules \
    && ln -sfn "$(dirname "$playwright_cli")" /usr/local/lib/node_modules/playwright \
    && NODE_PATH=/usr/local/lib/node_modules node -e 'require("playwright")'

# ---- egress firewall (ADR-0009 hardening) --------------------------------
# init-firewall.sh installs an iptables+ipset default-DROP egress allowlist; the
# container entrypoint runs it as root at start, BEFORE any user exec lands.
# User code execs as the non-root cocola user (no NET_ADMIN), so it cannot alter
# the rules. Requires the container to be started with CAP_NET_ADMIN, which the
# Docker provider grants only when an egress policy is configured.
COPY init-firewall.sh /opt/cocola/init-firewall.sh
COPY firewall-entrypoint.sh /opt/cocola/firewall-entrypoint.sh
COPY code-server-launch.sh /opt/cocola/code-server-launch.sh
COPY runtime-entrypoint.sh /opt/cocola/runtime-entrypoint.sh
COPY supervisord.conf /opt/cocola/supervisord.conf
COPY runtime-manifest.json /opt/cocola/runtime-manifest.json
COPY cocola_sandbox.py /opt/cocola/cocola_sandbox.py
COPY browser-runner.js /opt/cocola/browser-runner.js
COPY skills/ /opt/cocola/skills/
RUN chmod +x \
        /opt/cocola/init-firewall.sh \
        /opt/cocola/firewall-entrypoint.sh \
        /opt/cocola/code-server-launch.sh \
        /opt/cocola/runtime-entrypoint.sh \
        /opt/cocola/cocola_sandbox.py \
    && ln -sf /opt/cocola/cocola_sandbox.py /usr/local/bin/cocola-sandbox

# ---- the stdio shim ------------------------------------------------------
COPY shim/ /opt/cocola/shim/
RUN chmod +x /opt/cocola/shim/entrypoint.sh

# ---- non-root user + mount points ----------------------------------------
# /session             -> bound onto the SESSION volume (RW); sandbox-manager
#                         links the runtime paths below into its stable layout
RUN useradd -m -u 10001 -s /bin/bash cocola \
    && mkdir -p /session /workspace /cache \
        /home/cocola/.claude /home/cocola/.codex /home/cocola/.cocola \
        /home/cocola/.local/bin \
        /home/cocola/.local/lib/node_modules \
        /home/cocola/.local/share/pnpm \
        /home/cocola/.local/share/man \
        /data/plugins \
    # Keep /opt/cocola root-owned. Supervisor runs as root and may restart the
    # fixed launchers stored there; making those files guest-writable would turn
    # an optional-service restart into a privilege-escalation path.
    && chown -R cocola:cocola /home/cocola /session /workspace /cache

# NOTE: the container's main process runs as ROOT so firewall-entrypoint.sh can
# install the egress firewall (needs NET_ADMIN). This is NOT a privilege
# regression: user/agent code never runs as the container's main process -- it
# arrives via exec (docker exec / kubectl exec), which the sandbox-manager pins
# to the non-root cocola user (uid 10001). The keep-alive entrypoint does
# nothing exploitable.
# OpenSandbox starts bootstrap.sh (and therefore execd) in the image WORKDIR
# before Cocola's custom entrypoint prepares the session symlinks. /workspace
# is replaced with a symlink during that preparation, so using it here leaves
# the already-running execd holding a deleted cwd inode. Keep infrastructure
# daemons rooted at an immutable directory; Cocola Exec requests and
# code-server select /workspace explicitly after it is ready.
WORKDIR /

# CLAUDE_CONFIG_DIR points to a hidden session-local mount outside /workspace so
# memory/sessions are isolated without appearing in user file listings.
ENV CLAUDE_CONFIG_DIR=/home/cocola/.claude \
    ANTHROPIC_CONFIG_DIR=/home/cocola/.claude \
    CODEX_HOME=/home/cocola/.codex \
    HOME=/home/cocola \
    XDG_CACHE_HOME=/cache/xdg \
    UV_CACHE_DIR=/cache/uv \
    PIP_CACHE_DIR=/cache/pip \
    npm_config_cache=/cache/npm \
    npm_config_prefix=/home/cocola/.local \
    PNPM_HOME=/home/cocola/.local/share/pnpm \
    GOBIN=/home/cocola/.local/bin \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/cache/go-mod \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
    CLAUDE_CODE_MAX_RETRIES=3 \
    COCOLA_WORKSPACE=/workspace \
    COCOLA_CODE_SERVER_PORT=39378 \
    JAVA_HOME=/opt/cocola/toolchains/java \
    NODE_PATH=/usr/local/lib/node_modules \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    CHROME_BIN=/usr/local/bin/chromium \
    PUPPETEER_EXECUTABLE_PATH=/usr/local/bin/chromium \
    PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/local/bin/chromium \
    PATH=/opt/cocola/toolchains/bin:/home/cocola/.local/bin:/home/cocola/.local/share/pnpm:$PATH

# tini reaps the `claude` CLI subprocesses the SDK spawns. Default command keeps
# the container alive (session-lived per ADR-0009); the shim is invoked on
# demand via `docker exec ... /opt/cocola/shim/entrypoint.sh`. Both ENTRYPOINT
# and CMD override the base image's Jupyter launcher. The optional Code Server
# binds only its in-container port and is never published to the host.
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/opt/cocola/runtime-entrypoint.sh"]
