# fix: 切换会话时保持活动 Run 状态

- 变更时间：2026-07-22 21:07 (+08:00)

## 变更理由

Agent 回答进行中切换到其他会话再返回时，前端会根据已加载的 Assistant 消息错误地将会话标记为非运行态。后台 SSE 和 Run 实际仍在继续，但页面提前显示 `Processed`、取消 Plan 吸顶，并按终态规则折叠正在增长的消息时间线。

根因是 `loadConversation` 在读取缓存或历史消息后执行 `setRunning(false)`，随后已有 Run cursor 的 `connectActiveRun` 按幂等保护直接返回，无法恢复刚被清除的运行状态。

## 变更内容

- `apps/web/app/runtime-provider.tsx`：移除根据 Assistant 消息内容推断 Run 终态的两处状态写入。
- Run 状态继续只由 `done` 事件、用户取消或明确的重连失败维护；切换会话不再改变后台 Run 生命周期。
- 清理不再使用的 `hasAssistantResponse` 辅助函数和 Hook 依赖。
- 通过消息时间线单元测试、TypeScript、Web lint 和生产构建。
