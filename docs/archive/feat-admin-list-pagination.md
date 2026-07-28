# feat: 为 Admin 大数据列表增加分页

- 变更时间：2026-07-28 23:20 (+08:00)

## 变更理由

Admin 的 Skills、Agent Runs 和 Session Storage 数据会持续增长。原页面要么一次渲染全部数据，要么只提供不明显的翻页箭头；其中 Agent Runs 在结果数刚好等于页大小时还可能进入一个空白下一页。

## 变更内容

- `apps/web/components/admin/admin-ui.tsx`：新增统一、可见且支持总数/无总数两种模式的 Admin 分页控件。
- `apps/web/app/admin/skills/page.tsx`：按 24 条请求和展示 Skill，并处理删除最后一页数据后的页码回退。
- `apps/web/app/admin/audit/page.tsx`：按 50 条展示 Agent Run，通过多取一条判断是否存在下一页，避免进入空白页。
- `apps/web/app/admin/storage/page.tsx`：按 25 条展示 Session Storage，并继续显示全量 PVC 数量和请求容量汇总。
- `apps/admin-api/internal/httpapi/handlers.go`：为 Skills 和 Session Storage 增加可选的 `limit` / `offset`、总数和汇总元数据；未传分页参数时保留原有全量响应语义。
- `apps/admin-api/internal/httpapi/api_test.go`、`apps/web/lib/admin-pagination.test.mjs`：覆盖分页边界、兼容语义和页面接线。
