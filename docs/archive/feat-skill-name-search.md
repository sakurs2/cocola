# feat: 为 Skill 列表增加名称搜索

- 变更时间：2026-07-28 18:00 (+08:00)

## 变更理由

Skill 数量增加后，用户需要在 Skill 页面和 Agent 配置页快速定位目标 Skill。现有页面只能浏览列表或翻页，缺少直接按名称查找的入口。

## 变更内容

- `apps/web/app/skills/page.tsx`：为共享 Skill 和个人 Skill 增加统一的名称搜索框及空结果状态。
- `apps/web/components/agents/agent-capabilities-editor.tsx`：为 Agent Skill 选择区增加名称搜索，并在搜索变化时重置到第一页。
- 两处搜索均仅进行大小写不敏感的 Skill 名称匹配，placeholder 统一为 `input skill name`。
- `apps/web/lib/agent-capabilities.test.mjs`：增加两处名称搜索和分页重置的回归约束。
