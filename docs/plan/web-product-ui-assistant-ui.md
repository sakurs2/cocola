# Plan: web 产品级聊天 UI(复用 assistant-ui)

> 状态:规划中(2026-06-30)。本文先评审、后动代码(遵循"非平凡改动先写 Plan")。
> 关联:ADR-0007(gateway BFF + agent-runtime gRPC,SSE 契约)、ADR-0009
> (Route-A:每会话沙箱内跑完整 Claude Code)、ADR-0010(tool-use 透传);
> 现有资产 `apps/web`(Next.js 14 测试工具 + `/api/chat` 同源 SSE 代理)。
> 标准约束:尽量复用开源、避免造轮子;沙箱内禁止起监听端口(影响本地预览方式)。

## 1. 背景与动机

后端已达 MVP:`POST /v1/chat`(SSE)→ agent-runtime(gRPC)→ 每用户沙箱内
Claude Code,端到端 chat 已验收。前端目前只有 `apps/web/app/page.tsx` ——
一个**刻意做丑、零依赖**的测试工具(token 框 + prompt 框 + 原始事件日志),
文件头自注"不是产品 UI,真 UI 是后续里程碑"。现在补这块产品级 UI。

agent 对话前端开源项目极多,经调研分两类:

- **整套应用**(LibreChat / LobeChat / Open WebUI / Vercel ai-chatbot):都假设
  "后端 = 一个 OpenAI 兼容的模型 API",自带 auth / DB / 模型管理。cocola 的后端
  是"网关 BFF + 自定义 `AgentEvent`(含 `sandbox`/`thinking`/`tool_use` 等语义)
  + 每会话沙箱",套整套应用要写兼容 shim 把 cocola **伪装成 LLM API**,是增加
  集成成本而非节省,还冲淡 Route-A 的 agent-in-sandbox 语义。**否决**。
- **UI 组件库**(assistant-ui / Vercel AI Elements / CopilotKit):代码进自己仓库,
  只换"皮 + 交互"。其中 **assistant-ui**(MIT,React/Next/Tailwind/TS 栈完全对齐,
  shadcn 风格)提供 **`ExternalStoreRuntime`** —— 专为"自定义后端 / 已有状态"设计:
  你给它 messages + 回调,它给你 ChatGPT 级渲染(流式、自动滚动、Markdown、思考块、
  工具卡、分支/编辑/重试/取消按钮按回调能力自动点亮)。

结论:**保留现有 Next.js 壳 + `/api/chat` SSE 代理不动,引入 assistant-ui 作为
UI 层,用 `ExternalStoreRuntime` 写一个薄适配器消费现有 SSE 帧。** 复用最大化、
美观零成本、语义匹配、License 干净(MIT,适合企业自托管)。

## 2. 范围与非目标

**做**:
- `apps/web` 引入 `@assistant-ui/react`,落 shadcn 风格 `Thread` 组件。
- 写 `ExternalStoreRuntime` 适配器:`onNew` → 现有 `/api/chat` SSE,复用
  `parseFrames`,把 9 类 `AgentEvent` 映射为 assistant-ui 消息;流式 in-place 更新;
  `onCancel` 走 `AbortController`。
- `page.tsx` 换成产品级聊天页;保留 Bearer token / session_id 输入外壳(本地态)。
- Tailwind 主题变量 + globals.css 接 assistant-ui 设计 token。

**不做**:
- 不动后端任何代码(网关 / agent-runtime / 沙箱),SSE 契约零改动。
- 不动 `/api/chat/route.ts` 的代理逻辑(已验证,保留)。
- 不引入整套开源应用、不引入 redux/zustand(单线程会话用 `useState` 足够)。
- 不做多会话列表 / 持久化 / 登录页(后续里程碑;本次只产品化单会话主路径)。
- 不在沙箱内起 `next dev`(禁止监听端口),验证靠 `next build` + 类型 + lint。

## 3. 后端事件契约(适配器输入,来自代码核对)

网关 SSE 帧:`event: <kind>` 换行 `data: <json>` 加空行,`data` 为
`{kind, data:{...}}`,其 `data` 是 `map<string,string>`(agent-runtime 用
`_stringify` 把结构化值 JSON 编码)。agent-runtime 实际发出的 kind
(`claude_sdk_provider.py` + `server.py`):

| kind | data 关键字段 | UI 处理 |
|---|---|---|
| `text` | `text` | 追加到当前 assistant 文本气泡 |
| `thinking` | `thinking` | 渲染为推理块(折叠/弱化样式) |
| `tool_use` | `id,name,input`(input 为 JSON 串) | 工具调用卡:名称 + 入参 |
| `tool_result` | `tool_use_id,content,is_error` | 工具结果卡,按 tool_use_id 配对 |
| `result` | `is_error,num_turns,total_cost_usd,session_id,result` | 终态元信息(本次轻量:可仅记日志/隐藏) |
| `system` | `subtype,data` | 系统信息(轻量:弱化/隐藏) |
| `sandbox` | `sandbox_id,endpoint,reused` | 会话级状态条:已绑定沙箱(reused?) |
| `error` | `error` | 错误气泡(红) |
| `done` | (空) | 标记本轮结束,`isRunning=false` |

