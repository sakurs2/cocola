# feat: 跨 Runtime 的单轮 Skill 选择

- 变更时间：2026-07-14 15:07 (+08:00)

## 变更理由

用户需要在 Workspace 和 Folder 的对话输入框中通过 `/` 搜索并显式选择 Skill，且同一套交互需要在 Claude Code 与 Codex Runtime 下保持一致。此前 Cocola 只把全部 Skill 描述拼入 system prompt，既消耗上下文，也没有结构化的单轮 Skill 选择、权限复核和历史展示能力。

## 变更内容

- `apps/admin-api`：新增只返回安全摘要的 `GET /me/skills/effective` 用户接口。
- `db/migrations/00038_skill_runtime_ids.sql`、`apps/admin-api`：将按用户隔离的内部 catalog
  `id` 与 Claude/Codex 可见的 `runtime_id` 分离；迁移历史 Personal Skill 及消息
  metadata，避免 `user-<hash>-` 前缀进入用户界面或模型上下文。Personal 与 Shared
  同名时由 Personal 明确覆盖。
- `apps/web`：在共享 Composer 中增加开头 `/` Skill 搜索、键盘选择、可移除标签和历史消息标签；选择列表按 Personal / Shared 分组，名称与简介单行排列，超长简介以省略号截断；选中标签作为首行前置 token，并通过动态首行缩进让后续文字恢复完整宽度，附件预览使用独立区域；发送结构化 `skill_id`，正文保持原始问题。
- `apps/gateway`、`packages/proto`：在任何写入前校验 `skill_id`，保存到用户消息 metadata，并透传到 Agent Runtime。
- `apps/agent-runtime`：在 Acquire Sandbox 前校验当前用户有效 Skill，并将完整有效集合按期望状态同步到 Runtime 原生目录；Catalog 或同步失败时明确终止 Run。
- `deploy/sandbox-runtime/shim`：Claude 将选择转换为 `/skill-id`，Codex 转换为 `$skill-id`；未选择时维持原 Prompt。
- 删除将全部 Skill 描述拼入 system prompt 的逻辑，改用 Claude Code 与 Codex 的原生发现机制；不新增数据库表、功能开关或管理员配置。
