# fix: 优化 Admin 表格滚动与 Storage 操作反馈

- 变更时间：2026-08-08 12:39 (+08:00)

## 变更理由

Admin 宽表格的横向滚动条只出现在表格底部，长列表需要先滚到末尾才能横向操作。Storage 页面同时存在容量口径不清、批量清理文案容易被理解为删除全部 PVC，以及 Measure 执行期间缺少明显反馈的问题。

## 变更内容

- `apps/web/components/admin/admin-ui.tsx`：为共用 DataGrid 增加与表格双向同步的视口底部横向滚动条；增加基于 HeroUI Card 的居中操作提示。
- `apps/web/app/globals.css`：补充视口滚动条的紧凑样式，并复用现有语义色与表面色变量。
- `apps/web/app/admin/storage/page.tsx`：移除不代表真实磁盘占用的 Session requests 汇总和容易误解的批量清理入口；保留逐行的“删除孤儿 Volume”与“清理失效绑定”；Measure 与清理结果改为居中提示。
- `apps/web/lib/admin-operational-ui.test.mjs`、`apps/web/lib/admin-pagination.test.mjs`：覆盖滚动条同步、Storage 清理范围和 Measure 反馈。
- 关键取舍：请求容量只是软限制，不能替代节点容量或实测使用量，因此不作为 Storage 顶部容量指标展示；高风险清理操作保持逐行确认，避免批量按钮产生歧义。

## 验证

- `make web-format-check`
- `pnpm exec tsc --noEmit -p apps/web/tsconfig.json`
- `pnpm exec tsc --noEmit -p packages/ui-compat/tsconfig.json`
- `pnpm --filter @cocola/web lint`
- `node --test apps/web/lib/*.test.mjs`（202 项通过）
- `pnpm --filter @cocola/web build`
