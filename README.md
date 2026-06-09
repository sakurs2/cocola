# cocola

> Open-source enterprise AI Agent platform — self-host your own agents, bring your own tokens, let your team chat for free.

`cocola` 是一个面向企业内部部署的 Agent 平台。核心定位：

- **企业自部署**：完全私有化部署，数据不出企业
- **统一计费**：企业统一采购 LLM Token，员工使用零成本
- **可定制业务逻辑**：登录鉴权、敏感词、Skill Market 等可插拔
- **沙箱执行**：基于云沙箱安全执行 Agent 产生的代码与命令

## 技术栈

- **Agent 核心**：Claude Code Agent SDK
- **沙箱**：K8s + gVisor（可通过 `SandboxProvider` 抽象切换至 Docker / E2B / CubeSandbox）
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

> 当前里程碑：**M5 Admin API + Skill Market** — 已完成 M0–M5。Go 控制面（admin-api）现在签发 / 吊销 cocola
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

## 路线图

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M0 | Monorepo 地基、本地开发依赖 | ✅ |
| M1 | SandboxProvider 抽象 + Docker 实现 | ✅ |
| M2 | 会话↔沙箱绑定 + 租约/两段式 GC（Agent Runtime 闭环） | ✅ |
| M3 | LLM Gateway：Anthropic 兼容代理（承接 Claude Agent SDK）+ 计费账本 | ✅ |
| M4 | Auth + Token 配额：cocola 签发令牌即 SDK API Key（HS256 离线校验）+ 周期化 token 配额（按用户日 / 租户月，超额 429） | ✅ |
| M5 | Admin API + Skill Market：Go 控制面（令牌签发 / 吊销denylist + 动态 per-subject 配额覆盖 + Skill 目录 CRUD + 审计日志），令牌编解码与 Python 网关跨语言互通（HS256 字节级一致） | ✅ |
| M6 | K8s + gVisor Provider | ⏳ |
| M7 | 持久化数据分层、Vault 接入 | ⏳ |
| M8 | 可观测性与压测 | ⏳ |

## License

Apache-2.0
