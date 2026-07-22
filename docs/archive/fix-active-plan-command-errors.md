# fix: 修复执行中计划吸顶与命令错误提示

- 变更时间：2026-07-22 20:00 (+08:00)

## 变更理由

执行中的 Plan 原先渲染在单条 Assistant 消息内部，受到消息动画和布局层级影响，无法相对对话滚动视口稳定吸顶；同时，命令返回非零状态会被统一标记为 `Tool call failed`，容易被误解为 Sandbox 或工具调用链路故障。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`：将运行中的最新 Plan 提升为对话滚动视口的直接子节点，并保持原始进度节点挂载以确保 assistant-ui 索引稳定。
- `apps/web/components/assistant-ui/rail.tsx`：增加吸顶 Plan 卡片；精确区分 Runtime 协议中的命令工具与普通工具，并允许展开查看原始失败输出。
- `apps/web/app/globals.css`：运行期间隐藏时间线内重复的 Plan 节点。
- `apps/web/lib/progress-items.mjs`：增加从实时和历史消息中读取最新 Plan 快照的纯函数。
- `apps/web/lib/tool-failure.mjs`：仅按 Runtime 明确定义的 `Bash`、`command_execution` 工具名分类，不解析或猜测自然语言错误文本。
- 补充 Plan 快照与命令工具分类测试；通过 Web 单元测试、TypeScript、lint 和生产构建。
