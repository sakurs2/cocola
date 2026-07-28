# fix: 统一 Admin 删除确认并支持批量清理 orphan

- 变更时间：2026-07-28 23:31 (+08:00)

## 变更理由

Admin 的 Session Storage 和 Sandboxes 删除操作仍使用浏览器原生确认框，视觉和交互与 Cocola 不一致。Session Storage 只能逐条清理 orphan，在 orphan 较多时操作成本过高。

## 变更内容

- `apps/web/app/admin/storage/page.tsx`：用 Cocola Admin 确认对话框替换原生确认框，增加一键删除全部 orphan 的入口、删除进度和部分失败反馈。
- `apps/web/app/admin/sandboxes/page.tsx`：用统一确认对话框替换浏览器原生确认框。
- `apps/admin-api/internal/service/session_storage.go`：服务端枚举并逐项安全删除当前标记为可删除的 Session Volumes，单项失败不阻断其他 orphan。
- `apps/admin-api/internal/httpapi/api.go`、`handlers.go`：新增批量删除接口，并在分页列表中返回全量 orphan 数量。
- `apps/web/app/api/admin/session-storage/orphans/route.ts`：代理批量清理请求。
- `apps/admin-api/internal/httpapi/api_test.go`、`apps/web/lib/admin-pagination.test.mjs`：覆盖批量删除、部分失败和统一对话框接线。
