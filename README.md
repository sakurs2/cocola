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
│   ├── docker-compose/       # dev 依赖与正式 Docker 栈
│   ├── opensandbox-k8s/      # 本地 OpenSandbox Kubernetes runtime
│   └── sandbox-runtime/      # 沙箱运行时镜像
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

### 一键拉起本地调试栈

`make dev` 是默认 dev 调试入口：OpenSandbox Kubernetes runtime、PostgreSQL、
Redis、MinIO 等依赖由 Docker/k3d/Helm 准备，cocola 自己的服务
（sandbox-manager、llm-gateway、admin-api、agent-runtime、gateway、web）在本机
原生前台运行，方便改代码后 Ctrl-C 重启。各服务日志落在 `.run-logs/<服务名>.log`。

```bash
make dev   # dev 调试栈：OpenSandbox runtime + 本机原生 cocola 服务
make prod  # 正式/完整 Docker 启动：scripts/start.sh + docker-compose.full.yml
```

> 端口约定：gateway BFF 与 llm-gateway 默认都监听 `8080`，编排脚本把
> llm-gateway 改钉到 `8081`（`COCOLA_LLM_PORT`）规避端口冲突；沙箱内大脑经
> `COCOLA_SANDBOX_LLM_BASE_URL` 回连它。`make dev` 会自动把 agent-runtime 指向本机
> sandbox-manager，并把 sandbox-manager 指向准备好的 OpenSandbox server。

#### 接入模型

启动后在 `Admin -> Models` 中配置 Provider、endpoint、API key、模型路由和默认
模型。模型目录以 Postgres 为唯一事实源，LLM Gateway 自动加载变更；不再支持一份
独立的模型 JSON 或 provider 环境变量，避免 Web、Agent Runtime 和 LLM Gateway
读取到不同配置。API key 加密保存，不写入仓库文件。

