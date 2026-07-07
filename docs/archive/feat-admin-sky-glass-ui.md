# feat: Admin Sky-Glass UI

- 变更时间：2026-07-08 01:11 (+08:00)

## 变更理由

用户侧已完成 sky-glass 视觉升级后，Admin 后台仍保持原先的朴素布局和默认卡片样式，和新的 cocola 工作台视觉不一致。本次按“克制运维版”方案升级 Admin 前端 UI，只调整 shell、设计 token 和首页 surface，不改变 API、路由、权限或数据流。

## 变更内容

- apps/web/components/admin/admin-shell.tsx：重构 AdminShell 为固定左侧 glass sidebar、sticky admin header 和独立 Back to chat 入口。
- apps/web/app/globals.css：新增 `.cocola-admin-ui` scoped 设计变量与 admin glass surface、control、table 等通用样式。
- apps/web/app/admin/page.tsx：将 Admin overview 的 metric 和模块卡片改为克制版玻璃面板。
- 关键取舍：通过 admin 作用域样式批量提升现有业务页面观感，避免逐页重写业务组件或触碰数据逻辑。
