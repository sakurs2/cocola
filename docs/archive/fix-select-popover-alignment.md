# fix: 统一下拉选择器边界与弹层对齐

- 变更时间：2026-07-28 00:25 (+08:00)

## 变更理由

Web 端多处表单使用原生 `<select>`。在 macOS 上，浏览器会交给系统绘制箭头和选项菜单，导致箭头过度贴近右侧边框，展开菜单也可能相对触发框横向偏移。不同 Radix primitive 间的 dismissable-layer 版本不一致还会让嵌套 Select 的 Escape 事件同时关闭外层 Dialog。

## 变更内容

- `apps/web/components/ui/select-control.tsx`：新增基于 Radix Select 的共享选择器，统一箭头间距、焦点态、选中态、弹层宽度和 Portal 主题。
- `apps/web/app/`、`apps/web/components/scheduled-tasks/task-drawer.tsx`：将用户端和管理端共 26 处原生 `<select>` 替换为共享组件，保留原有状态与禁用逻辑。
- `apps/web/package.json`、`pnpm-lock.yaml`：声明 Radix Select 直接依赖。
- `package.json`：统一 Radix dismissable-layer 版本，保证 Select 嵌套在 Dialog 时只关闭最上层弹层。
- 浏览器实测触发框与浮层左右边界偏差均为 0px，箭头距右边框 13px；Escape、Dialog 清理和用户端/管理端主题均正常。
