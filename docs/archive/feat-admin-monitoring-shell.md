# feat: admin monitoring shell

- 变更时间：2026-07-04 18:42 (+08:00)

## 变更理由

admin 监控页面后续会包含多个模块，需要先把当前 web 已支持的 admin 能力按功能汇总到统一入口，并提供页内侧边栏用于切换不同监控页面。

## 变更内容

- `apps/web/app/admin/page.tsx`：新增 Admin Monitoring 汇总页，展示 Users 与 Sandbox Nodes 两个当前已支持模块。
- `apps/web/app/admin/layout.tsx`：为 `/admin` 路由组增加统一 admin layout。
- `apps/web/components/admin/admin-shell.tsx`：新增 admin 专用侧边栏，按 Overview、Access、Infrastructure 分组。
- `apps/web/components/assistant-ui/app-sidebar.tsx`：聊天侧边栏将多个 admin 入口收敛为单个 Admin 入口。
- `apps/web/app/admin/users/page.tsx`：修复 response error 类型收窄，避免 `next build` 类型检查失败。
- 验证：`pnpm --filter @cocola/web lint` 与 `pnpm --dir apps/web build` 通过。