> 设计原则(呼应 proto 注释):**消费者必须容忍未知 kind**。适配器对未列出的
> kind 走兜底(降级为可见的中性信息),不得崩流。

## 4. 适配器映射设计

核心:`useExternalStoreRuntime({ messages, isRunning, onNew, onCancel, convertMessage })`。

- **本地状态**:`messages: UiMessage[]`(自定义结构,保留 cocola 语义:文本、
  思考、工具调用/结果、沙箱状态),`convertMessage` 转成 `ThreadMessageLike`
  (`content` parts:`text` / `reasoning` / `tool-call`)。
- **`onNew`**:push 一条 user 消息 → push 一条空 assistant 消息 → `setIsRunning(true)`
  → POST `/api/chat`(带 Bearer + session_id) → 逐帧消费:
  - `text`:assistant 末块文本累加;
  - `thinking`:累加到 reasoning part;
  - `tool_use`:新增 tool-call part(parse input JSON);
  - `tool_result`:按 `tool_use_id` 回填对应 tool-call 的 result;
  - `sandbox`:更新会话级 banner(不进消息流,或作为一条 system note);
  - `error`:assistant 追加错误 part(红);
  - `done` / 流结束:`setIsRunning(false)`。
- **`onCancel`**:`abortRef.current?.abort()`,复用现有 page.tsx 的 abort 模式。
- **流式 in-place 更新**:严格不可变更新(`map` 出新数组),只改 `id===assistantId`
  那条,符合 assistant-ui best practice。
- **handler 矩阵**:本次只接 `onNew` + `onCancel`(取消按钮)。`onEdit/onReload/
  setMessages`(编辑/重试/分支)留待后续里程碑,不在范围。

## 5. 文件清单

**新增**:
- `apps/web/app/runtime-provider.tsx`:`CocolaRuntimeProvider`,`ExternalStoreRuntime`
  + SSE 适配器 + token/session 上下文。
- `apps/web/lib/sse.ts`:从 page.tsx 抽出的 `parseFrames` + `AgentEvent` 类型
  (适配器与旧测试页共用,单一真相)。
- `apps/web/components/assistant-ui/thread.tsx`(及其依赖的小组件):assistant-ui
  CLI/官方模板生成的 shadcn 风格 Thread。
- `apps/web/components/ui/*`:Thread 依赖的 shadcn 基础件(button 等,按需)。
- `apps/web/lib/utils.ts`:`cn()`(clsx + tailwind-merge),shadcn 约定。

**修改**:
- `apps/web/package.json`:加 `@assistant-ui/react`、`@assistant-ui/react-markdown`
  (如 Thread 模板需要)、`tailwindcss-animate`、`clsx`、`tailwind-merge`、
  `lucide-react`、`class-variance-authority`(shadcn 常规依赖,按实际模板裁剪)。
- `apps/web/app/page.tsx`:换成 `<CocolaRuntimeProvider><Thread/></...>` + 顶部
  token/session 外壳。旧测试视图迁到 `app/(debug)/raw/page.tsx` 路由组(保留作 debug)。
- `apps/web/app/layout.tsx`:保留;如需主题 class 在此加。
- `apps/web/app/globals.css`:加 assistant-ui / shadcn 主题 CSS 变量。
- `apps/web/tailwind.config.ts`:加 `tailwindcss-animate` 插件 + content 覆盖
  `./components/**`、`./lib/**`;接主题变量。

**不动**:`apps/web/app/api/chat/route.ts`(代理)、`next.config.mjs`、根 workspace。

## 6. 校验与提交

1. `pnpm install`(workspace 根)成功,lockfile 更新入提交。
2. `pnpm --filter @cocola/web build` 通过(类型 + 编译)。
3. `pnpm --filter @cocola/web lint` 通过;prettier 干净(按仓库现有格式化流程)。
4. 因沙箱禁起监听端口:**不**跑 `next dev`;UI 行为正确性靠适配器纯函数单测
   /类型 + 真机由用户本地预览。
5. 写 `docs/archive/` changelog(动机 / 复用面 / 新增改动文件 / 校验)。
6. 单次提交(不用 `--no-verify`,不提交 `.claude/`),push。

## 7. 风险与回滚

- **风险:assistant-ui 版本 API 漂移**(`ai@6` 系列 + `@assistant-ui/react` 最新)。
  **缓解**:pin 具体版本;Thread 组件用官方当前模板生成,不手搓内部 API。
- **风险:Thread 模板默认走 AI SDK runtime**。**缓解**:只用 `ExternalStoreRuntime`
  接我们自己的 store,Thread 仅作纯展示组件,不引 AI SDK transport。
- **风险:结构化 data 是 JSON 串需二次 parse**(tool_use.input 等)。**缓解**:
  适配器 try/parse,失败则按原始串显示,不崩流(容忍未知/异常)。
- **回滚**:纯前端增量,`git revert` 单 commit 即可;旧测试页保留在路由组,
  随时可退回验证后端链路。