> 鉴权闭环:`run-stack.sh` 会把 `admin-mint` 签出的令牌注入 sandbox——网关用同一个
> `COCOLA_AUTH_SECRET` 离线校验它并按令牌主体计费,无需手动配 key。agent 发给
> 网关的模型别名由 Admin 模型目录选择并解析。

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
> export COCOLA_BOOTSTRAP_ADMIN_USERNAME=admin
> export COCOLA_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
> export COCOLA_BOOTSTRAP_ADMIN_PASSWORD=<初始管理员密码>
> go run ./apps/admin-api/cmd/admin-api               # 默认监听 :8090
> # 跨语言互通校验（Go 签发 -> Python 校验）：
> apps/llm-gateway/.venv/bin/python scripts/admin-m5-e2e.py
> ```
>
> Web 产品入口使用 Auth.js Credentials 登录。`admin-api` 是白名单、角色、密码哈希
> 与审计事实源；浏览器只持有 Auth.js session/cookie，不保存 cocola runtime token。
> `make dev` dev 模式默认 bootstrap 管理员为 `admin` 或 `admin@cocola.local`，
> 密码 `cocola-admin`；会重置成这个固定密码，并在控制台 ready banner 中打印。可通过同名环境变量覆盖。
> 生产部署必须同时配置
> `AUTH_SECRET`、`COCOLA_ADMIN_KEY`、`COCOLA_AUTH_SECRET` 以及上述 bootstrap admin
> 环境变量；初始账号已存在时默认不覆盖密码，只有 `COCOLA_BOOTSTRAP_ADMIN_RESET=true`
> 才会重置。
>
> **后端 MVP（端到端链路）**：前端 → gateway（BFF，HTTP/SSE + 令牌校验）→
> agent-runtime（gRPC）→ llm-gateway / sandbox-manager → 事件流式回传。
> agent-runtime 暴露 `AgentRuntimeService.Query` 服务端流式 RPC；gateway 作为
> BFF 校验 cocola 令牌（复用 `packages/go-common/token` 同一套 HS256 编解码，
> 与 admin-api 共享，不重复造轮子），并把 Agent 事件以 SSE 推给浏览器。
> Gateway 在本进程后台执行 Agent Run，浏览器断线只中断订阅；重连会先收到完整
> assistant snapshot。Stop 才会显式取消任务。Gateway/Agent Runtime 重启不会重放
> 工具调用，而是把 Run 标记为 `interrupted` 并保留最近一次 partial answer。详见
> [`docs/core-chat-reliability.md`](./docs/core-chat-reliability.md) 与 ADR-0019。
>
> ```bash
> # 启动 agent-runtime gRPC 服务（缺省 :50061；real 模式要求配置 sandbox）
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
> **接入真实 LLM 链路（Route A，ADR-0009）**：给 agent-runtime 设置
> `COCOLA_SANDBOX_ADDR` 指向 sandbox-manager，它即从 EchoProvider 切换到
> `InSandboxShimProvider`——整个 Claude Code 大脑跑在用户自己的沙箱里，
> agent-runtime 只做控制面路由。沙箱创建时经 ENV 注入模型凭证：
> `ANTHROPIC_BASE_URL`←`COCOLA_SANDBOX_LLM_BASE_URL`（cocola llm-gateway 根）、
> `ANTHROPIC_AUTH_TOKEN`←`COCOLA_SANDBOX_LLM_TOKEN`（cocola 令牌）。**校验令牌与
> 沙箱内 CLI 令牌是同一个**——网关收到 `x-api-key` 后离线校验、按令牌主体计费，
> M4 的设计在此收口。凭证只走沙箱 ENV，绝不走 prompt 通道。
>
> ```bash
> # agent-runtime 绑定 sandbox-manager；沙箱内 claude CLI 经注入的 ENV 回连 llm-gateway
> export COCOLA_SANDBOX_ADDR=127.0.0.1:50051
> export COCOLA_SANDBOX_IMAGE=cocola/sandbox-runtime:dev
> export COCOLA_SANDBOX_LLM_BASE_URL=http://127.0.0.1:8080
> export COCOLA_SANDBOX_LLM_TOKEN=<cocola 签发令牌>
> export COCOLA_SANDBOX_MODEL_ALIAS=cocola-default   # 网关 registry 解析为真实模型
> cd apps/agent-runtime && uv run python -m cocola_agent_runtime
> # llm-gateway 侧的真实上游在 Admin -> Models 中配置
> ```
>
> > 注：早期的中心化 SDK 路径（Route B，`ClaudeAgentSDKProvider` 在
> > agent-runtime 进程内 spawn claude CLI，由 `COCOLA_LLM_BASE_URL` 驱动）已于
> > 2026-07-02 下线，详见 ADR-0009。
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

> **Web 产品入口**:`apps/web` 提供 Auth.js 登录、聊天、会话列表与后台页。页面经
> 同源 Next.js route handler 反代到 gateway / admin-api；Web 服务端按当前 Auth.js
> session 从 admin-api 换取短 TTL runtime token，再把 SSE 原样透传给浏览器。
>
> ```bash
> # 先起 agent-runtime 与 gateway(见上),再起前端
> export COCOLA_GATEWAY_URL=http://127.0.0.1:8080   # route.ts 反代目标(缺省即此)
> export COCOLA_ADMIN_URL=http://127.0.0.1:8090
> export AUTH_SECRET=<Auth.js session 密钥>
> export COCOLA_ADMIN_KEY=<与 admin-api 一致>
> pnpm install
> pnpm --filter @cocola/web dev                     # http://localhost:3000
> ```

## 路线图

