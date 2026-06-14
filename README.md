# cocola

> Open-source enterprise AI Agent platform — self-host your own agents, bring your own tokens, let your team chat for free.

`cocola` 是一个面向企业内部部署的 Agent 平台。核心定位：

- **企业自部署**：完全私有化部署，数据不出企业
- **统一计费**：企业统一采购 LLM Token，员工使用零成本
- **可定制业务逻辑**：登录鉴权、敏感词、Skill Market 等可插拔
- **沙箱执行**：基于云沙箱安全执行 Agent 产生的代码与命令

## 技术栈

- **Agent 核心**：Claude Code Agent SDK
- **沙箱**：K8s（默认 runc + 用户命名空间，零节点安装;gVisor 为可选增强;可通过 `SandboxProvider` 抽象切换至 Docker / E2B / CubeSandbox）
- **后端**：Go（Gateway / Sandbox Manager / Admin API）+ Python（Agent Runtime / LLM Gateway）
- **前端**：Next.js (App Router) + Tailwind CSS 3 + TypeScript
- **存储**：PostgreSQL + Redis + S3-compatible（MinIO）+ NFS/CephFS + HashiCorp Vault
- **通信**：服务间 gRPC，对前端 SSE/WebSocket

## 仓库结构

```
cocola/
├── apps/                     # 业务应用
│   ├── web/                  # Next.js 前端
│   ├── gateway/              # Go: API 网关 (BFF + Auth)
│   ├── sandbox-manager/      # Go: 沙箱编排
│   ├── agent-runtime/        # Python: Agent 运行时
│   ├── llm-gateway/          # Python: LLM 网关与计费
│   └── admin-api/            # Go: 管理后台后端
├── packages/                 # 共享库
│   ├── proto/                # gRPC IDL
│   ├── go-common/            # Go 公共库
│   ├── py-common/            # Python 公共库
│   └── ts-common/            # TS 公共库
├── deploy/                   # 部署清单
│   ├── docker-compose/       # 本地开发
│   ├── k8s/                  # 原生 K8s YAML
│   └── helm/                 # Helm Chart
├── docs/
│   ├── adr/                  # 架构决策记录
│   ├── api/                  # API 文档
│   └── plugin-dev/           # 二次开发指南
├── scripts/                  # 构建/运维脚本
├── Makefile
└── README.md
```

## 快速开始（M0 阶段）

```bash
# 一键拉起本地依赖：PostgreSQL + Redis + MinIO
make dev-up

# 关闭
make dev-down
```

### 一键拉起整套本地应用栈

依赖（数据库等）用 `make dev-up`；**应用进程**用 `make up`。后者由
`scripts/run-stack.sh` 编排，零配置即拉起 agent-runtime + gateway（回退
EchoProvider），自动等待端口就绪、签发一个开发令牌、并在 `Ctrl-C` 时一次性
回收所有子进程（`trap cleanup`）。各服务日志落在 `.run-logs/<服务名>.log`。

```bash
make up        # agent-runtime + gateway（Echo，零配置，最快验证 SSE 链路）
make up-web    # + 前端浏览器测试页（:3000，直接粘贴打印出的令牌）
make up-all    # + llm-gateway（真实 Claude Agent SDK 链路）+ 前端
```

> 端口约定：gateway BFF 与 llm-gateway 默认都监听 `8080`，编排脚本把
> llm-gateway 改钉到 `8081`（`COCOLA_LLM_PORT`）并让 agent-runtime 的
> `COCOLA_LLM_BASE_URL` 指向它，规避端口冲突。需要绑定沙箱时先导出
> `COCOLA_SANDBOX_ADDR`，脚本会透传给 agent-runtime。

#### 接入你购买的模型(全链路真实测试)

`make up`(Echo)不需要任何模型。要跑真实模型,把上游配置写进仓库根的 `.env`
(已被 `.gitignore` 忽略),`run-stack.sh` 启动时会自动加载它:

```bash
cp .env.example .env
# 编辑 .env:填入你购买的 Anthropic 兼容服务
#   COCOLA_LLM_PROVIDER=anthropic
#   COCOLA_ANTHROPIC_BASE_URL=https://你的服务域名
#   COCOLA_ANTHROPIC_API_KEY=sk-ant-xxxx
make up-all      # 自动拉起 llm-gateway 接到你的上游 + 真实 SDK 链路 + 前端
```

需要多模型 / 别名与真实模型解耦 / 自定义计费时,改用配置文件:复制
`deploy/llm-config.example.json` 为 `deploy/llm-config.json`,在 `.env` 里设
`COCOLA_LLM_CONFIG=deploy/llm-config.json`。密钥始终走 `api_key_env` 间接引用,
绝不写进配置文件(ADR-0004 硬约束)。

> 鉴权闭环:`run-stack.sh` 在真实 LLM 模式下,会把 `admin-mint` 签出的令牌同时
> 注入 agent-runtime 作为 SDK 的 `ANTHROPIC_API_KEY`——网关用同一个
> `COCOLA_AUTH_SECRET` 离线校验它并按令牌主体计费,无需手动配 key。agent 发给
> 网关的模型别名固定为 `cocola-default`(与 env / 文件两种配法注册的 route 对齐)。

