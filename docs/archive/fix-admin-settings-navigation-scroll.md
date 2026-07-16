# fix: Admin Settings 跟随侧栏导航滚动

- 变更时间：2026-07-16 21:05 (+08:00)

## 变更理由

Admin 侧栏将 Settings 与“返回工作区”一起放在固定 footer 中，而 Overview、
Configuration、Operations 和 Infrastructure 位于独立的滚动容器。导航项超过视口高度
后，Settings 会固定在左下角，无法与其他 Tab 一起滚动，移动端抽屉也存在同样的结构
差异。

## 变更内容

- `apps/web/components/admin/admin-shell.tsx`：把 Settings 放入导航滚动区中的 System
  分组，桌面侧栏和移动抽屉共用相同顺序与滚动语义。
- 固定 footer 只保留“返回工作区”，继续提供始终可见的退出 Admin 操作。
- 不修改现有 Admin 视觉 token、选中态动画、折叠侧栏和路由匹配逻辑。
