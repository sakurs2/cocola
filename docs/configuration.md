# Cocola 配置规范

Cocola 只保留两类配置源。一个配置项只能有一个 owner，不允许文件、环境变量、
数据库和 Redis 同时表达同一含义。

仓库根目录的 `.env.example` 是可直接复制的本地 dev/prod 配置：
`cp .env.example .env`。其中凭据仅供本机开发；真实部署必须替换。已有环境升级时
应合并变量而不是覆盖，尤其必须保持 `COCOLA_MODEL_SECRET_KEY` 稳定。

## 1. 配置所有权

| 类型           | 唯一来源                    | 生效方式       | 适用内容                                                            |
| -------------- | --------------------------- | -------------- | ------------------------------------------------------------------- |
| 运行时业务配置 | Admin/Postgres              | 保存后热加载   | 模型、Prompt、MCP、Skill、Scheduler、Warm Pool sizing、Trace 保留期 |
| 进程启动配置   | 环境变量 / Secret 文件      | 重启对应进程   | 地址、凭据、存储、沙箱、超时、资源、可观测性                        |
| 本地编排参数   | `make dev` / `scripts/*.sh` | 重新启动本地栈 | 端口、k3d/OpenSandbox 准备、开发凭据                                |

Redis 只用于缓存、租约、事件和权限传播，不是配置真源。Warm Pool sizing 以
Postgres 为准，Redis 只是 sandbox-manager 的运行时投递缓存，Admin 会周期对账。
`NEXT_PUBLIC_*` 是 Web 构建期配置，修改后必须重新构建 Web。

## 2. 可热加载配置

以下配置由 Admin/Postgres 持有：

- Models：Provider 类型、endpoint、加密 API key、模型 alias、真实模型、默认模型；
- Agent policy：系统 Prompt、MCP、Skill；
- Scheduler：`scheduler.enabled`、poll/run timeout/heartbeat/lease/min interval；
- Sandbox：`sandbox.warm_pool_enabled`、`sandbox.warm_pool_size`；
- Observability：`observability.trace_retention_days`。

Scheduler、Warm Pool sizing 和 Trace 的同名环境变量只是无 DB override 时的默认值。
管理页只展示这些确实可热加载的设置；Reset 会回到环境默认或代码默认。

## 3. 启动配置

启动配置按 owner 分组。除明确注明外，修改后需要重启对应进程。

| 分组                   | 主要变量                                                                                                                                                       |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 服务地址               | `COCOLA_*_ADDR`、`COCOLA_*_HOST`、`COCOLA_*_PORT`、`COCOLA_ADMIN_URL`、`COCOLA_GATEWAY_URL`、`COCOLA_LLM_GATEWAY_URL`                                          |
| Auth/Secret            | `AUTH_SECRET`、`COCOLA_AUTH_SECRET`、`COCOLA_AUTH_ISSUER`、`COCOLA_AUTH_ALLOW_ANON`、`COCOLA_ADMIN_KEY`、`COCOLA_MODEL_SECRET_KEY`、`COCOLA_CONFIG_SECRET_KEY` |
| 初始管理员             | `COCOLA_BOOTSTRAP_ADMIN_USERNAME`、email、password/password hash、reset                                                                                        |
| Postgres/Redis         | `COCOLA_PG_DSN`、`COCOLA_REDIS_ADDR`、`COCOLA_REDIS_PASSWORD`、`COCOLA_REDIS_DB`、`COCOLA_REDIS_POOL_SIZE`                                                     |
| MinIO                  | `COCOLA_MINIO_ENDPOINT`、access/secret key、bucket、TLS、附件阈值                                                                                              |
| Agent/Run              | `COCOLA_AGENT_MODE`、`COCOLA_AGENT_RUN_TIMEOUT_SECS`、gRPC/message/artifact limits                                                                             |
| Sandbox                | `COCOLA_SANDBOX_ADDR`、image、lease/reaper/heartbeat、LLM URL/token/model、egress、volume backend                                                              |
| Warm Pool provisioning | `COCOLA_SANDBOX_WARM_POOL_REFILL_SECS`（enabled/size 是 Admin 可热加载配置，环境变量仅作为默认值）                                                             |
| OpenSandbox            | `COCOLA_OPENSANDBOX_*`（URL、API key、HTTP/Exec timeout、resources、K8s 部署参数）                                                                             |
| Checkpoint             | `COCOLA_SESSION_CHECKPOINT_*`、`COCOLA_SANDBOX_CHECKPOINT_DRAIN_SECS`                                                                                          |
| LLM 可靠性             | `COCOLA_LLM_TIMEOUT_SECS`、`COCOLA_LLM_MAX_RETRIES`、`COCOLA_LLM_RATE_LIMIT_RPS`、registry cache TTL                                                           |
| Quota/Auth cache       | quota 默认限额、override/revocation cache TTL、token TTL                                                                                                       |
| Observability          | `COCOLA_METRICS_*`、`COCOLA_OTEL_*`                                                                                                                            |

