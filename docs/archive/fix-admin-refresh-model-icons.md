# fix: 修复 Admin 刷新动画并扩充模型图标

- 变更时间：2026-07-10 14:34 (+08:00)

## 变更理由

Admin 页面原先直接用请求的 `loading` 状态控制刷新图标旋转。当接口响应较快时，动画只执行很小一段，看起来像按钮只轻微转动；部分页面的刷新图标甚至没有加载态动画。Models 配置页的品牌图标选项则只来自 7 项本地映射，没有利用项目已安装的 Lobe Icons 主流厂商资源。

## 变更内容

- `apps/web/components/admin/admin-ui.tsx`、`apps/web/app/globals.css`：新增共享 `AdminRefreshButton`，点击后保证一次完整旋转，请求较慢时持续旋转，并沿用 reduced-motion 降级规则。
- `apps/web/app/admin/**`：将 Architecture、Audit、Component Logs、Models、Prompts、Sandbox Nodes、Sandboxes、Scheduled Tasks、Settings、Token Usage、Trace Detail 和 Users 的刷新入口统一迁移到共享控件。
- `apps/web/lib/model-icons.ts`：复用现有 Lobe Icons SVG route，将模型品牌选项从 7 个扩充到 41 个，覆盖主流国际、国内和自托管模型供应商。
- `apps/web/app/admin/models/page.tsx`：将图标类型文案从 `Local icon` 调整为更准确的 `Brand icon`，不修改 API、数据结构和保存行为。
