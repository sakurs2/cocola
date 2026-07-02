# Changelog: 修复"新问题续接全部历史" —— 前端会话生命周期

## 问题
web 对话中发送一句新问题,agent 回答像综合了此前所有问题,如同把整段历史发了过去。

## 根因
不是前端拼接历史(前端每轮只发当前 `{ prompt, session_id }`)。是后端 Route A 多轮续接被"钉死"在同一会话:

- `apps/web/app/runtime-provider.tsx` 里 `sessionId` 写死为常量 `"s1"`,页面生命周期内永不变,侧边栏 "New Chat" 是未接线的装饰按钮。
- `shim_provider.py` 用该 `session_id` 从 `session_map` 查上一轮 `claude_session_id`,带 `--resume` 让沙箱内 claude CLI 重开磁盘会话继续对话。

同一个 `"s1"` 让每句新问题都续接在同一条越滚越长的会话上,且没有任何"开新对话"的重置入口。多轮记忆本身是期望行为,缺的是会话边界。

次生风险:`"s1"` 为所有客户端共享常量,一旦启用 Postgres 版 `session_map`,不同用户会 resume 彼此的 claude 会话(跨用户上下文泄露)。

## 改动(纯前端,后端 resume/session_map 语义不动)
- `apps/web/app/runtime-provider.tsx`
  - `sessionId` 初值由常量 `"s1"` 改为 `useState(genId)` 惰性初始化 —— 每个浏览器标签页一个随机 UUID。
  - context 新增 `newConversation()`:abort 在途流 → 清空 messages → 清空 sandbox → `setSessionId(genId())`。新 session_id 在后端查不到 resume,claude CLI 从零开始,历史被干净切断。
- `apps/web/components/assistant-ui/app-sidebar.tsx`
  - "New Chat" 按钮接线到 `useCocola().newConversation()`;从纯数据循环拆出单独渲染(需 onClick)。其余项仍装饰。
  - `SidebarButton` 增加可选 `onClick` prop。
- `apps/web/app/page.tsx`:无改动(开发者面板 session id 输入框保留)。

## 验证
- `pnpm --filter web` 下 `tsc --noEmit` / `next lint` / `next build` 全绿。
- 逻辑核对:首载 sessionId 为随机 UUID;同 thread 内多轮仍 resume(记忆正常);点 "New Chat" → messages/sandbox 清空、sessionId 轮换 → 下一问题后端从零开始。

## 非目标
多 thread 持久化 / 历史列表接后端(侧边栏 Chats 仍装饰);后端逻辑改动。
