# ADR-0009: Run the Claude Code agent runtime *inside* each user's sandbox

- Status: Accepted (Route A 已落地，见文末「实现进展」；Route B 保留为 fallback)
- Date: 2026-06-10 (accepted 2026-06-11)
- Deciders: @wangjiahui
- Revises: ADR-0007 (gateway BFF + agent-runtime gRPC), ADR-0004 (LLM gateway proxy)

## Context

cocola's goal is an **enterprise, self-hosted, Mira-like** agent platform: the
company deploys it once, employees open a web page, and **nothing is installed on
the employee's machine**. Claude Code therefore lives entirely on the server
side — this goal is already met today (the employee only ever talks to the web
app over HTTP/SSE).

Through M5, cocola placed the Claude Agent SDK (and the `claude` CLI subprocess
it spawns) in a **central, multi-tenant `agent-runtime` process**. The agent's
"brain" (the ReAct loop, context, `~/.claude` memory) runs on that shared host;
only its "hands" reach a sandbox, via an in-process MCP server
(`sandbox_tools.py`) that forwards `bash` / `read_file` / `write_file` calls into
the session's bound container. Call this **Route B: brain in the centre, hands in
the sandbox**.

Reviewing two reference write-ups — Tencent's CubeSandbox and an internal
Mira/ByteDance discussion of how Claude Code is operated server-side — surfaced
that the industry converges on a different shape for multi-tenant agents, which
we call **Route A: the whole runtime runs inside each user's sandbox**. Mira's
Cowork mode "is basically vanilla Claude Code" running per-user in a sandbox.

Route B has three structural problems for an enterprise multi-tenant target:

1. **Shared-host blast radius.** One `agent-runtime` process spawns N users'
   `claude` CLI children. A crash, a noisy neighbour, or a session-id collision
   in the in-process registry affects unrelated users.
2. **`~/.claude` isolation is a patch, not a property.** Memory/config/sessions
   live on the shared host; we isolate them only with a per-invocation temp
   `CLAUDE_CONFIG_DIR`. This is the root cause of the 503 settings.json leak we
   already hit (the host's global `~/.claude/settings.json` env block overriding
   our injected `ANTHROPIC_BASE_URL`).
3. **The MCP forwarding layer is a seam holding a split architecture together,
   and it is leaky.** We allow-list only the three MCP tools, but we do **not**
   disable Claude Code's *native* Bash/Read/Write/Edit/Glob. Those default to
   executing on the **agent-runtime host's** filesystem. Without an explicit
   `disallowed_tools` + pinned `permission_mode`, the model can bypass the
   sandbox entirely and run untrusted code on the shared host. "Looks isolated,
   brain actually exposed."

## Decision

**Adopt Route A: package the Claude Code runtime (Node + `claude` CLI +
claude-agent-sdk + a thin shim) into the per-user sandbox image. The brain and
the hands both run inside the user's own container.** `agent-runtime` degrades
from "the process that runs the agent" to a **control-plane router**: it
authenticates, resolves `user_id`/`session_id` → sandbox, ensures the sandbox is
up, forwards the prompt to the in-sandbox shim, and relays the event stream back
to the gateway. It no longer spawns `claude` itself.

Consequences of the brain moving into the sandbox:

### 1. Native tools are now safe — delete the MCP forwarding seam

With the CLI running inside the user's own container, Claude Code's **native**
Bash/Read/Write/Edit/Glob execute against *that user's own* filesystem — they
are naturally isolated, and one user's command cannot touch another's. Therefore:

- **`sandbox_tools.py` (the in-process MCP forwarding server) is removed.** It
  was a Route-B stitch; Route A makes it dead weight.
- The agent runs with the **full native Claude Code toolset** (Edit, Glob, Grep,
  WebFetch, Task subagents, …) — i.e. "vanilla Claude Code" fidelity, not three
  hand-rolled MCP tools.
- The Route-B emergency patch (adding `disallowed_tools` to fence off native
  execution) is **moot and will not be implemented**.

### 2. The security boundary moves from "tool allow-list" to "network egress"

