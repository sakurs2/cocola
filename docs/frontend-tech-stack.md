# cocola 前端技术选型与设计系统

本文是 `apps/web` 的前端技术选型、品牌视觉与组件使用边界的单一说明入口。进行用户侧 WebUI、Admin UI、聊天交互或前端依赖调整前，应先阅读本文，并以当前源码和 `apps/web/package.json` 为最终事实来源。

## 1. 技术基线

| 领域           | 统一方案                                      | 职责与当前状态                                                           |
| -------------- | --------------------------------------------- | ------------------------------------------------------------------------ |
| 应用框架       | Next.js 14 App Router + React 18 + TypeScript | 路由、BFF、SSR/CSR 边界与应用外壳                                        |
| 聊天基础能力   | assistant-ui `ExternalStoreRuntime`           | 消息流、composer、附件、reasoning、tool call、取消与自动滚动，已接入     |
| 样式系统       | Tailwind CSS 3 + shadcn 语义 token            | 布局、颜色、间距、响应式和基础视觉，已接入                               |
| 复杂交互       | Radix UI + shadcn 风格组件                    | Dialog、Popover、Dropdown Menu、Tooltip，按需封装                        |
| 命令面板       | cmdk                                          | 全局命令菜单和可搜索选择器，已接入                                       |
| 组件动效       | Framer Motion                                 | 用户/Admin 侧栏、消息/欢迎区、composer、弹层和 artifact 面板过渡，已接入 |
| 细节动效       | CSS Animation + Vivus                         | 流式状态、hover/focus、品牌字标和低频扫光                                |
| 文件与代码预览 | Monaco Editor                                 | 代码和文本 artifact 预览，客户端动态加载                                 |
| 数据图表       | Chart.js + react-chartjs-2                    | Admin 监控与用量图表，已接入                                             |
| 流程可视化     | React Flow                                    | Agent 流程、架构和任务链路的候选方案，尚未引入                           |
| 模型图标       | Lobe Icons Static SVG                         | 模型和供应商品牌图标，已接入                                             |

不要为了单个效果重复引入职责重叠的库。当前动效优先使用 Framer Motion 和 CSS；除非出现时间轴编排、粒子或复杂路径动画等明确需求，否则不引入 anime.js 或 mo.js。

## 2. 聊天架构边界

assistant-ui 负责聊天语义和交互 primitive，不负责 cocola 的视觉品牌或后端协议。`CocolaRuntimeProvider` 持有会话、模型、sandbox、artifact、历史记录和 SSE 状态，通过 `useExternalStoreRuntime` 转换为 assistant-ui 可消费的消息。

```text
Gateway SSE
  -> CocolaRuntimeProvider / UiMessage / UiPart
  -> assistant-ui ExternalStoreRuntime
  -> Thread / Message / Composer / Attachment / Tool Card
```

- 不绕过 runtime 另建一套聊天消息状态。
- 新的消息 part 或 SSE event 先在 runtime adapter 中完成类型化和容错，再交给渲染层。
- `environment_status` 是当前 Agent 会话首次初始化的环境快照，包含已加载的 Skills 和 MCP 连接状态；不写入消息历史，也不在正常 follow-up 中重复检查。它与 artifact 共用右侧 Context Dock，移动端使用覆盖面板。
- `environment_prepare` 是某一轮消息的阻塞性环境准备快照，仅在绑定新 sandbox 时产生，并作为消息的第一个 Rail 节点持久化；复用现有 sandbox 时不产生。它使用开放的 `schema_version + part_id + state + components[]` 结构，当前只展示 workspace、checkpoint、attachments 和 Skills，不包含 MCP。
- Environment 已完成但首个模型输出尚未到达时，消息 Rail 根据本地 running 状态显示临时的 `Starting response` 节点；首个 reasoning、text、tool 或 file part 到达后自动替换。该节点不进入 SSE 协议和消息历史。
- composer、附件、消息流和 tool call 优先复用 assistant-ui primitive。
- cocola 的视觉组合集中在 `components/assistant-ui/`，不要修改 assistant-ui 内部实现。

## 3. 字体规范

### 3.1 UI 字体

主页面和 Admin 的界面字体统一使用 **Geist Sans**，通过 `font-sans` 全局应用。中文依次回退到 `PingFang SC`、`Microsoft YaHei`、`Noto Sans SC` 和系统无衬线字体。

