# fix: 避免 Agent 最终输出在流式结束时重复渲染

- 变更时间：2026-07-21 14:04 (+08:00)

## 变更理由

带有推理或工具步骤的 Agent 回答在最终文本流式输出期间，会由完整时间线 renderer
展示；收到 `done` 后，页面又切换为折叠过程和最终输出两套 `PartByIndex` renderer。
最终文本因此在终态切换时被卸载并重新挂载，表现为回答突然折叠、重新流式显示，部分
情况下同一回答短暂或持续出现两份。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`：流式态和终态统一使用按索引渲染，最终
  输出从首次出现起固定在独立容器；终态只把过程节点替换为折叠摘要。
- `apps/web/lib/agent-turn-summary.mjs`：增加纯渲染计划，明确实时过程、摘要过程和最终
  输出索引，确保最终输出在状态切换前后保持同一条渲染路径。
- `apps/web/lib/agent-turn-summary.test.mjs`：覆盖工具后最终文本和文件在流式完成前后索引
  稳定、过程与输出互斥的回归场景。
- 关键取舍：不延迟 `Processed` 折叠，也不增加动画或定时器；通过稳定 React renderer
  身份消除重复显示。
