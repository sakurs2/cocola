# feat(deploy): 全栈服务容器化

## 背景

承接 M-minimal 纵向切片（sandbox-manager 已容器化）。此前 6 个服务里只有
sandbox-manager 有 Dockerfile，其余 5 个（admin-api / gateway / agent-runtime /
llm-gateway / web）必须在宿主机装齐 Go / Python+uv / Node+pnpm 才能跑，部署摩擦大。

本次把剩余 5 个服务全部容器化，目标：部署者只需 Docker + Docker Compose，
一条命令拉起整个控制面 + 数据面，所有语言工具链锁死在各自构建镜像里。

## 改动

### 新增 5 个 Dockerfile
- `apps/admin-api/Dockerfile` — Go 多阶段（golang:1.25-alpine → alpine:3.20），
  同时打入 admin-api 与 admin-mint（离线发 token 工具）。
- `apps/gateway/Dockerfile` — Go 多阶段，静态二进制。
- `apps/agent-runtime/Dockerfile` — Python 单阶段（python:3.11-slim + uv sync）。
- `apps/llm-gateway/Dockerfile` — 同上。
- `apps/web/Dockerfile` — Next.js 多阶段（node:22-slim + pnpm，build → next start）。

### 新增编排
- `deploy/docker-compose/docker-compose.full.yml` — 串起 7 个服务
  （redis + 6 个 cocola 服务），含 DooD socket 挂载、路径同构卷、按依赖顺序的
  healthcheck 门控、dev 默认零配置（auth off / EchoProvider）。

### 文档
- `docs/plan/m-full-containerization.md` — 设计与决策。

## 关键设计

1. **构建上下文统一为仓库根**：Go 的相对 replace、Python 的 editable path 源、
   web 的 pnpm workspace，都要求 packages/ 与 apps/ 在同一上下文。
2. **Python 单阶段**：cocola-common / cocola-proto 是 uv 的 editable 路径源，
   venv 指向磁盘源码，运行期必须保留，多阶段搬运 venv 会断链。
3. **统一 golang:1.25-alpine**：admin-api/gateway 的 go.mod 写 go 1.22，但
   go-common 经 otel 间接要求 go≥1.25，用 1.25 构建向后兼容。
4. **端口**：llm-gateway 与 gateway 容器内都默认 8080，宿主机映射错开
   （gateway 8080、llm-gateway 8081）；容器内经服务名互访不冲突。
5. **离线构建**：DOCKER_BUILDKIT=0 --pull=false，复用本地缓存基础镜像
   （公司 TLS 代理阻断 docker.io token，但 PyPI / npm / alpine apk 源已验证可达）。
6. **web 启动修正**：弃用 `pnpm start -- -H ...`（pnpm 的 -- 转发会把参数当目录），
   改 `pnpm exec next start` + HOSTNAME/PORT env。

## 验证

- 5 个镜像全部离线构建成功（rc=0）。
- `docker compose -f docker-compose.full.yml up -d` 后 7 容器全部 running，
  redis/admin-api/gateway/llm-gateway 报告 healthy。
- 健康检查：gateway /healthz 200、llm-gateway /healthz 200（JSON ok）、
  admin-api /healthz 200、web / 200。
- agent-runtime 启动日志确认已连 sandbox-manager:50051 + admin-api + EchoProvider。
- 端到端：`POST /v1/chat {prompt, session_id}` 经
  gateway→agent-runtime→sandbox-manager 创建真实 docker 沙箱，SSE 流式返回
  sandbox→text→done 三段事件，回包 `echo(smoke-s1): hello cocola`。

## 范围外

- K8s manifests / Helm；NFS HA / 备份；生产密钥管理；镜像推送私有 registry。