Route A injects credentials (the cocola-issued token, `ANTHROPIC_BASE_URL`) into
the sandbox env, and the sandbox must reach the llm-gateway over the network.
Since untrusted code now runs *next to* those credentials, the containment story
becomes **the sandbox is the blast radius, bounded by the network**:

- The sandbox's **egress is locked down to an allowlist** — only the llm-gateway
  (and required internal services); **no arbitrary public internet**. This
  reuses the `Networking.EgressAllowlist` already on `SandboxSpec`.
- This is a *cleaner* model than Route B: we stop relying on "the model won't
  call native bash" and instead enforce a hard network perimeter.

### 3. The CLI is pre-baked into the image, never installed at runtime

Per the reference practice, the base image bakes in Node.js + Claude Code (via an
offline `npm pack` tgz to survive networks without registry access) + Python (uv)
+ the shim. Sandboxes are ready on start; Claude Code upgrades = swap the tgz,
rebuild the image, upper layers untouched. The shim wraps `query()` /
`ClaudeSDKClient` so the control plane talks to it over a defined RPC rather than
driving the non-streaming CLI directly.

### 4. Lifecycle = lazy-start + session binding + idle hibernate (see ADR-0008)

Because the brain is stateful and lives in the container, the container **must**
be at least session-lived — per-command "run then release" is incompatible with
Route A and is explicitly rejected (it only fits a stateless executor where the
brain is outside). Idle cost is handled by hibernate/resume, detailed in the
updated ADR-0008.

## Alternatives Considered

- **Stay on Route B, just patch the holes.** Add `disallowed_tools`, pin
  `permission_mode`, keep per-invocation `CLAUDE_CONFIG_DIR` + an M7 per-user
  volume. Pros: smallest diff, keeps the lightweight sandbox image, and keeps LLM
  credentials *out* of the untrusted-code environment (a genuine security plus).
  Cons: isolation stays a patchwork; shared-host blast radius and noisy-neighbour
  remain; we run a "thin custom agent", not vanilla Claude Code, so fidelity and
  the native toolset are permanently reduced; every Claude Code upgrade risks
  re-opening the native-tool bypass. Rejected for the enterprise multi-tenant
  target, where isolation must be a property, not a patch.
- **Per-command ephemeral sandbox (brain outside, stateless executor).** The E2B
  / code-interpreter shape. Rejected: incompatible with a stateful in-sandbox
  brain; and even CubeSandbox's own guidance is to bind a sandbox to the
  *session*, not a single tool call, "otherwise you lose state and thrash on
  start/stop".
- **Dual-mode (thin agent for chat + vanilla Claude Code for cowork), like
  Mira.** Attractive long-term and not precluded — the control plane stays the
  same. Deferred: we standardise on Route A first and may add a thin-agent lane
  later, rather than carrying two runtimes from day one.

## Consequences

- **Positive** — isolation, memory, and native-tool fidelity become *structural*
  (one container per user/session); the settings.json leak disappears by
  construction; the MCP forwarding seam is deleted (less code); the agent gains
  the full native Claude Code toolset; the security model is a clean network
  perimeter.
