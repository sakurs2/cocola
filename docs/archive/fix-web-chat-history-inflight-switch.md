# fix: Web 会话列表即时更新与后台流式回答

- 变更时间：2026-07-02 23:53 (+08:00)
- 关联提交：待提交后补充

## 变更理由

Web 端存在两个会话体验问题：

1. 用户发起 chat 后，侧边栏会话列表要等 agent 回复结束、`refreshConversations()` 执行后才新增会话；用户发送后缺少即时反馈。
2. 在一个会话中 agent 正在流式回答时，切换到其他会话再切回，当前回答不可见。根因是原实现用单一全局 `messages` 数组和单一 `AbortController`，`loadConversation` 会 abort 在途流并整体替换消息数组，导致后台流无法继续写回原会话。

## 变更内容

- `apps/web/app/runtime-provider.tsx`：
  - 将消息、运行态、AbortController 从全局单实例改为按 `session_id` / conversation id 存储。
  - 用户发送消息时立即乐观插入/置顶侧边栏会话，流结束后继续用服务端列表刷新对账。
  - 切换会话时不再 abort 其他会话的在途 SSE 流；后台回答继续写入所属会话 buffer。
  - sandbox banner 改为按会话保存，避免后台会话的 sandbox 事件覆盖当前会话展示。

## 验证

- 用户已手动验证两个问题修复通过。
- 本次按用户要求未额外启动服务或运行测试。