Secret 支持统一的 `<NAME>_FILE` 约定：文件内容优先于同名环境变量。生产环境应
通过 Secret/Vault 文件注入，不把明文写入仓库。

Warm Pool 默认开启、空闲目标默认 10。Enabled/Size 可在系统设置页热更新；Admin 每
5 秒把 Postgres 期望值对账到 Redis，sandbox-manager 每次 refill 读取。Redis 短暂失败
或投递键暂时缺失时，本轮不扩缩容，恢复后自动收敛，避免错误回退到启动默认值。
Image、LLM 凭据和 refill interval 仍是启动配置。

### 热加载边界复核

| 配置域                                   | 结论                             | 原因 / 调整入口                                        |
| ---------------------------------------- | -------------------------------- | ------------------------------------------------------ |
| Warm Pool enabled / idle target          | 热加载                           | reconcile loop 可安全补充或清理未 claim 的 sandbox     |
| Warm Pool image / 凭据 / refill interval | 重启                             | 创建参数与 ticker 在 sandbox-manager 启动时构造        |
| Scheduler interval / timeout / lease     | 热加载                           | worker 每轮从 system settings 重读                     |
| Trace retention                          | 热加载                           | maintenance 每轮读取 system setting                    |
| Node 最大 Sandbox 数                     | 热加载，但不属于 System Settings | Admin -> Sandbox Nodes 直接更新 Kubernetes annotation  |
| 用户/租户 quota override                 | 热加载，但不属于 System Settings | Admin -> Quotas，按 subject 独立管理                   |
| Auth secret / token TTL / Admin key      | 重启                             | issuer/verifier 在进程启动时构造，必须跨进程一致       |
| 服务地址 / Postgres / Redis / MinIO      | 重启                             | listener、连接池和客户端在启动时构造                   |
| Agent Run timeout / Sandbox lease        | 重启                             | 跨 Gateway、Agent、token TTL 的启动期校验必须一致      |
| LLM timeout / retry / rate limit         | 重启                             | resilience policy 与 limiter 在 LLM Gateway 启动时构造 |

## 4. 已废弃配置

以下配置已删除，不再兼容别名：

| 已废弃                                            | 替代方案                                |
| ------------------------------------------------- | --------------------------------------- |
| `COCOLA_LLM_CONFIG`                               | Admin -> Models / Postgres              |
| `COCOLA_LLM_PROVIDER`、`COCOLA_LLM_DEFAULT_ALIAS` | Admin -> Models / Postgres              |
| `COCOLA_ANTHROPIC_*`、`COCOLA_OPENAI_*` 路由配置  | Admin -> Models / Postgres              |
| `COCOLA_ADMIN_BASE_URL`                           | `COCOLA_ADMIN_URL`                      |
| `COCOLA_AUTH_DEV_ANON`                            | `COCOLA_AUTH_ALLOW_ANON`                |
| `COCOLA_AGENT_API_KEY`                            | Gateway 每个 Run 下发的用户 token       |
| `COCOLA_SANDBOX_PROVIDER`                         | OpenSandbox 是唯一生产后端              |
| `COCOLA_ENV`                                      | 无；启动模式由 `make dev/prod` 明确选择 |
| `COCOLA_LOG_LEVEL`、`COCOLA_SERVICE_NAME`         | 已删除未接线的假配置；服务名由进程固定  |

Fake LLM provider 只允许测试代码直接构造，不允许通过 Admin 创建，也不会从生产
Postgres 加载。

## 5. 变更规则

新增配置前必须回答：owner 是哪个进程、来源是什么、是否热加载、默认值和非法值
如何处理、是否是 secret。能由现有状态推导的配置不新增；临时灰度开关在功能全量后
必须连同分支和文档一起删除。
