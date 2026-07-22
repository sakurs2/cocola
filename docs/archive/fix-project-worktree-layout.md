# fix: 隔离 Project 工作树与平台目录

- 变更时间：2026-07-22 16:47 (+08:00)

## 变更理由

Project 之前直接以 `/workspace` 作为 Git 仓库和 Agent 工作目录，导致平台管理的 `outputs`、`uploads`、`downloads` 等目录与项目源代码混在同一仓库根目录中。这样不仅会污染 Git 状态，也会让 Workspace Files、Code Server 和 Preview 暴露非项目文件。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/project_git.py`：将 Project Git 工作树固定到 `/workspace/project`，新增旧根目录仓库的安全迁移，并保持平台目录位于 `/workspace`。
- `apps/agent-runtime/cocola_agent_runtime/server.py`、`agent_provider.py`、`shim_provider.py`：Project Agent、Claude Code 与 Codex 统一在 `/workspace/project` 工作；附件和产物继续写入平台绝对路径。
- `deploy/sandbox-runtime`：更新 Guest CLI、Shim、Runtime manifest、入口脚本和内置 Skill 的工作目录契约，Preview 默认跟随当前 Agent 工作树。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：Project 的 Workspace Files、Code Server 和默认 Preview 以 `/workspace/project` 为根目录，普通 Chat 仍使用 `/workspace`。
- `apps/agent-runtime/tests`、`apps/web/lib/code-editor-readiness.test.mjs`：覆盖新工作树、旧仓库迁移、平台目录隔离、Agent cwd 和 Code Server URL。
- `docs/adr/0024-versioned-sandbox-runtime-contract.md`、`docs/github-projects.md`：记录新的 Runtime 路径契约和 Project 本地工作树规则。

## 关键取舍

- Project 名称和 GitHub 仓库名称继续作为业务元数据，不映射为本地目录名；本地固定路径减少路径注入、重命名和恢复复杂度。
- 旧仓库迁移保留平台目录；遇到无法安全判定的 `/workspace/project` 冲突时明确失败，避免覆盖用户文件。
- Runtime manifest、Shim 和 Skill 已发生变化，发布后需要重建 Runtime 镜像并使用新 Sandbox 生效。
