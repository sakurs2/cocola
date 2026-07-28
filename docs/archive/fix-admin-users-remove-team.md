# fix: 移除 Admin Users 的 Team 字段

- 变更时间：2026-07-29 01:04 (+08:00)

## 变更理由

Admin Users 当前把后端兼容字段 `tenant_id` 包装为 Team，并在统计页搜索、列表和创建 / 编辑表单中暴露该概念。Cocola 当前不需要用户团队管理，这个字段会制造无效列、空值和不必要的配置入口。

## 变更内容

- `apps/web/app/admin/users/page.tsx`：移除 Team 列、Team 搜索、Team 选项和新建 Team 输入。
- 创建与编辑用户时不再提交 `tenant_id`；后端可选字段和数据库结构保持不变，避免引入破坏性迁移。
- Users 表格从六列缩减为五列，并同步调整空状态的跨列数和最小宽度。
