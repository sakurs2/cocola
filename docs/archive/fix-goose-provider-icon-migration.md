# fix: 修复 Provider 图标迁移的 goose 分段标记

- 变更时间：2026-08-01 18:34 (+08:00)

## 变更理由

Provider 图标 migration 使用了 `migrate:up/down` 标记，而 cocola 的数据库迁移统一由 goose 执行。远端 CI 在继续执行完整 Go workspace 测试时，通过 `TestEmbeddedSQLMigrationsHaveGooseSections` 检测到该文件缺少 goose 分段，因此 Go job 失败。

## 变更内容

- `db/migrations/00056_llm_provider_icons.sql`：将迁移分段改为 `+goose Up` 和 `+goose Down`，与项目现有 migration 格式保持一致。
- 该调整只修复 migration 元数据，不改变 Provider 图标字段、回填和回滚 SQL 的业务语义。

## 验证

- `go test ./db/...`
- `go test ./apps/admin-api/... ./apps/gateway/... ./apps/cli/... ./db/... ./packages/go-common/... ./packages/proto/gen/go/...`
