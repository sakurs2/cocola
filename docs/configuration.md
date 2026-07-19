# Cocola 配置规范

Cocola 只保留两类配置源。一个配置项只能有一个 owner，不允许文件、环境变量、
数据库和 Redis 同时表达同一含义。

仓库根目录的 `.env.example` 是可直接复制的本地 dev/prod 配置：
`cp .env.example .env`。其中凭据仅供本机开发；真实部署必须替换。已有环境升级时
应合并变量而不是覆盖，尤其必须保持 `COCOLA_MODEL_SECRET_KEY` 和
`COCOLA_CONFIG_SECRET_KEY` 稳定。

## 1. 配置所有权

| 类型           | 唯一来源                    | 生效方式       | 适用内容                                                                           |
| -------------- | --------------------------- | -------------- | ---------------------------------------------------------------------------------- |
| 运行时业务配置 | Admin/Postgres              | 保存后热加载   | 模型、Prompt、MCP、Skill、Execution、Scheduler、Session 默认请求容量、Trace 保留期 |
| 进程启动配置   | 环境变量 / Secret 文件      | 重启对应进程   | 地址、凭据、存储、沙箱、超时、资源、可观测性                                       |
| 正式部署参数   | `cocola` CLI                | 重新创建正式栈 | 镜像版本、Registry、端口、OpenSandbox 部署方式、初始管理员                         |
| 本地开发参数   | `make dev` / `scripts/*.sh` | 重新启动本地栈 | 端口、k3d/OpenSandbox 准备、开发凭据                                               |

Redis 只用于缓存、租约、事件和权限传播，不是配置真源。Sandbox Manager 在创建
Session Volume 时直接从 Postgres 读取有效默认容量；不通过 Redis 投递存储配置。
`NEXT_PUBLIC_*` 是 Web 构建期配置，修改后必须重新构建 Web。

## 2. 可热加载配置

以下配置由 Admin/Postgres 持有：

- Models：Provider 类型、endpoint、加密 API key、模型 alias、真实模型、默认模型；
- Agent policy：系统 Prompt、MCP、Skill；
- Execution：`execution.agent_max_turns`、`execution.tool_step_timeout_secs`；
- Scheduler：`scheduler.enabled`、poll/run timeout/heartbeat/lease；
- Storage：`storage.session_volume_default_size`；
- Observability：`observability.trace_retention_days`。

Execution、Scheduler、Session 默认容量和 Trace 的同名环境变量只是无 DB override
时的默认值。
管理页只展示这些确实可热加载的设置；Reset 会回到环境默认或代码默认。

## 3. 启动配置

启动配置按 owner 分组。除明确注明外，修改后需要重启对应进程。

| 分组             | 主要变量                                                                                                                             |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| 服务地址         | `COCOLA_*_ADDR`、`COCOLA_*_HOST`、`COCOLA_*_PORT`、`COCOLA_ADMIN_URL`、`COCOLA_GATEWAY_URL`、`COCOLA_LLM_GATEWAY_URL`                |
| Auth/Secret      | `AUTH_SECRET`、`COCOLA_AUTH_SECRET`、`COCOLA_AUTH_ISSUER`、`COCOLA_ADMIN_KEY`、`COCOLA_MODEL_SECRET_KEY`、`COCOLA_CONFIG_SECRET_KEY` |
| 初始管理员       | `COCOLA_BOOTSTRAP_ADMIN_USERNAME`、email、password/password hash、reset                                                              |
| Postgres/Redis   | `COCOLA_PG_DSN`、`COCOLA_REDIS_ADDR`、`COCOLA_REDIS_PASSWORD`、`COCOLA_REDIS_DB`、`COCOLA_REDIS_POOL_SIZE`                           |
| MinIO            | `COCOLA_MINIO_ENDPOINT`、access/secret key、bucket、TLS、附件阈值                                                                    |
| Agent/Run        | `COCOLA_AGENT_MAX_TURNS`、`COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS`、`COCOLA_SANDBOX_TOKEN_TTL_SECONDS`、gRPC/message/artifact limits    |
| Sandbox          | `COCOLA_SANDBOX_ADDR`、image、Profile、Code Server、lease/reaper/heartbeat、LLM URL/model、egress                                    |
| Session Storage  | `COCOLA_CLUSTER_MANAGER_MODE`、`COCOLA_SESSION_STORAGE_CLASS`、`COCOLA_SESSION_VOLUME_SIZE`、`COCOLA_SESSION_STORAGE_ROOT`           |
| OpenSandbox      | `COCOLA_OPENSANDBOX_*`（URL、API key、HTTP/Exec timeout、resources、K8s 部署参数）                                                   |
| LLM 可靠性       | `COCOLA_LLM_TIMEOUT_SECS`、`COCOLA_LLM_MAX_RETRIES`、`COCOLA_LLM_RATE_LIMIT_RPS`、registry cache TTL                                 |
| Quota/Auth cache | quota 默认限额、override/revocation cache TTL、token TTL                                                                             |
| Observability    | `COCOLA_METRICS_*`、`COCOLA_OTEL_*`                                                                                                  |

