# feat: 折叠已完成的 Agent 执行步骤

- 变更时间：2026-07-20 15:37 (+08:00)

## 变更理由

Agent 回答可能包含较长的环境准备、推理、工具调用和中间说明。运行结束后继续完整展示这些过程会挤占最终回答的阅读空间，也无法直观看到本轮实际耗时。

## 变更内容

- `apps/web/lib/agent-turn-summary.mjs`：增加过程/最终输出划分、耗时格式化、旧消息时间回退和最终输出复制纯函数。
- `apps/web/components/assistant-ui/thread.tsx`：运行期间保持完整时间线，终态后默认折叠过程步骤，最终文字和文件保持可见，复制仅包含最终输出。
- `apps/web/components/assistant-ui/rail.tsx`：增加带完成图标、耗时和键盘展开能力的 Processed 摘要，并与回答时间线图标对齐。
- `apps/web/components/conversation-readonly.tsx`：只读分享页复用相同的步骤划分、耗时显示和复制规则。
- `apps/web/app/runtime-provider.tsx`：接收 `done.duration_ms`，并为旧历史消息使用相邻消息时间戳回退。
- `apps/gateway/internal/chatrun/`、`apps/gateway/internal/httpapi/simple_chat.go`：统一 Run 完成时间，在 assistant metadata 和 `done` SSE 中写入一致的耗时，并补充取消/中断终态说明。
- Web 与 Gateway 测试覆盖步骤划分、文件和终态说明可见性、耗时边界、历史回退、SSE 与持久化一致性。
- 关键取舍：不引入数据库迁移；运行期间不折叠；折叠状态仅保存在当前页面；文件卡片始终属于最终输出。
