# Plan: 全栈服务容器化（M-full containerization）

> 状态：实施中。本文档承接 `m-minimal-vertical-slice.md`，把剩余 5 个服务全部容器化，
> 目标是一条 `docker compose up` 拉起整个 cocola 数据面 + 控制面。

## 1. 目标

- cocola 的全部 6 个服务节点都能在容器内运行（RocketMQ 式开箱即用）。
- 部署者宿主机只需 Docker + Docker Compose，**无需** 安装 Go / Python / Node / uv / pnpm。
- 所有语言工具链（go≥1.25、python3.11+uv、node22+pnpm）锁死在各自构建镜像里。

## 2. 服务清单与拓扑

| 服务 | 栈 | 监听 | 依赖 | 角色 |
|---|---|---|---|---|
| sandbox-manager | Go | :50051 gRPC | redis, docker.sock | 沙箱控制面（已容器化） |
| admin-api | Go | :8090 HTTP | redis | 运营控制面：发 token / 配额 / Skill 市场 / 审计 |
| gateway | Go | :8080 HTTP | agent-runtime | 对外 BFF：鉴权 + SSE |
| agent-runtime | Python/uv | :50061 gRPC | sandbox-manager, llm-gateway, admin-api | Agent 编排 |
| llm-gateway | Python/uv | :8080(容器内) HTTP | redis | 模型路由 + 配额计费 |
| web | Next.js | :3000 | gateway | 前端 |
| redis | redis:7 | :6379 | — | 共享态 |

调用链：`web → gateway → agent-runtime → {sandbox-manager, llm-gateway, admin-api}`；
admin-api/llm-gateway 经 redis 同步吊销/配额；sandbox-manager 经 docker.sock 创建用户沙箱。

## 3. 构建策略（按栈分三类）

### 3.1 Go 服务（admin-api, gateway）
多阶段：`golang:1.25-alpine` 编译静态二进制 → `alpine:3.20` 运行。
- 构建上下文必须是仓库根：go.mod 用相对 `replace` 指向 `packages/go-common`（gateway 还需 `packages/proto/gen/go`）。
- `GOWORK=off` 单模块构建；`CGO_ENABLED=0` 出静态二进制。
- admin-api 同时打入 `admin-mint`（离线发 token 工具）。

### 3.2 Python 服务（agent-runtime, llm-gateway）
单阶段 `python:3.11-slim` + `uv sync --no-dev --frozen`。
- **为什么单阶段**：两者的一方依赖（cocola-common / cocola-proto）在 pyproject 里是
  `[tool.uv.sources]` 的 **editable 路径源**，venv 指向磁盘源码，运行期必须保留该源码。
  多阶段搬运 venv 会因 editable 路径断裂而失败，单阶段最稳。
- 构建上下文必须是仓库根（path 源在 `../../packages/...`）。

### 3.3 Web（Next.js / pnpm workspace）
多阶段 `node:22-slim`：先 `pnpm install --frozen-lockfile`（只 COPY 各 package.json 以利缓存）
→ `pnpm --filter @cocola/web build` → 运行阶段 `next start`。
- 构建上下文必须是仓库根：web 依赖 `@cocola/ts-common`（workspace:*）。

## 4. 关键决策

1. **统一 golang:1.25-alpine**：admin-api/gateway 的 go.mod 虽写 `go 1.22`，但 go-common 经
   otel 间接要求 go≥1.25；用 1.25 构建向后兼容，避免版本漂移。
2. **端口冲突**：llm-gateway 与 gateway 容器内都默认 :8080。容器各自独立网络命名空间，
   compose 内用服务名互访不冲突；仅宿主机端口映射需错开（llm-gateway 映射到 :8081，gateway :8080）。
3. **离线构建**：`DOCKER_BUILDKIT=0 --pull=false`，复用本地缓存基础镜像（公司 TLS 代理阻断
   docker.io registry token，但 PyPI/npm/alpine apk 源已验证可达）。
4. **鉴权默认关闭（dev）**：compose 默认不设 `COCOLA_AUTH_SECRET`/`COCOLA_ADMIN_KEY`，
   开箱即跑；生产经 env 注入密钥即开启，无需改代码。
5. **provider 默认 Echo（dev）**：不设 `COCOLA_LLM_BASE_URL` 时 agent-runtime 用 EchoProvider，
   零配置可端到端联通；接真实模型时把 `COCOLA_LLM_BASE_URL` 指向 llm-gateway 即可。

## 5. 实施步骤

1. 写 5 个 Dockerfile（admin-api / gateway / agent-runtime / llm-gateway / web）。✅
2. 逐个离线构建验证镜像产出。
3. 写全栈 `docker-compose.full.yml`，串起 7 个服务（含 redis、DooD、路径同构卷）。
4. `compose up` 冒烟：各容器健康，调用链 web→gateway→agent-runtime→sandbox/llm 通。
5. 写 changelog，提交（待用户审阅后）。

## 6. 验收标准

- 5 个镜像全部构建成功。
- `docker compose -f docker-compose.full.yml up -d` 后 7 容器全部 healthy/running。
- gateway `/healthz` 200；llm-gateway `/healthz` 200；admin-api 可达；web 首页 200。
- agent-runtime 启动日志显示已连 sandbox-manager + 选定 provider。
- 沙箱链路仍满足 m-minimal 的持久化与重启存活（不回归）。

## 7. 范围外（本次不做）

- K8s manifests / Helm（后续 ADR）。
- NFS HA / 备份（后续 ADR）。
- 生产密钥管理（Vault / sealed-secrets）。
- 镜像推送到私有 registry（部署者本地构建即可）。
