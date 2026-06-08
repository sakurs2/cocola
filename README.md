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

> 当前里程碑：**M0 地基** — 仅包含 monorepo 结构、共享库雏形、本地依赖编排。
> 业务功能将在 M1 开始逐步落地，详见 `docs/adr/`。

## 路线图

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M0 | Monorepo 地基、本地开发依赖 | ✅ |
| M1 | SandboxProvider 抽象 + Docker 实现 | ⏳ |
| M2 | Agent Runtime 最小闭环 | ⏳ |
| M3 | LLM Gateway + 计费 | ⏳ |
| M4 | Gateway + Auth + 用户体系 | ⏳ |
| M5 | Admin API + Skill Market | ⏳ |
| M6 | K8s + gVisor Provider | ⏳ |
| M7 | 持久化数据分层、Vault 接入 | ⏳ |
| M8 | 可观测性与压测 | ⏳ |

## License

Apache-2.0
