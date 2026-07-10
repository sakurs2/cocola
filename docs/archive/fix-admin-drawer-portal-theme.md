# fix: 修复 Admin Drawer Portal 主题丢失

- 变更时间：2026-07-10 16:51 (+08:00)

## 变更理由

Radix Dialog Portal 会把 Drawer 挂载到 `body`，使其脱离 `.cocola-admin-ui` 主题容器。Admin Drawer 因此无法继承 Sky Glass token，也匹配不到浅色玻璃背景规则，在全局 dark class 存在时会显示为黑色。

## 变更内容

- `apps/web/components/admin/admin-ui.tsx`：让 Portal 中的 Admin Drawer 自身携带 `.cocola-admin-ui` 主题作用域，并显式应用 Admin 前景色，避免继承 `body.dark` 的白色文字。
- `apps/web/components/admin/admin-shell.tsx`：同步修复使用相同 Portal 机制的移动端 Admin 导航及其文字颜色。
- `apps/web/app/globals.css`：让 Sky Glass 背景规则同时支持主题类与 Drawer/移动导航位于同一元素。