- **Negative / accepted risk** — the sandbox image gets heavy (Node + Claude Code
  + Python), so cold start grows from container-spawn to seconds; mitigated by
  the lazy-start + hibernate strategy in ADR-0008 (the warm-pool part was later
  removed — see ADR-0016; on-demand cold-start is the only allocation path). Credentials now
  live inside the untrusted-code container, so **egress lockdown is mandatory,
  not optional**. `agent-runtime` must be rebuilt as a router (revises ADR-0007),
  and llm-gateway must accept traffic originating from sandboxes (revises
  ADR-0004's trust assumptions).
- **Follow-ups:**
  - Build the per-user sandbox base image (Node + Claude Code offline tgz + uv +
    shim); pin the shim's RPC contract.
  - ~~Rework `agent-runtime` into a control-plane router; delete `sandbox_tools.py`
    and the Route-B MCP path in `claude_sdk_provider.py`.~~ ✅ 已落地（router
    自 Route A 起即成立；MCP 转发缝合层已于 2026-06-15 删除，见下「实现进展」）。
  - ~~Enforce `Networking.EgressAllowlist` end-to-end (sandbox → llm-gateway only).~~
    ✅ 已落地，见下「实现进展（2026-06-15）egress 硬化」。
  - Run a gVisor (runsc) compatibility spike for Node/Claude Code before full
    build-out (see ADR-0008 backend choice).

## 实现进展（2026-06-11）

Route A 已在本地全栈（docker-compose.full）端到端落地并通过真实模型验证：

- **沙箱基础镜像** `cocola/sandbox-runtime:dev`：Node + claude CLI +
  claude-agent-sdk + stdio shim（`/opt/cocola/shim/entrypoint.sh`）。已就绪。
- **agent-runtime 控制面路由**：`COCOLA_AGENT_ROUTE=A` 启用
  `InSandboxShimProvider`，经 `exec_stream` 把 prompt 喂给沙箱内 shim、把
  NDJSON 事件流回传。Route B（`ClaudeAgentSDKProvider`）当时保留为最小 fallback，
  其 MCP 转发缝合层已删除（见 2026-06-15 进展）；Route B 本身已于 2026-07-02
  正式下线（见下「实现进展（2026-07-02）下线 Route B」），现仅 Route A 一条真实路径。
- **凭证注入**：composition root 把沙箱镜像 + `ANTHROPIC_BASE_URL/AUTH_TOKEN/
  MODEL` 经 SandboxSpec 注入沙箱 ENV（绝不走 prompt 通道）。沙箱内 CLI 经宿主
  发布端口回连 llm-gateway 出网。
- **已验证**：Web 对话、纯文本回复、原生 Bash 工具调用、真实
  `/v1/messages` 出网计费，均正常。

仍未做（保持本 ADR 的 Follow-ups 状态）：

- ~~**egress allowlist 尚未强制**（沙箱目前可任意出网）—— 上线前必做的硬化项。~~ ✅ 已完成（见下）。
- ~~`sandbox_tools.py` 与 Route-B MCP 路径**尚未删除**（仍作为 fallback 保留）。~~ ✅ 已删除（2026-06-15，见下）。
- gVisor 兼容性 spike、K8s provider（M6）未开始。

## 实现进展（2026-06-15）删除 Route-B MCP 转发缝合层

落地本 ADR §1「Native tools are now safe — delete the MCP forwarding seam」：

- **删除** `apps/agent-runtime/cocola_agent_runtime/sandbox_tools.py`（in-process
  MCP 转发服务器）及其单测 `tests/test_sandbox_tools.py`。
- **`claude_sdk_provider.py`** 去缝合：`ClaudeAgentSDKProvider.__init__` 移除
  `executor` 形参；`_build_options()` 移除「挂载 `cocola_sandbox` MCP server +
  `allowed_tools`」分支。Provider 现在只携内置工具集。
- **`__main__.py`**：Route-B 分支改为 `ClaudeAgentSDKProvider(cfg)`（不再传
  executor）。`_build_executor()` 保留——仍供 Route A 的 `InSandboxShimProvider`
  经 `exec_stream` 驱动沙箱内大脑。
- **范围抉择**：本次为外科式删缝合层。`ClaudeAgentSDKProvider` 本身**保留**为最小
  fallback（零沙箱 dev boot、llm-gateway JWT 透传契约测试当时仍依赖它），符合本
  ADR §1 只点名删除「MCP 缝合层」的授权边界。收敛为 Route A 单路径（连带删 provider /
  `COCOLA_AGENT_ROUTE` 开关 / compose / run-stack）已作为独立后续任务于 2026-07-02
  落地（见下）。
- `disallowed_tools` 补丁按 §1 判定 moot，确认从未实现、本次不引入。
- 验收：agent-runtime `ruff` + `pytest`（48 passed, 2 skipped）全绿；代码内零
  `sandbox_tools` 残引用。

## 实现进展（2026-06-15）egress 硬化

ADR 第 2 节「安全边界从 tool allowlist 迁移到 network egress」的强制项已落地，
计划见 `docs/plan/hardening-sandbox-egress-allowlist.md`，分 S1–S4 提交：

- **S1 编排层注入**（`internal/orchestrator/networking.go`）：`NetworkingFromEnv`
  从 `COCOLA_SANDBOX_EGRESS_ALLOWLIST` 读取 allowlist，并**始终**并入
  `COCOLA_SANDBOX_LLM_BASE_URL` 的 gateway host，经 `Binder` 透传进 provider。
- **S2 Docker 强制**（`provider/docker` + `deploy/sandbox-runtime`）：复用 iptables+ipset
  「默认 DROP + allowlist」模式（与 Anthropic Claude Code devcontainer init-firewall
  同源）。容器主进程以 root 装防火墙，用户/agent 代码经 exec 钉到非 root `cocola`。
- **S3 K8s 收口**（`provider/k8s`）：原生 `NetworkPolicy`，DNS + 集群内 llm-gateway
  基线**始终放行**，CIDR/IP 作 ipBlock 追加；域名需 DNS-aware CNI（Cilium `toFQDNs`，
  已在 Helm values 留扩展点）。
- **统一语义**：`Networking.EgressAllowlist` 为 nil = 未配置策略（遗留全开）；
  非 nil（含空）= 防火墙生效、基线放行 DNS+gateway、其余 DROP。因编排层恒并入
  gateway host，实际部署下 allowlist 恒非空，沙箱默认即被锁定到「仅 DNS+gateway」。

端到端实跑验证（沙箱内 curl gateway 通 / curl 任意公网被拒）需 Linux+Docker 或
K8s 集群环境，随本批次合并后在目标机执行（开发机为 macOS）。

## 实现进展（2026-07-02）下线 Route B

Route A 已在 opensandbox 后端全栈端到端验证成功（Web 对话、原生工具、真实
`/v1/messages` 出网计费均正常），本次将 Route B（中心化 SDK 路径）正式下线，
agent-runtime 收敛为 Route A 单路径。计划见 `docs/plan/hardening-route-b-fallback-cleanup.md`
（本次执行取代其「保留 provider」的保守边界）。

- **删除实现与单测**：`apps/agent-runtime/cocola_agent_runtime/claude_sdk_provider.py`
  及 `tests/test_claude_sdk_provider.py`（纯 provider 映射单测，随代码走）。
- **`_build_provider` 塌缩为两层**：有 sandbox executor（`COCOLA_SANDBOX_ADDR`）
  → `InSandboxShimProvider`（Route A）；否则 → `EchoProvider`（零配置、无模型调用，
  仍走通 gRPC 契约）。删除 `COCOLA_LLM_BASE_URL` / `ClaudeAgentSDKProvider` 分支。
- **移除 `COCOLA_AGENT_ROUTE` 开关**：`__main__.py` 不再读取它（executor 是否存在
  即决定路径）；`docker-compose.full.yml`、`scripts/run-stack.sh` 中的该开关与
  Route-B 降级措辞一并清理。
- **删除依赖旧 provider 的 e2e**：`scripts/llm-m3-e2e.py`、`scripts/llm-m4-e2e.py`、
  `apps/llm-gateway/tests/test_token_passthrough_e2e.py`。其中 token 透传 / 401 / 429
  / 计费归属这条 gateway 契约已由 provider 无关的 `test_server_auth_quota.py` 完整覆盖，
  删除不丢覆盖率（旧 e2e 只是碰巧拿 `ClaudeAgentSDKProvider` 当测试载体）。
- **注释收敛**：`server.py` / `agent_provider.py` / `skill_loader.py` / `shim_provider.py`
  / `deploy/sandbox-runtime/shim/agent_shim.py` / `llm-gateway auth/identity.py` 中对
  `ClaudeAgentSDKProvider` 的命名引用改为中性表述（Route A / 通用 provider）。
- **回滚性**：Route B 的一键回滚（`unset COCOLA_AGENT_ROUTE`）随本次下线不再适用；
  回滚需 revert 本次提交。这是 ADR-0009 采纳 Route A 为唯一路径后的预期收敛。

changelog：`docs/archive/refactor-decommission-route-b.md`。
