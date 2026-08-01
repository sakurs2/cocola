# fix: 修复 Provider 图标配置与 Web CI

- 变更时间：2026-08-01 18:30 (+08:00)

## 变更理由

远端 GitHub Actions 的 Web 任务因存量前端文件未通过 Prettier 而失败，导致 lint、测试与构建未继续执行。与此同时，Admin 的 Provider 配置缺少图标选择和持久化链路，README 在首个公开版本前也需要完善品牌展示与项目元信息。

## 变更内容

- `apps/web/app/admin/models/page.tsx`：为 Provider 增加品牌图标或 HTTPS 图片地址选择，并在列表中展示已保存的图标。
- `apps/admin-api/internal/{httpapi,service,store}`：贯通 Provider 图标的请求、校验、存储和更新逻辑。
- `db/migrations/00056_llm_provider_icons.sql`：新增 Provider 图标字段，并为现有数据补充兼容默认值。
- `apps/web/lib/admin-provider-icons.test.mjs` 与 Admin API 测试：覆盖 Provider 图标提交、校验和存储回读。
- `apps/web/components/assistant-ui/thread.tsx` 与 `apps/web/app/agents/[id]/page.tsx`：修复格式检查后暴露的 Agent 欢迎提示词和测试入口回归。
- Web 相关文件：统一执行 Prettier，修复远端 `web-format-check` 失败。
- `README.md`、`docs/assets/` 与 `scripts/export-readme-brand.mjs`：完善英文项目介绍、品牌素材、架构图和 badges，移除 GitHub Stars 数量 badge。

## 验证

- `make format-check`
- `go test ./apps/admin-api/...`
- `node --test apps/web/lib/*.test.mjs`
- `pnpm --filter @cocola/web lint`
- `pnpm --filter @cocola/web build`
- `git diff --check`
