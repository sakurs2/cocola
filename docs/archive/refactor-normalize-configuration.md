# refactor: 收敛配置所有权与热加载边界

- 变更时间：2026-07-12 13:50 (+08:00)

## 变更理由

MVP 演进后，同一含义可能同时由环境变量、模型 JSON、Admin DB 和 Redis override
表达。Web、Agent Runtime 与 LLM Gateway 因此可能看到不同模型目录；Warm Pool 的
管理页写入也可能在 Redis 发布失败后显示成功但实际未生效。部分只读启动配置被
Admin 页面展示为“系统设置”，模糊了是否能热加载和需要重启的边界。

## 变更内容

- `apps/llm-gateway`：模型路由只从 Admin/Postgres 加载；删除文件/env provider
  fallback；模型热刷新复用未变化的 Registry，并在活跃请求释放后关闭旧客户端。
- `apps/agent-runtime`：统一使用 `COCOLA_ADMIN_URL`；删除第二份模型 JSON 校验；
  Admin Prompt 拉取失败时使用 last-known-good，无缓存时拒绝无策略运行。
- `apps/admin-api`、`apps/sandbox-manager`：管理页只保留真正热加载的 Scheduler、
  Warm Pool sizing 与 Trace 设置；Warm Pool sizing 以 Postgres 为准，通过 Redis
  投递并周期对账；投递值缺失或异常时暂停扩缩容，provisioning 仍为启动配置。
- `apps/gateway`、`deploy`、`scripts`、`apps/web`：统一匿名鉴权和 Admin URL 命名，
  删除永久开关、sandbox provider 选择、Agent API key 和旧 LLM 配置变量；LLM
  Gateway 与 Gateway 共享鉴权配置。
- `packages/go-common/config`：删除从未被服务读取的 `COCOLA_ENV`、
  `COCOLA_LOG_LEVEL`、`COCOLA_SERVICE_NAME` 假配置定义。
- `db/migrations/00031_remove_warm_pool_runtime_settings.sql`：清理废弃 Warm Pool DB
  设置和生产 Fake provider 数据；补齐 Goose Up/Down section，并增加 embedded
  migration 结构测试，避免格式错误阻断服务启动。
- `docs/configuration.md`、`docs/adr/0020-configuration-ownership-and-reload.md`：记录
  配置 owner、生效方式、废弃项和新增规则。

关键取舍：不增加配置中心或消息队列；Redis 不再作为配置真源。启动配置通过一次
明确重启生效，换取没有“保存成功但进程仍使用旧值”的分裂中间态。