适用范围：

- 导航、标题、正文、按钮、表单、表格和消息内容。
- 除代码、日志和机器标识外的所有产品界面文案。

### 3.2 代码字体

代码、终端、日志、快捷键和机器标识统一使用 **Geist Mono**，通过 `font-mono` 应用。

适用范围：

- Markdown 代码块、Monaco Editor、终端命令和工具参数。
- trace ID、session ID、endpoint、时间精度数据和日志正文。
- 不要把 `font-mono` 用于普通按钮、标题或大段产品文案。

### 3.3 品牌字体

- `CocolaWordmark` 是 SVG 手写字标，不依赖系统字体。
- `Cormorant Garamond Italic 500` 仅用于品牌 tagline。
- 品牌字标和 tagline 只用于登录页、空会话欢迎区等品牌场景，不用于 Admin 标题和普通业务页面。
- 所有字体均本地加载，不增加 Google Fonts 或其他字体 CDN 请求。

## 4. 图标规范

图标按语义分层，不按页面临时选择。

### 4.1 Phosphor Icons：产品领域与 Agent 语义

用户侧和 Admin 控制面的领域 icon 使用 **Phosphor Icons**，优先采用 `duotone`，用于：

- Skills、MCP、Tasks、Chats 等一级导航。
- 展开/收起侧边栏和用户工作台功能入口。
- reasoning、answer、tool call、terminal、search、file 等 Agent 执行 rail 节点。
- Admin 一级导航、页面身份和控制域模块入口。

Phosphor 的视觉更柔和、更有产品性，是 cocola 用户工作台与 Admin 控制面共享的领域图标语言。

### 4.2 Lucide：通用操作与密集数据工具

**Lucide React** 保留为通用操作和 Admin 工具图标，适用于：

- 发送、复制、关闭、下载、展开箭头、刷新等标准操作。
- Dialog、Popover、表单和表格中的工具按钮。
- Admin 监控、审计和配置页面中的行操作与通用控件。

同一个功能区域不要让两套图标承担相同职责。新增领域入口和页面身份时优先 Phosphor；新增标准操作和密集数据工具时优先 Lucide。

### 4.3 品牌与模型图标

- cocola 品牌统一使用 `CocolaLogo`，不得用普通 Sparkle 图标临时代替。
- 模型和供应商统一使用 Lobe Icons，通过 `ModelIconConfig` 和本地 SVG route 加载。
- 模型图标回退顺序为：Lobe Icons -> 本地 Simple Icons -> 字母 badge。
- 不把 Lobe Icons 用作普通 UI 操作图标。

## 5. 视觉系统

Tailwind 使用 shadcn 风格的语义变量，如 `background`、`card`、`popover`、`muted`、`accent`、`border` 和 `sidebar`。业务组件优先使用语义类，不直接复制具体 HSL 值。

- `.cocola-user-ui`：用户工作台。白色基底、sky-glass、透明侧栏和较轻的云层氛围。
- `.cocola-admin-ui`：Admin 运维界面。采用 Sky Glass Control Plane：沿用 sky-glass，并以低对比拓扑网格作为唯一环境装饰；背景更实、动效更少，优先保证表格、日志和表单可读性。
- 品牌主色为 sky blue 到 violet；成功、警告、错误等状态色保持明确的功能语义。
- 空会话可以有品牌氛围；进入长文本、代码、日志和数据密集页面后应降低装饰强度。

shadcn/ui 在本项目中表示组件组织方式和 token 约定，不代表必须通过 CLI 批量生成完整组件库。新增复杂组件时优先封装 Radix primitive，并复用现有 `components/ui/` 模式。

## 6. 动效规范

- Framer Motion 负责组件级状态变化，例如侧栏宽度、面板进入/退出和 composer focus lift。
- Admin 动效只用于侧栏折叠、激活导航、页面轻量入场和 Drawer/Dialog；表格行、日志与长列表不做入场动画。
- CSS Animation 负责消息轻量入场、流式边框、hover/focus 和细小状态反馈。
- Vivus 只服务 `CocolaWordmark` 品牌书写动画。
- 常规过渡建议保持在 160-240ms；侧栏等空间变化可以使用克制的 spring。
- 所有非必要动效必须支持 `prefers-reduced-motion`，不能影响输入、滚动和长文本阅读。

## 7. 专项组件边界

