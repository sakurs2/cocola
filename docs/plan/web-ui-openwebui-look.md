# Plan: web 聊天 UI 改造为 Open WebUI 观感(深色 + 静态侧边栏 + 居中欢迎区)

- 状态: Proposed(待评审,先过一遍再动手)
- 日期: 2026-07-01
- 关联: ADR-0007(gateway BFF + SSE 契约)/ADR-0009(Route-A 沙箱内 Claude Code)/ADR-0010(tool-use 透传)
- 关联 Plan: docs/plan/web-product-ui-assistant-ui.md
- 关联 changelog: docs/archive/feat-web-ui-polish-shadcn-tokens.md
- 决策人: @王佳辉

## 1. 目标(一句话)

把 cocola 的 web 聊天 UI 从浅色单栏改造成 **Open WebUI demo 观感**:深色主题 + 左侧
会话历史侧边栏(静态壳)+ 居中欢迎区(logo + 模型名)+ 药丸输入框 + Suggested 建议卡,
并为后期 Artifacts 右侧画布预留布局扩展位。**不动后端 / SSE 契约 / runtime 适配器。**

## 2. 需求来源与边界(用户确认)

- 用户提供 Open WebUI demo 截图为目标观感,明确"就想要这种样式"。
- 用户确认:侧边栏历史/文件夹**做成静态好看的壳即可**,不接后端;**去掉麦克风/语音图标**。
- 用户确认:同意用官方 example 当地基改,不从 0 手写。
- Artifacts 为后期能力,本次**只预留布局扩展位**,不实现。

## 3. 复用地基(官方 example 调研结论)

| 复用对象 | 来源 | 用法 |
| --- | --- | --- |
| 深色调色板 | assistant-ui docs `grok.tsx`(硬编码 hex:#141414/#212121/#2a2a2a…) | 转成 cocola 的 `.dark` 令牌值,和截图黑底对齐 |
| 侧边栏 + 会话历史布局 | `templates/default/app/assistant.tsx` + `@assistant-ui/ui` 的 threadlist-sidebar / thread-list | 照抄结构(header/content/footer + 分组列表),但**手写轻量版**,不引 Radix |
| Artifacts 右侧画布 | `examples/with-artifacts`(flex 并排 + tab 卡片 + iframe) | 仅参考,本次留扩展位 |

### 关键约束:官方 example 是 Tailwind v4,cocola 是 v3

照抄 className 时需转译:`rounded-4xl`→`rounded-[2rem]`、`max-h-100`→`max-h-[25rem]`、
`size-8`→`h-8 w-8`(v3.4 起 size-* 也支持,保留亦可)、`has-data-*`/`w-(--var)` 等 v4 简写
避免使用。侧边栏令牌(sidebar/sidebar-foreground/…)需在 globals.css + tailwind.config 注册。

### 侧边栏底座决策:手写轻量版,不引 Radix

放弃 `shadcn add sidebar`(会拉 collapsible/dialog/tooltip 等 6 个 Radix 包)。
本项目既定"不引 Radix、保持依赖轻量"(见 tooltip-icon-button 决策)。侧边栏是静态壳,
用 plain div + lucide 图标 + 一个 useState 折叠即可,v3 原生、零新依赖。

## 4. 改动清单(apps/web)

### 4.1 令牌层 —— 切默认深色 + 补 sidebar 令牌
- `app/layout.tsx`:`<html className="dark">`,body 改 `bg-background text-foreground`(去 neutral 硬编码)。
- `app/globals.css`:`.dark` 令牌改为 Open WebUI 式近黑(background ~ #141414、card/muted ~ #212121、
  border ~ #2a2a2a、input ~ #212121、ring 提亮);新增一组 `--sidebar-*` 令牌
  (sidebar / sidebar-foreground / sidebar-border / sidebar-accent / sidebar-accent-foreground)。
- `tailwind.config.ts`:`theme.extend.colors` 注册 `sidebar` 及其子色。

### 4.2 新增组件
- `components/assistant-ui/app-sidebar.tsx` —— 静态侧边栏:
  - Header:cocola logo 圆标 + 名称 + 折叠按钮(PanelLeft)。
  - 顶部动作:New Chat / Search / Notes / Workspace(lucide 图标 + 文案,static <button>)。
  - 分组:Channels(# general)、Folders(Finance/Study,带 emoji 色块)、Chats → Today(示例会话)。
  - Footer:用户区(头像圆标 + 名称,取自 useCocola 或静态占位)。
  - 折叠态:收成窄条(仅图标),useState 控制,不接后端。

### 4.3 修改
- `app/page.tsx`:改为左右分栏 `flex h-screen` —— 左 `<AppSidebar/>`,右主区(含顶栏 + `<Thread/>`)。
  顶栏放折叠触发 + 居中模型名 pill(如 "gpt-4.1-nano" 占位)+ sandbox 状态。SessionBar 的
  token/session 输入迁入一个可收起的小区(保留开发用途,视觉弱化)。**为 Artifacts 预留**:主区
  用 flex,后期可在 Thread 右侧并排画布。
- `components/assistant-ui/thread.tsx`:
  - 欢迎区改为 Open WebUI 式:居中 logo + 模型名大字 + 药丸输入框在**正中**(空态);Suggested
    建议卡改为"标题 + 副标题"两行结构(截图观感)。
  - Composer:**去掉麦克风/语音图标**,只保留 Send/Stop 圆按钮;药丸 `rounded-[2rem]`。
  - 全部走令牌色,深色下自动生效。

## 5. 非目标
- 不改后端 / 网关代理(app/api/chat/route.ts)/ SSE 契约 / runtime-provider 能力面(仍 send/cancel)。
- 侧边栏历史/文件夹/频道/模型选择器均为**静态**,不接后端(无多线程持久化)。
- 不实现 Artifacts,仅留布局扩展位。
- 不引入 Radix / shadcn sidebar 包;沙箱禁起监听端口,校验以 build/lint/prettier 为准。

## 6. 校验
- `corepack pnpm@10 --filter @cocola/web build` PASS。
- `corepack pnpm@10 --filter @cocola/web lint` PASS。
- `prettier --write`(路径相对 apps/web)全绿。

## 7. 交付
- changelog: docs/archive/feat-web-ui-openwebui-look.md。
- 单次提交(不加 --no-verify,不提交 .claude/),推送 master。
