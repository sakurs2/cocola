# feat: 用户侧 WebUI 白色主题与模型图标长期方案

- 变更时间：2026-07-07 22:35 (+08:00)
- 关联提交：待提交

## 变更理由

用户希望先优化 cocola 用户侧 WebUI，切换到白色背景，并引入更适合 Agent 聊天产品的交互与视觉栈；同时希望评估并落地 lobehub/lobe-icons，用于统一模型/供应商图标体系。当前 assistant-ui 负责聊天 runtime 和消息基础能力，但默认视觉与动效较弱，模型图标也缺少稳定的 provider/family/icon_slug 契约。

## 变更内容

- apps/web：将用户侧 workspace 切换为白色设计语言，优化侧边栏、顶部 workspace、消息卡片、composer、artifact 预览等视觉层级。
- apps/web/components/assistant-ui：引入 Framer Motion、Radix Tooltip/Popover、cmdk 等交互能力，优化模型选择器、命令面板、消息入场与按钮 tooltip。
- apps/web/app/page.tsx：接入 Monaco Editor 作为代码/文本 artifact 预览能力。
- apps/admin-api：公共模型接口扩展 provider、family、icon_slug，并从现有 route 信息中推导模型家族与图标 slug，保留旧 icon 字段兼容。
- apps/gateway：聊天请求与历史消息 metadata 透传 model_provider、model_family、model_icon_slug，并兼容 lobe-icons/simple-icons/image 三类图标配置。
- apps/web/app/api/model-icons/[slug]：新增本地 Lobe Icons 静态 SVG 读取路由，使用 @lobehub/icons-static-svg 避免 React 19 peer dependency 风险。
- apps/web/lib/model-icons.ts：新增 Lobe icon slug 归一化和旧 slug 别名映射，保留现有本地图标兜底。
- 测试与校验：前端 lint/build 通过；admin-api service 测试通过；gateway httpapi 除沙箱端口受限的 metrics 用例外通过。