- Monaco Editor 仅在代码或结构化文本需要编辑器体验时加载，并保持 `ssr: false` 动态加载。
- Chart.js 是当前 Admin 图表标准；不要在同一后台同时引入 Recharts 或 ECharts，除非现有方案无法满足明确需求。
- React Flow 只在需要节点、边、缩放、拖拽和自动布局的真实图场景中引入；普通步骤列表或状态时间线继续使用常规 React 组件。
- cmdk 用于命令搜索和大型可搜索选项集；小型固定选项使用 Dropdown Menu、Tabs 或原生控件。
- Tasks 使用独立 `/tasks` 页面和 `TaskDrawer`：用户侧以真实任务卡片呈现并负责创建、编辑和启停；Admin 侧只提供紧凑管理表格、只读详情和删除能力。用户表单统一 Once / Hourly / Daily / Weekly / Monthly、时区与过期校验；不向用户暴露 Cron 或 Interval，新旧任务兼容逻辑留在 API 与调度层。
- 定时任务始终归属于用户并通过 Gateway 以 Owner 身份执行，结果复用固定 Conversation 进入 Chat History。Admin 只有跨用户管理权限，不是一种任务类型，也不提供无归属任务创建入口。
- Admin MCP 配置遵循“列表即状态、Drawer 即编辑”的单层结构。保存时只校验并安全持久化配置，不额外申请 sandbox；连接能力由首次真实 Agent 会话自然验证，不增加独立测试、健康页或发布状态。远程 URL 作为完整 secret 输入，界面只展示移除 userinfo、query 和 fragment 后的 `url_hint`。
- Session Status 使用一份完整环境快照：agent-runtime 在 Skill 同步成功后补入 `kind=skill` 的 Loaded 组件，sandbox shim 继续提供真实的 `kind=mcp` 连接状态；前端按 Skills 与 MCP servers 分组并允许独立折叠，不在浏览器合并多个局部快照。
- Environment 消息节点与 Session Status 职责分离：前者解释本轮为何尚未开始，只呈现阻塞性的环境准备；后者呈现会话能力和异步 MCP 连接。Gateway 以原始 JSON 保存 Environment 快照并按稳定 `part_id` 原位更新，未知 component 与字段不得被中间层丢弃。
- MCP 连接终态必须来自实际执行首次对话的同一个 `ClaudeSDKClient`，不得通过 system prompt 注入，也不得申请额外 sandbox。模型请求在 client 初始化后立即开始；仅当 SDK 报告 `pending` 时进行最多 8 秒的有界查询，终态到达后停止，并通过 `environment_status` SSE 快照更新 Session Status。有 `resume` 的 follow-up 使用 one-shot SDK 路径且不重复查询状态。该状态表示会话初始化时的加载结果，不是持续健康监控。

## 8. 关键源码入口

- `apps/web/app/layout.tsx`：字体加载和全局应用外壳。
- `apps/web/tailwind.config.ts`：字体 fallback 与语义 token 映射。
- `apps/web/app/globals.css`：用户侧/Admin token、sky-glass 和动效。
- `apps/web/app/runtime-provider.tsx`：assistant-ui runtime 和 SSE 适配。
- `apps/web/components/assistant-ui/thread.tsx`：聊天界面组合。
- `apps/web/components/assistant-ui/session-status-panel.tsx`：当前会话环境与 MCP 加载状态的 Context Dock。
- `apps/web/components/assistant-ui/app-sidebar.tsx`：用户侧 Phosphor 导航体系。
- `apps/web/app/tasks/page.tsx`、`apps/web/components/scheduled-tasks/task-drawer.tsx`：用户任务卡片页与用户/Admin 共享编辑表单。
- `apps/web/components/assistant-ui/rail.tsx`：Agent 过程 Phosphor 图标体系。
- `apps/web/components/admin/admin-shell.tsx`：Admin 响应式侧栏、移动导航和控制面上下文。
- `apps/web/components/admin/admin-ui.tsx`：Admin Page、Panel、Metric、Table、状态与 Drawer primitives。
- `apps/web/components/cocola-logo.tsx`：cocola 品牌标志。
- `apps/web/lib/model-icons.ts`：Lobe Icons slug 和回退规则。

若调整上述技术选型、字体、图标职责或主题 token，应同步更新本文，并在 `docs/archive/` 记录变更理由。