> 当前里程碑：**Route A 真实模型全链路打通** — 已完成 M0–M5、后端 MVP，并落地
> ADR-0009 的 Route A（Claude Code 大脑进沙箱），接入真实模型，Web 端对话与原生
> 工具调用端到端可用。Go 控制面（admin-api）签发 / 吊销 cocola
> 签发的令牌（即 Claude Agent SDK 的 `ANTHROPIC_API_KEY`），并按周期 token 配额
> 限流。详见 `docs/adr/`。
>
> 为员工签发令牌（令牌即 SDK 的 API Key）：
>
> ```bash
> export COCOLA_AUTH_SECRET=<gateway 与签发方共享的密钥>
> python -m cocola_llm_gateway.issue_token --user emp-12345 --tenant team-platform --ttl-days 30
> # 启用配额：COCOLA_QUOTA_USER_DAILY_TOKENS / COCOLA_QUOTA_TENANT_MONTHLY_TOKENS
> ```
>
> M5 起，同一套令牌也可由 Go 控制面（admin-api）通过 HTTP 签发 / 吊销，并管理动态配额覆盖与 Skill 目录：
>
> ```bash
> export COCOLA_AUTH_SECRET=<与 gateway 共享的密钥>  # 令牌签发；缺省则令牌端点 400
> export COCOLA_ADMIN_KEY=<管理面访问密钥>            # 缺省则管理鉴权关闭（仅本地/CI）
> go run ./apps/admin-api/cmd/admin-api               # 默认监听 :8090
> # 跨语言互通校验（Go 签发 -> Python 校验）：
> apps/llm-gateway/.venv/bin/python scripts/admin-m5-e2e.py
> ```
>
> **后端 MVP（端到端链路）**：前端 → gateway（BFF，HTTP/SSE + 令牌校验）→
> agent-runtime（gRPC）→ llm-gateway / sandbox-manager → 事件流式回传。
> agent-runtime 暴露 `AgentRuntimeService.Query` 服务端流式 RPC；gateway 作为
> BFF 校验 cocola 令牌（复用 `packages/go-common/token` 同一套 HS256 编解码，
> 与 admin-api 共享，不重复造轮子），并把 Agent 事件以 SSE 推给浏览器。
>
> ```bash
> # 启动 agent-runtime gRPC 服务（缺省 :50061；未配置 LLM 时回退 EchoProvider）
> cd apps/agent-runtime && uv run python -m cocola_agent_runtime
> # 启动 gateway BFF（缺省 :8080，转发至 127.0.0.1:50061）
> export COCOLA_AUTH_SECRET=<与签发方共享的密钥>   # 缺省则鉴权关闭（仅本地）
> go run ./apps/gateway/cmd/gateway
> # 发起一次对话（SSE 流式返回）
> curl -N -X POST localhost:8080/v1/chat \
>   -H "authorization: Bearer <令牌>" \
>   -H "content-type: application/json" \
>   -d '{"prompt":"hello","session_id":"s1"}'
> ```
>
> **接入真实 LLM 链路**：给 agent-runtime 设置 `COCOLA_LLM_BASE_URL`，它即从
> EchoProvider 切换到 `ClaudeAgentSDKProvider`，把 Claude Agent SDK 指向 cocola
> 的 llm-gateway（`ANTHROPIC_BASE_URL`），并把 cocola 令牌作为 SDK 的
> `ANTHROPIC_API_KEY` 注入子进程。**校验令牌与 SDK 令牌是同一个**——网关收到
> SDK 发来的 `x-api-key` 后离线校验、按令牌主体计费，M4 的设计在此收口。纯环境
> 变量注入，零 SDK 改动。
>
> ```bash
> # agent-runtime 接到 llm-gateway（缺省 :8080），令牌即 SDK 的 API Key
> export COCOLA_LLM_BASE_URL=http://127.0.0.1:8080
> export COCOLA_AGENT_API_KEY=<cocola 签发令牌>
> export COCOLA_ANTHROPIC_MODEL=default        # 网关 registry 解析为真实模型
> cd apps/agent-runtime && uv run python -m cocola_agent_runtime
> # llm-gateway 侧用真实上游：COCOLA_LLM_PROVIDER=anthropic + COCOLA_ANTHROPIC_API_KEY
> ```
>
> 令牌透传契约由 `apps/llm-gateway/tests/test_token_passthrough_e2e.py` 无网络
> 验证：真实 provider 用 `_build_env()` 注入的令牌驱动网关 ASGI（FakeUpstream），
> 响应经 provider 映射回 `AgentEvent` 流回。
>
> **绑定会话沙箱**：给 agent-runtime 设置 `COCOLA_SANDBOX_ADDR` 指向
> sandbox-manager,`Query` 即在会话首次进入时 `Acquire` 一个沙箱(create-or-reuse
>
> - 续租,M2 闭环),把真实 `sandbox_id` 注入会话并以 `sandbox` 事件流式回传供前端
>   观测。调用方显式传入 `sandbox_id` 时按原样尊重;绑定失败则以终止 `error` 事件
>   结束、Agent 不在缺沙箱时运行。未配置则会话不绑定沙箱(零配置启动)。
>
> ```bash
> export COCOLA_SANDBOX_ADDR=127.0.0.1:50051   # sandbox-manager gRPC 地址
> ```
>
> **让沙箱真正被用起来**:同一个 `COCOLA_SANDBOX_ADDR` 还会把 Agent 的
> bash / 文件读写工具落到所绑定的沙箱里执行——通过 Claude Agent SDK 的进程内
> MCP 机制(`create_sdk_mcp_server`,无子进程、无端口)注册 `bash`/`read_file`/
> `write_file` 三个工具,handler 经 `SandboxExecutor`(anyio 桥接既有
> `SandboxClient` 的 Exec/Read/WriteFile)在会话绑定的 `sandbox_id` 上运行。命令
> 跑通但非零退出会把退出码与 stdout/stderr 交还模型自行决断(类真实 shell);
> 仅沙箱级失败才算工具错误。仅在「有 executor 且会话已绑定沙箱」时挂载,避免
> Agent 持有指向空的工具。

