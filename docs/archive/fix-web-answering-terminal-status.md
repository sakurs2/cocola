# fix: Web 对话完成后 Answering 状态悬挂

- 变更时间：2026-07-06 15:57 (+08:00)

## 变更理由

用户反馈最新一轮对话中 agent 看起来已经回答完成，但 UI 状态一直停留在 `answering`。
排查发现前端 runtime 只在 `/api/chat` fetch 流自然结束的 `finally` 中关闭 running 状态；
如果后端已经发送 `done` 事件但底层 HTTP/SSE 连接没有立即关闭，消息内容会显示完整，但
assistant-ui 的 `isRunning` 仍保持 true，侧边栏和线程底部就会继续显示 Answering。

## 变更内容

- apps/web/app/runtime-provider.tsx：新增终止事件判断，收到 `done` 或 `error` 事件后立即执行
  单次收尾逻辑，关闭 running、刷新会话列表并 cancel reader。
- apps/web/app/runtime-provider.tsx：保留原 `finally` 兜底，确保异常断流、取消请求等路径仍能复位。

