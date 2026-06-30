# feat: web 产品级聊天 UI(复用 assistant-ui + ExternalStoreRuntime)

日期：2026-07-01 · 关联 ADR：0007(gateway BFF + SSE 契约)/0009(Route-A 沙箱内 Claude Code)/0010(tool-use 透传)· 关联 Plan：docs/plan/web-product-ui-assistant-ui.md

## 背景
`apps/web` 此前只有一个刻意做丑、零依赖的测试工具(token 框 + prompt 框 + 原始事件
日志),文件头自注"不是产品 UI"。后端已达 MVP(`POST /v1/chat` SSE → agent-runtime
gRPC → 每会话沙箱内 Claude Code),需要补齐产品级聊天前端。

调研后否决"整套开源应用"(LibreChat/LobeChat/Open WebUI/Vercel ai-chatbot —— 均
假设后端 = OpenAI 兼容模型 API,套用需把 cocola 伪装成 LLM API,增加集成成本且冲淡
Route-A 语义),选用 **assistant-ui**(MIT,React/Next/Tailwind/TS 栈完全对齐,
shadcn 风格)的 **`ExternalStoreRuntime`** —— 专为自定义后端/已有状态设计:给它
messages + 回调,它给 ChatGPT 级渲染(流式、自动滚动、Markdown、思考块、工具卡、
取消按钮)。

## 复用面与边界
- **不动**:Next.js 14 壳、`apps/web/app/api/chat/route.ts` 同源 SSE 代理、网关
  SSE 契约(`event: <kind>` + `data: <json>` 帧,`data` 为 `{kind, data: map<string,string>}`)。
- **引入**:`@assistant-ui/react` + `@assistant-ui/react-markdown`,配套
  `class-variance-authority` / `clsx` / `tailwind-merge` / `lucide-react` /
  `remark-gfm` / `tailwindcss-animate`(shadcn 同款依赖)。
- **自写**:仅一个薄适配器把 9 种 `AgentEvent`(text、thinking、tool_use、
  tool_result、result、system、sandbox、error、done)映射为 assistant-ui 的
  `ThreadMessageLike`,并对未知 kind 容错忽略。

## 改动(apps/web)
### 新增
- `lib/utils.ts` —— shadcn `cn()`(clsx + tailwind-merge)。
- `lib/sse.ts` —— SSE 解析单一真源:`type AgentEvent` + `parseFrames(buffer)`
  (按空行切帧、取 data 行、JSON.parse,返回 `{events, rest}`),适配器与
  raw 调试页共用。
- `app/runtime-provider.tsx` —— 核心适配器(`"use client"`)。本地消息模型
  (`UiPart` = text|reasoning|tool-call,`UiMessage`)、`SandboxInfo` 与
  `CocolaContext`/`useCocola`;纯函数 reducer `reducePart` 按 kind 累积 parts、
  `fillToolResult` 按 `tool_use_id` 配对结果;`convertMessage` 产出
  `ThreadMessageLike`(tool-call 仅透传 `argsText`,不解析 args 以避开
  `ReadonlyJSONObject` 类型约束);`CocolaRuntimeProvider` 用
  `useExternalStoreRuntime` 接 `onNew`(push user + 空 assistant、fetch `/api/chat`
  带 Bearer + session_id、读 `ReadableStream` 流式 apply)与 `onCancel`(abort)。
- `components/ui/button.tsx` —— shadcn button(cva variants)。
- `components/assistant-ui/markdown-text.tsx` —— `MarkdownTextPrimitive` +
  `remarkGfm` + prose 样式。
- `components/assistant-ui/thread.tsx` —— shadcn 风格 Thread:Welcome/Composer
  (Send↔Cancel 随 running 切换)/UserMessage 气泡/AssistantMessage
  (Parts → Text=Markdown、Reasoning 思考块、工具卡 ToolFallback 可折叠展示 argsText
  与 result)/Copy ActionBar。

### 修改
- `app/page.tsx` —— 替换为产品 UI:`<CocolaRuntimeProvider>` 包 `SessionBar`
  (品牌 + 沙箱状态 pill + Bearer/session_id 输入)+ `<Thread/>`,h-screen 纵向布局。
- 旧测试工具经 `git mv` 迁至 `app/(debug)/raw/page.tsx`,改为 import 共享
  `lib/sse`、删本地重复类型与 parseFrames。
- `tailwind.config.ts` —— content 增 `./lib/**`、plugins 增 `tailwindcss-animate`。
- `app/globals.css` —— `html, body { height: 100%; }` 支撑全高壳。
- `tsconfig.json` —— 补 `"baseUrl": "."`,使 `@/*` 在 apps/web 内解析(此前继承根
  tsconfig 的 baseUrl 致 `@/lib/sse` 解析到仓库根而 Module not found)。
- `package.json` / `pnpm-lock.yaml` —— 上述依赖。

## 校验
- `corepack pnpm@10 --filter @cocola/web build` —— PASS(路由 `/` 144kB/231kB
  First Load、`/_not-found`、`/api/chat`、`/raw`)。
- `corepack pnpm@10 --filter @cocola/web lint` —— PASS(No ESLint warnings or errors)。
- `prettier --check` —— 全绿(5 个文件经 `--write` 规范后复跑 lint+build 仍绿)。

## 非目标
- 不改后端、网关代理、SSE 契约。
- 不做 auth/DB/历史会话持久化/多会话切换(沿用手填 token + session_id)。
- 沙箱内禁起监听端口,故未引入 `next dev` 预览;校验以 build/lint/prettier 为准。