> **前端最小聊天测试页(仅测试工具)**:`apps/web` 提供一个最小流式聊天页,
> 用途是从浏览器端到端验证后端链路,**不是产品 UI**(产品前端待后端完全稳定后
> 再打造)。页面用 `fetch` + `ReadableStream` 手动解析 SSE(网关是 `POST` + Bearer
> 令牌,`EventSource` 只能 GET 且不能带头,故不可用)。由于网关不返回 CORS 头,
> 页面经同源 Next.js 路由 `app/api/chat/route.ts` 反代到网关 `POST /v1/chat`,
> 把 SSE 原样透传;网关地址由服务端 `COCOLA_GATEWAY_URL` 配置,不暴露给浏览器。
>
> ```bash
> # 先起 agent-runtime 与 gateway(见上),再起前端
> export COCOLA_GATEWAY_URL=http://127.0.0.1:8080   # route.ts 反代目标(缺省即此)
> pnpm install
> pnpm --filter @cocola/web dev                     # http://localhost:3000
> ```
>
> 页面提供令牌 / session / prompt 输入框与原始事件流日志,可直接观测
> `text` / `thinking` / `sandbox` / `error` 等事件;未知 kind 回退原始 JSON。

## 路线图

| 里程碑  | 内容                                                                                                                                                                            | 状态 |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---- |
| M0      | Monorepo 地基、本地开发依赖                                                                                                                                                     | ✅   |
| M1      | SandboxProvider 抽象 + Docker 实现                                                                                                                                              | ✅   |
| M2      | 会话↔沙箱绑定 + 租约/两段式 GC（Agent Runtime 闭环）                                                                                                                            | ✅   |
| M3      | LLM Gateway：Anthropic 兼容代理（承接 Claude Agent SDK）+ 计费账本                                                                                                              | ✅   |
| M4      | Auth + Token 配额：cocola 签发令牌即 SDK API Key（HS256 离线校验）+ 周期化 token 配额（按用户日 / 租户月，超额 429）                                                            | ✅   |
| M5      | Admin API + Skill Market：Go 控制面（令牌签发 / 吊销denylist + 动态 per-subject 配额覆盖 + Skill 目录 CRUD + 审计日志），令牌编解码与 Python 网关跨语言互通（HS256 字节级一致） | ✅   |
| **MVP** | **后端端到端打通：agent-runtime gRPC 服务（`AgentRuntimeService.Query` 服务端流式）+ gateway BFF（HTTP/SSE + 令牌校验，复用 go-common/token 共享 HS256 编解码）**               | ✅   |
| **R-A** | **Route A：Claude Code 大脑进沙箱（ADR-0009）+ 真实模型接入 + 全栈容器化（docker-compose.full）+ Web 对话 / 原生工具端到端**                                                       | ✅   |
| M6      | K8s Provider:client-go 实现 8 方法 + 休眠(删 Pod 留 PVC)/恢复(凭 binding 重建)/Exec 自愈 + egress NetworkPolicy + 部署物(K8s 清单 / Helm Chart);默认 runc + 用户命名空间(零节点安装),gVisor 为可选增强;代码与单测就绪,真实集群端到端验收(Layer C)已在 k3d(本地)跑通,发行版无关(k3d/k3s/EKS/GKE/AKS) | ✅   |
| M7      | 持久化数据分层：会话 `session_map`／计费账本／控制面元数据落 Postgres，重启不丢、可自托管、多副本可共享（Vault 密钥托管按 ADR-0008 留待后续）                                      | ✅   |
| M8      | 可观测性与压测：五服务统一 RED 指标(Prometheus)+ OTel 链路(默认关，Tempo)+ 部署观测栈(Grafana 看板)+ 压测套件(k6 SSE / ghz gRPC)与容量基线 runbook                          | ✅   |

## License

Apache-2.0
