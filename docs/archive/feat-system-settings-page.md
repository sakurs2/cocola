# feat: system settings page

- 变更时间：2026-07-05 20:56 (+08:00)
- 关联提交：待提交

## 变更理由

需要建设配置管理页面，集中收纳系统运行参数。初版采用启动时环境变量提供默认运行配置，管理员在页面中写入数据库覆盖值；仅对当前架构能够真实热更新的 scheduler 参数开放编辑，secret 与启动期依赖参数只展示配置状态，避免产生“页面保存成功但运行时不生效”的误导。

## 变更内容

- db/migrations/00018_system_settings.sql：新增 `system_settings` 表，保存配置覆盖值、版本号和更新审计字段。
- apps/admin-api/internal/store：为内存和 Postgres store 增加系统配置 CRUD，支持乐观版本校验。
- apps/admin-api/internal/service/settings.go：定义首批配置项、默认值、env 来源、敏感字段脱敏、只读/热更新边界和校验逻辑。
- apps/admin-api/internal/service/scheduler.go：scheduler 每轮读取动态配置，支持热更新 poll/run timeout/heartbeat/lease/enabled。
- apps/admin-api/internal/service/scheduled_tasks.go：最小调度间隔改为读取动态配置，仍强制默认至少 1 小时。
- apps/admin-api/internal/httpapi：新增 `/admin/settings` 查询、更新、重置接口。
- apps/web/app/admin/settings/page.tsx：新增管理员配置管理页，展示默认/env/db 来源、可编辑热更新项、secret 配置状态和重置操作。
- apps/web/app/api/admin/settings：新增 Next 同源代理接口。
- apps/web/app/admin/page.tsx、apps/web/components/admin/admin-shell.tsx：接入独立的 Settings 管理入口。
- .env.example、apps/admin-api/cmd/admin-api/main.go：补齐 scheduler 配置默认项说明。
- 测试：新增 service/httpapi 配置相关测试，覆盖 secret 脱敏、只读拒绝、最小间隔校验、DB override/reset 和 HTTP 路由契约。

## 关键取舍 / 注意事项

- 默认配置源选择 env，不引入 JSON 配置文件；数据库只保存管理员运行时覆盖值。
- secret 不允许在页面读取或编辑，只返回 configured/not configured。
- `auth.token_ttl_secs`、网关地址、Redis/Postgres 等启动期依赖参数初版只读，后续如要热更新需要改造对应运行时组件的配置读取模型。
