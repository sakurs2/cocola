# feat: 统一 Admin Sky Glass Control Plane UI

- 变更时间：2026-07-10 13:17 (+08:00)

## 变更理由

用户工作台已形成 Geist、Phosphor Duotone、sky-glass 和克制动效的稳定视觉语言，但 Admin 仅有外层玻璃壳，内部页面仍大量使用旧式卡片、重复页面结构和 Lucide 领域图标。不同管理页面的表格、指标、弹层和焦点反馈也缺少统一边界，移动端没有可用的 Admin 导航。

## 变更内容

- `apps/web/components/admin/admin-shell.tsx`：重构为 Cocola 品牌、Phosphor Duotone 导航、64/272px 桌面折叠侧栏、移动端 Radix 导航和控制面上下文顶栏。
- `apps/web/components/admin/admin-ui.tsx`：新增 Page、Header、Panel、Metric、Toolbar、Table、Status、Alert、Empty State 和 Radix Drawer 等内部 primitives。
- `apps/web/app/admin/page.tsx`：Overview 改为按控制域分组的模块索引，移除无业务含义的静态模块计数，加入低对比拓扑视觉。
- `apps/web/app/admin/**`：主要页面身份改用 Phosphor Duotone；Users 与 Scheduled Task Run Detail 使用共享 Drawer；Token Usage 图表切换为浅色 Sky/Signal/Violet 配色。
- `apps/web/app/globals.css`：补齐 Cloud/Frost/Ink/Sky/Violet/Signal token、拓扑网格、玻璃 surface、表单焦点、sticky 表头、等宽数字、响应式和 reduced-motion 规则。
- `docs/frontend-tech-stack.md`：更新 Admin 图标职责、主题、动效边界和关键源码入口。

关键取舍：不修改 API、鉴权或页面数据逻辑，不新增依赖；领域身份使用 Phosphor，刷新、复制、删除等通用操作继续使用 Lucide。Admin 复用用户端品牌语言，但降低装饰和动效强度，优先保证运维信息密度。