Secret 支持统一的 `<NAME>_FILE` 约定：未设置时读取同名环境变量；显式设置后文件
不可读会导致启动失败，不会回退到可能过期的环境值。生产环境应通过 Secret/Vault
文件注入，不把明文写入仓库。

Gateway、Agent Runtime、Sandbox Manager、Admin API 和 LLM Gateway 要求
`COCOLA_PG_DSN`；Sandbox Manager 与 Admin API 还要求 `COCOLA_REDIS_ADDR`，LLM
Gateway 要求 `COCOLA_LLM_REDIS_URL`。MinIO 只服务附件、Artifact 和 Skill bundle，
Sandbox Manager 不持有 MinIO 凭据，也不读写 Session checkpoint。源码开发由
`make dev` 准备依赖；正式部署由部署配置注入独立变量。
内存实现只用于测试，不是运行模式。

`storage.session_volume_default_size` 默认 `2Gi`，格式使用 Kubernetes
`resource.Quantity`。DB 设置保存后立即影响下一个新 Session；删除 DB override 后回到
`COCOLA_SESSION_VOLUME_SIZE` 或代码默认值。它是 local-path PVC 的请求容量/软上限，
不保证阻止超额写入，也不会修改已创建 PVC。v1 不提供扩容接口。

`COCOLA_SESSION_STORAGE_ROOT` 默认 `/var/lib/cocola/storage`，供 Admin 将
local-path PV 的宿主机路径安全转换为探针挂载内的相对路径；修改后需要重启
Admin API，并且必须与 local-path provisioner 和探针 DaemonSet 的宿主机路径一致。
节点 `statfs` 只在 Storage 页面请求时读取，Session 实际占用只在管理员显式点击
Measure 时计算，没有周期存储扫描。

Agent Run 默认不设置总 wall-clock deadline。`execution.agent_max_turns` 默认 `200`，
`execution.tool_step_timeout_secs` 默认 `600` 秒；两者的 DB 覆盖都在下一个新 Run
开始时读取，运行中的 Run 不受配置变化影响。客户端只能请求更小的 max turns，不能绕过
管理员上限。工具步骤超时会以 `STEP_TIMEOUT` 结束当前 Run，但 Session PVC、Workspace
和 Runtime 会话状态继续保留，用户可在下一轮继续。

`COCOLA_LLM_TIMEOUT_SECS` 默认 `600` 秒，限制单次模型请求，修改后需要重启
LLM Gateway。`COCOLA_SANDBOX_TOKEN_TTL_SECONDS` 默认 `604800` 秒（7 天），修改后
需要重启 Gateway；它是沙箱访问 LLM Gateway 的凭据有效期，不是 Agent Run 超时。

`COCOLA_SANDBOX_PROFILE` 由 Sandbox Manager 启动配置持有，只接受 `coding`（默认）
和 `minimal`。`coding` 默认启用 resident Code Server 与按需 headless Browser，并在
未显式申请资源时使用 `1000m/2048Mi`；`minimal` 默认关闭两项能力，使用
`500m/512Mi`。两个 Profile 都保留基础 Artifact output contract。运维可以用
`COCOLA_CODE_SERVER_ENABLED`、`COCOLA_BROWSER_ENABLED` 和
`COCOLA_OPENSANDBOX_DEFAULT_CPU/MEMORY` 覆盖对应默认值。Sandbox Manager 会删除 Agent
请求里的同名值再注入运维配置；修改后只影响新建 Sandbox，需要重启 Sandbox Manager。
Profile 不是对话级设置，不进入 Admin/Postgres。

`cocola-sandbox-browser` 与 `cocola-sandbox-artifacts` 是随 Sandbox Runtime 镜像发布的
内置 Agent Skill，没有独立环境变量或 Admin 开关。Agent Runtime 会读取当前镜像的
platform Skill manifest，和 Admin/Personal Skill 合并后同时暴露到 Claude、Codex 的
标准目录。Browser Skill 只指导 Agent 调用受 `COCOLA_BROWSER_ENABLED` 控制的 guest
CLI，不会绕过 Profile 或网络策略；Artifact Skill 说明 `/workspace/outputs` 发布契约和
隔离 HTML 预览边界。

### 热加载边界复核