| 里程碑  | 内容                                                                                                                                                                                                                                                                                                | 状态 |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---- |
| M0      | Monorepo 地基、本地开发依赖                                                                                                                                                                                                                                                                         | ✅   |
| M1      | SandboxProvider 抽象 + Docker 实现                                                                                                                                                                                                                                                                  | ✅   |
| M2      | 会话↔沙箱绑定 + 租约/两段式 GC（Agent Runtime 闭环）                                                                                                                                                                                                                                                | ✅   |
| M3      | LLM Gateway：Anthropic 兼容代理（承接 Claude Agent SDK）+ 计费账本                                                                                                                                                                                                                                  | ✅   |
| M4      | Auth + Token 配额：cocola 签发令牌即 SDK API Key（HS256 离线校验）+ 周期化 token 配额（按用户日 / 租户月，超额 429）                                                                                                                                                                                | ✅   |
| M5      | Admin API + Skill Market：Go 控制面（令牌签发 / 吊销denylist + 动态 per-subject 配额覆盖 + Skill 目录 CRUD + 审计日志），令牌编解码与 Python 网关跨语言互通（HS256 字节级一致）                                                                                                                     | ✅   |
| **MVP** | **后端端到端打通：agent-runtime gRPC 服务（`AgentRuntimeService.Query` 服务端流式）+ gateway BFF（HTTP/SSE + 令牌校验，复用 go-common/token 共享 HS256 编解码）**                                                                                                                                   | ✅   |
| **R-A** | **Route A：Claude Code 大脑进沙箱（ADR-0009）+ 真实模型接入 + 全栈容器化（docker-compose.full）+ Web 对话 / 原生工具端到端**                                                                                                                                                                        | ✅   |
| M6      | K8s Provider:client-go 实现 8 方法 + 休眠(删 Pod 留 PVC)/恢复(凭 binding 重建)/Exec 自愈 + egress NetworkPolicy + 部署物(K8s 清单 / Helm Chart);默认 runc + 用户命名空间(零节点安装),gVisor 为可选增强;代码与单测就绪,真实集群端到端验收(Layer C)已在 k3d(本地)跑通,发行版无关(k3d/k3s/EKS/GKE/AKS) | ✅   |
| M7      | 持久化数据分层：会话 `session_map`／计费账本／控制面元数据落 Postgres，重启不丢、可自托管、多副本可共享（Vault 密钥托管按 ADR-0008 留待后续）                                                                                                                                                       | ✅   |
| M8      | 可观测性与压测：五服务统一 RED 指标(Prometheus)+ OTel 链路(默认关，Tempo)+ 部署观测栈(Grafana 看板)+ 压测套件(k6 SSE / ghz gRPC)与容量基线 runbook                                                                                                                                                  | ✅   |
| WP      | Warm Pool 默认开启；预热沙箱 claim 后按 `reused=false` 从 MinIO checkpoint 恢复 session 数据，不依赖运行中热挂载持久卷。活跃沙箱与冷启动沙箱使用相同 owner 校验和 heartbeat，见 ADR-0019。                                                                                                          | ✅   |
| GV      | gVisor(runsc)兼容性 spike:Node + Claude Code 在 `RuntimeClass=runsc` 下跑通 Route A 的 pre-prod 验收门。Layer A/B 本机可做,Layer C(真集群 + gVisor 端到端)待目标集群                                                                                                                                | ⏳   |

## 安全：沙箱出网模型（egress）

沙箱里跑的是不可信的用户/Agent 代码,因此安全边界不是「工具白名单」,而是
**网络出网管控**(ADR-0009)。为让远程 MCP、网页访问和包管理开箱即用，当前默认
不下发 egress policy，允许访问公网：

- **默认开放**:未配置 `COCOLA_SANDBOX_EGRESS_ALLOWLIST` 时不创建网络策略。
- **生产收紧**:配置 `COCOLA_SANDBOX_EGRESS_ALLOWLIST` 后切换为 default-deny，
  仅允许逗号分隔的域名/CIDR/IP；编排层自动并入
  `COCOLA_SANDBOX_LLM_BASE_URL` 的 gateway host。
- **按需控制**:例如 `mcp.amap.com,api.github.com`，无需使用 `*` 全量放行。

当前内置 sandbox provider 只保留 OpenSandbox。cocola 把 egress allowlist 转成
OpenSandbox 的 `networkPolicy`，由 OpenSandbox 所在 runtime 负责执行；本地 dev 默认
使用 OpenSandbox Kubernetes runtime，正式 Docker 模式由 `scripts/start.sh` 管理
OpenSandbox server。

**域名级精确放行**:OpenSandbox 的 DNS-aware egress sidecar 负责解析域名并维护
nftables 动态 allow set，不需要在 Cocola 中固定供应商 IP。

> 配置语义：`Networking.EgressAllowlist` 为 nil = 未配置策略、允许公网；非 nil
> 且非空 = 防火墙生效、仅放行列表和模型网关、其余 DROP。

## License

Apache-2.0
