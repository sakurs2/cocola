# feat: web 聊天 UI 改造为 Open WebUI 观感(深色主题 + 静态侧边栏 + 欢迎区)

日期：2026-07-01 · 关联 ADR：0007(gateway BFF + SSE 契约)/0009(Route-A 沙箱内 Claude Code)/0010(tool-use 透传)· 关联 Plan：docs/plan/web-ui-openwebui-look.md

## 背景
上一版补齐 shadcn 令牌层后,观感已对齐 assistant-ui 官方 demo。本次按产品诉求
把整体观感改造为 **Open WebUI 风格**:近黑深色主题、左侧持久侧边栏(New Chat /
Search / Notes / Workspace + Channels / Folders / Chats)、居中的 logo + 模型名
欢迎区、药丸输入条 + "Suggested" 建议列表。

以 assistant-ui 官方 **Grok Clone(深色调色板)** + **AI SDK(侧边栏布局)** 两个
example 为底座借鉴:仅取其观感与布局结构,数据层仍走 cocola 的
`ExternalStoreRuntime` 适配器(AI SDK 的 `useChatRuntime`/`streamText` 与本项目
不兼容,不引入)。官方 example 基于 Tailwind v4,本项目为 v3.4.4,已做转译
(`rounded-4xl`→`rounded-[2rem]`、`max-h-100`→`max-h-[25rem]`,避免
`has-data-*`/`w-(--var)` 简写,侧边栏令牌在 config 显式注册)。

## 复用面与边界
- **不动**:后端、网关 SSE 代理(`app/api/chat/route.ts`)、SSE 契约、
  `app/runtime-provider.tsx` 适配器逻辑(send/cancel 能力不变)。
- **复用**:assistant-ui 已导出的 `ThreadPrimitive`(含
  `Empty`/`Suggestion`/`ScrollToBottom`/`If`)、`ComposerPrimitive`、
  `MessagePrimitive`、`ActionBarPrimitive`;Grok example 的近黑深色调色板;AI SDK
  example 的侧边栏 + 主列布局结构。
- **自写**:侧边栏采用手搓轻量实现(纯 div + lucide 图标 + `useState` 折叠),
  **不引 Radix、不跑 `shadcn add sidebar`**,与项目"依赖从简、不引 Radix"的既有
  取舍一致。

## 改动(apps/web)
### 新增
- `components/assistant-ui/app-sidebar.tsx` —— 静态侧边栏 `AppSidebar`。
  `useState(collapsed)` 控制展开/收起(`w-64` / 收起 `w-[3.25rem]`);Header 为
  logo 圆标 + "cocola" + `PanelLeft` 折叠钮;分区 New Chat / Search / Notes /
  Workspace、Channels(# general)、Folders(💵 Finance / 📕 Study)、Chats
  (Today · "Roman Concrete Durability");Footer 为静态用户头像。
  **刻意不接后端**——运行时仅支持单线程(send/cancel),历史 / 文件夹 / 频道均为
  装饰性壳子,待多线程持久化落地后再接线。

### 修改
- `app/globals.css` —— 保留 light `:root`;`.dark` 重定向为 Open WebUI 近黑调色板
  (bg `0 0% 8%`、card/popover/input `0 0% 13%`、muted/secondary/accent/border
  `0 0% 16%`、ring `0 0% 45%`、foreground `0 0% 98%`);`:root` 与 `.dark` 均新增
  sidebar 令牌(`--sidebar`/`--sidebar-foreground`/`--sidebar-border`/
  `--sidebar-accent`/`--sidebar-accent-foreground`)。
- `tailwind.config.ts` —— `theme.extend.colors` 新增 `sidebar`(DEFAULT +
  foreground/border/accent/accent-foreground),映射到上述 CSS 变量。
- `app/layout.tsx` —— `<html className="dark">` 强制深色;body 由 `neutral-*`
  迁移到令牌 `bg-background text-foreground`。
- `app/page.tsx` —— 改为 Open WebUI 外壳:左侧 `<AppSidebar />` + 右主列
  (slim TopBar:模型药丸 + 沙箱状态 + 可折叠开发面板 Bearer token/session id)。
  主列用 flex row 包裹 Thread,**预留未来 Artifacts 画布槽位**,无需重构即可并排。
- `components/assistant-ui/thread.tsx` —— 欢迎区改造:空态居中 logo 圆标 +
  "gpt-4.1-nano" 大标题 + 药丸输入条 + "Suggested"(Zap 图标)三张
  title/subtitle 建议卡(`ThreadPrimitive.Suggestion` 点击即发送);对话开始后输入
  条 docked 到底部。**去除麦克风/语音图标**(运行时不支持,仅保留 Send/Stop)。

## 校验
- `corepack pnpm@10 --filter @cocola/web build` —— PASS(路由 `/` 146kB/233kB
  First Load、`/_not-found`、`/api/chat`、`/raw`)。
- `corepack pnpm@10 --filter @cocola/web lint` —— PASS(No ESLint warnings or errors)。
- `prettier --write` —— 全绿。

## 非目标
- 侧边栏历史 / 文件夹 / 频道不接后端(装饰性壳子,待多线程持久化)。
- Artifacts 画布本次仅预留布局槽位,不实现。
- 不引入 Radix / 官方 canary `aui-*` 样式(Tailwind v4 专属)。
- 沙箱内禁起监听端口,故未引入 `next dev` 预览;校验以 build/lint/prettier 为准。
