# Plan: 前端会话生命周期 —— 修复"新问题续接了全部历史"

## 症状
web 对话里发一句新问题,agent 的回答像是综合了此前所有问题,如同把整段历史发了过去。

## 根因(已核实)
不是前端拼接历史。前端每轮只发当前 `{ prompt, session_id }`。真正机制是后端 Route A 的多轮续接:

- `apps/web/app/runtime-provider.tsx`:`const [sessionId, setSessionId] = useState("s1")` —— session_id 写死为常量 `"s1"`,页面生命周期内永不改变。侧边栏 "New Chat"(`app-sidebar.tsx`)是装饰按钮,未接线。
- `apps/agent-runtime/.../shim_provider.py`:`query()` 用 `session_id` 从 `session_map` 查出上一轮的 `claude_session_id`,带 `resume` 传给沙箱内 claude CLI,`--resume` 重开磁盘会话继续对话;轮末再把新 id 写回同一 `session_id`。

因此同一个 `"s1"` 让每一句新问题都续接在同一条越滚越长的会话上,而且没有任何"开新对话"的重置入口。多轮记忆本身是期望行为,缺的是会话边界。

次生风险:`"s1"` 是所有客户端共享的常量。一旦启用 Postgres 版 `session_map`(跨进程/多用户持久),不同用户会 resume 到彼此的 claude 会话 —— 跨用户上下文泄露。修复后每个浏览器会话拿到独立的随机 id,也顺带堵住这一点。

## 范围
纯前端。后端 resume / session_map 语义不动(它们是对的)。

## 改动

### 1. `apps/web/app/runtime-provider.tsx`
- session_id 初值改为**每个浏览器标签页启动时生成一个随机 id**,不再是共享常量 `"s1"`:
  `const [sessionId, setSessionId] = useState(genId)`(传函数给 useState,惰性初始化,只在首挂载生成一次)。`genId()` 已存在(crypto.randomUUID 回退)。
- 暴露一个 `newConversation()` 到 context:清空 `messages`、清空 `sandbox`、把 `sessionId` 重置为新的 `genId()`。这就是"开新对话"的重置边界 —— 新 session_id 在后端 session_map 里查不到 resume,claude CLI 从零开始,历史被干净切断。
  - 若正在流式(`isRunning`),先 `abortRef.current?.abort()` 再重置。
- context 类型 `CocolaContextValue` 增加 `newConversation: () => void`。

### 2. `apps/web/components/assistant-ui/app-sidebar.tsx`
- 让 "New Chat" 按钮真正接线:点击调用 `useCocola().newConversation()`。
- 其余装饰项(Search/Notes/Channels/Folders/历史列表)保持装饰,不在本次范围。
- 需要把 "New Chat" 从纯 `PRIMARY_NAV` 数据驱动的循环里拆出来单独渲染(它要 onClick),其余三项仍走循环。

### 3. `apps/web/app/page.tsx`
- 开发者面板里的 session id 输入框保留(手动指定 session 便于调试),但其 placeholder/说明不用改;它继续用 `setSessionId`。
- 无强制改动;确认 `useCocola()` 解构不受 context 扩展影响即可。

## 验证
- `pnpm --filter web build` 与 lint 全绿(仓库既有校验方式)。
- 逻辑核对:首次加载 sessionId 为随机 UUID;发两轮 → 后端应 resume(多轮记忆正常);点 "New Chat" → messages 清空、sandbox 清空、sessionId 变新值 → 再发问题时后端查不到 resume、从零开始(历史被切断)。
- 助手无法端到端起栈,交付时给出人工验收清单。

## 非目标
- 多 thread 持久化 / 历史列表接后端(侧边栏 Chats 仍装饰)。
- 后端 session_map / resume 逻辑改动。
