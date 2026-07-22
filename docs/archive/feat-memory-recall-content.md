# feat: 消息节点展示实际召回记忆

- 变更时间：2026-07-22 14:27 (+08:00)

## 变更理由

Agent 消息中的 Memory Recall 节点原本只显示召回状态和数量，用户无法确认本轮回答实际使用了哪些长期记忆。需要让节点展示发送给 Agent 的原始 `memory_context`，并且保持实时消息、历史消息和只读页面行为一致。

## 变更内容

- `apps/gateway/internal/httpapi/simple_chat.go`：在 Memory Recall 终态事件中携带实际召回上下文原文。
- `apps/gateway/internal/convo/`：在 `memory-recall` Part 中持久化原始内容，继续通过同一位置替换 running/终态节点，避免消息索引变化。
- `apps/web/app/runtime-provider.tsx`：贯通实时 SSE、历史消息恢复和 assistant-ui data part 的 `content` 字段，并兼容没有该字段的旧消息。
- `apps/web/components/assistant-ui/rail.tsx`、`thread.tsx`、`conversation-readonly.tsx`：允许点击 `Used N memories` 展开或收起实际记忆文本；内容使用纯文本渲染，不进入最终回答复制。
- 测试覆盖 SSE 发布与持久化内容完全一致、Reducer 节点替换，以及既有消息折叠和复制行为。

## 验证

- Gateway：`go test ./internal/convo ./internal/memory ./internal/httpapi` 通过。
- Web：TypeScript、ESLint、生产构建通过。
- Agent Turn：12 项折叠与复制测试通过。
- `git diff --check` 通过。