| 配置域                               | 结论                             | 原因 / 调整入口                                         |
| ------------------------------------ | -------------------------------- | ------------------------------------------------------- |
| Session Volume 默认请求容量          | 热加载，仅影响新 Session         | 创建 PVC 前从 Postgres 读取；无存储轮询或定时 reconcile |
| Scheduler interval / timeout / lease | 热加载                           | worker 每轮从 system settings 重读                      |
| Trace retention                      | 热加载                           | maintenance 每轮读取 system setting                     |
| Agent max turns                      | 热加载，仅影响新 Run             | Gateway 在创建 Run 时读取 Postgres                      |
| Tool step timeout                    | 热加载，仅影响新 Run             | 事件驱动计时器；无轮询或周期扫描                        |
| Node 最大 Sandbox 数                 | 热加载，但不属于 System Settings | Admin -> Sandbox Nodes 直接更新 Kubernetes annotation   |
| 用户/租户 quota override             | 热加载，但不属于 System Settings | Admin -> Quotas，按 subject 独立管理                    |
| Auth secret / token TTL / Admin key  | 重启                             | issuer/verifier 在进程启动时构造，必须跨进程一致        |
| 服务地址 / Postgres / Redis / MinIO  | 重启                             | listener、连接池和客户端在启动时构造                    |
| Agent Run timeout / Sandbox lease    | 重启                             | 跨 Gateway、Agent、token TTL 的启动期校验必须一致       |
| LLM timeout / retry / rate limit     | 重启                             | resilience policy 与 limiter 在 LLM Gateway 启动时构造  |

## 4. 从 checkpoint 模型破坏性升级

测试阶段不迁移旧 Workspace。升级前先停止新 Run 并销毁现有 Sandbox，然后删除旧
Session PVC 或 legacy Docker 的旧 Session 目录。`00039_local_session_storage.sql`
会清空运行绑定、删除 checkpoint 字段并创建新的 `session_storage`；现有 Conversation
下次运行时获得新的 Workspace。

确认旧 checkpoint 不再需要后，可用已有 MinIO Client 做一次性前缀清理：

```bash
mc alias set cocola "$COCOLA_MINIO_ENDPOINT" "$COCOLA_MINIO_ACCESS_KEY" "$COCOLA_MINIO_SECRET_KEY"
mc rm --recursive --force "cocola/$COCOLA_MINIO_BUCKET/checkpoints/"
```

该命令只在维护窗口人工执行。运行时没有 checkpoint 清理任务、迁移 Controller、
自动回滚或存储 reconcile。

## 5. 已废弃配置

以下配置已删除，不再兼容别名：

| 已废弃                                            | 替代方案                                |
| ------------------------------------------------- | --------------------------------------- |
| `COCOLA_LLM_CONFIG`                               | Admin -> Models / Postgres              |
| `COCOLA_LLM_PROVIDER`、`COCOLA_LLM_DEFAULT_ALIAS` | Admin -> Models / Postgres              |
| `COCOLA_ANTHROPIC_*`、`COCOLA_OPENAI_*` 路由配置  | Admin -> Models / Postgres              |
| `COCOLA_ADMIN_BASE_URL`                           | `COCOLA_ADMIN_URL`                      |
| `COCOLA_AUTH_DEV_ANON`、`COCOLA_AUTH_ALLOW_ANON`  | 已删除；所有实际请求使用签名 token      |
| `COCOLA_AGENT_API_KEY`                            | Gateway 每个 Run 下发的用户 token       |
| `COCOLA_AGENT_RUN_TIMEOUT_SECS`                   | 已删除；Run 无总超时，改用步骤级限制    |
| `COCOLA_SANDBOX_PROVIDER`                         | OpenSandbox 是唯一生产后端              |
| `COCOLA_SANDBOX_WARM_POOL_*`                      | 已删除；Sandbox 统一按 Session 按需创建 |
| `COCOLA_SESSION_CHECKPOINT_*`                     | 已删除；Session PVC 是文件状态源        |
| `COCOLA_SANDBOX_CHECKPOINT_DRAIN_SECS`            | 已删除；回收只销毁计算环境              |
| `COCOLA_ENV`                                      | 无；开发用 `make dev`，正式部署用 CLI   |
| `COCOLA_LOG_LEVEL`、`COCOLA_SERVICE_NAME`         | 已删除未接线的假配置；服务名由进程固定  |

Fake LLM provider 只允许测试代码直接构造，不允许通过 Admin 创建，也不会从生产
Postgres 加载。

## 6. 变更规则

新增配置前必须回答：owner 是哪个进程、来源是什么、是否热加载、默认值和非法值
如何处理、是否是 secret。能由现有状态推导的配置不新增；临时灰度开关在功能全量后
必须连同分支和文档一起删除。
