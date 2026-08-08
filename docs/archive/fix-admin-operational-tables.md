# fix: 完善 Admin 运维表格与操作反馈

- 变更时间：2026-08-08 12:03 (+08:00)

## 变更理由

Admin 的宽表在 macOS 上缺少可见的横向滚动提示，部分 Actions 列固定在视口右侧且横向堆叠按钮；Sandbox 长 ID 占用过多空间并缺少可用操作。Settings 的 Kubernetes quantity 也要求用户直接输入带单位字符串。Storage 页面则没有清晰反馈测量进度，并把 PVC 缺失、孤立 Volume 和失效绑定混在同一个语义中。与此同时，多数页面把操作错误作为横条插入布局，造成内容跳动且反馈不集中。

## 变更内容

- `apps/web/components/admin/admin-ui.tsx`、`apps/web/app/globals.css`：新增共享 Admin DataGrid、紧凑行操作菜单和 HeroUI 错误弹窗；为 Admin 宽表提供始终可见的横向滚动轨道，并增强长文本复制按钮的键盘可见性。
- `apps/web/app/admin/audit`、`models`、`sandbox-nodes`、`sandboxes`、`scheduled-tasks`、`storage`、`token-usage`、`users`：统一宽表滚动行为，移除固定 Actions 列；Nodes、Sandboxes 和 Storage 改为纵向下拉操作菜单。
- `apps/web/app/admin/sandboxes/page.tsx`：收紧列宽，合并 Session/User 信息，长 ID 使用省略、悬浮全文和复制交互，并为所有 Sandbox 提供确认后的删除入口。
- `apps/web/app/admin/settings/page.tsx`：quantity 改为数值输入与 MB/GB 单位选择，提交时继续序列化为 Kubernetes 兼容的 `Mi`/`Gi`。
- `apps/web/app/admin/storage/page.tsx`：重做容量概览和测量状态；Missing 且无有效对话时沿用 orphan 清理接口移除失效绑定，仍有关联对话时只提示下次运行自动创建新 Volume，不提供误删操作；批量清理统一覆盖 orphan Volume 与可清理的 Missing 绑定。
- `apps/web/app/admin/{architecture,component-logs,mcps,models,scheduled-tasks,settings,skills,storage,traces,users}`：页面级操作错误迁移到统一 HeroUI 弹窗，保留表单、抽屉和任务记录中的上下文内联错误。
- `apps/web/lib/admin-operational-ui.test.mjs`、`admin-pagination.test.mjs`：新增宽表、行操作、错误弹窗、quantity 和 Missing 清理语义的回归契约。
- `apps/web/lib/preview-ws-routing.test.mjs`：让已有自定义 Web Server 契约匹配当前 `exec_in_session` 启动封装，消除陈旧断言导致的 CI 失败。

## 验证

- `pnpm exec tsc --noEmit -p apps/web/tsconfig.json`
- `pnpm --filter @cocola/ui-compat exec tsc --noEmit`
- `pnpm --filter @cocola/web lint`
- `node --test apps/web/lib/*.test.mjs`（201 项通过）
- `pnpm --filter @cocola/web build`
