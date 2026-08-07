# test: 同步 Agent Skill 可用性断言

- 变更时间：2026-08-07 14:19 (+08:00)

## 变更理由

Web CI 的 Node 测试中有两条静态源码断言失败：不可用 Skill 标签已按原因显示为
`Admin disabled` 或 `Unavailable`，Skills 页面也已改为从可用目录中搜索，但旧断言仍匹配
固定的小写标签和全量 `skills.filter`。产品逻辑符合当前可用性设计，根因是测试没有随此前
Agent Skill 可用性改动同步。

## 变更内容

- `apps/web/lib/agent-capabilities.test.mjs`：验证管理员禁用与通用不可用两类标签，并将用户
  Skills 搜索断言更新为 `availableSkills.filter` 链路。
- 保持业务组件不变；目标测试、Web 全量测试、格式检查、lint 与生产构建均通过。
