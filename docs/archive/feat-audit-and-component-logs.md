# feat: 审计日志与组件日志初版

- 变更时间：2026-07-06 11:49 (+08:00)
- 关联提交：待提交

## 变更理由

系统需要新增日志模块：一类是用户行为审计日志，覆盖服务端可见的用户/API 行为，聊天行为只记录元数据而不记录 prompt、回答正文或附件内容；另一类是 infra 组件运行日志，v1 不新增日志组件，先复用本地/部署环境已有 stdout 日志文件进行查看。

## 变更内容

- db/migrations/00019_audit_events.sql：新增 `audit_events` 结构化审计表，兼容迁移旧 `audit_log` 数据。
- apps/admin-api：新增结构化审计事件模型、Postgres/Memory 查询与写入、HTTP 审计 middleware、`/admin/audit-events` 查询接口，并保留旧 `/admin/audit` 兼容语义。
- apps/gateway：对聊天、会话、消息、artifact 下载写入审计事件，聊天审计仅包含会话、模型、附件数量、artifact 数量和耗时等元数据。
- apps/web：新增 Admin Logs 分组、Audit Logs 分页页面、Component Logs 页面与本地日志读取 API。
- packages/go-common / packages/py-common：统一结构化日志中的 `service`、`component` 字段，新增 audit 写失败指标和 trace id helper。
- 关键取舍：组件日志 v1 不接 Loki/OpenSearch，默认读取 `.run-logs/*.log`，也支持 `COCOLA_COMPONENT_LOG_DIR` 指向部署日志目录；审计写入保持业务优先，失败只记录日志和指标。
