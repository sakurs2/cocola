# fix: 固定 Agent Skill 卡片并增加分页

- 变更时间：2026-07-28 17:51 (+08:00)

## 变更理由

Agent 编辑页直接展示完整 Skill 描述，长内容会把卡片高度无限撑开；同时 Skill 数量增加后会生成很长的卡片列表，影响配置效率和页面可读性。

## 变更内容

- `apps/web/components/agents/agent-capabilities-editor.tsx`：将 Skill 卡片固定为统一高度，标题和描述分别限制为一行、两行，并保留悬停查看完整描述的能力。
- `apps/web/components/agents/agent-capabilities-editor.tsx`：Skill 列表改为每页 6 个的前后翻页，翻页不改变已经选择的 Skill。
- `apps/web/lib/agent-capabilities.test.mjs`：增加固定卡片、文本截断和分页行为的回归约束。
- 编辑器保留响应式两列/三列布局；宽屏下每页呈现两行三列。
