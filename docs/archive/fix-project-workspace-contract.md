# fix: 收敛 Project 工作区契约

- 变更时间：2026-07-23 02:01 (+08:00)

## 变更理由

Agent 在普通单项目的 Git 根目录 `/workspace/project` 下再次创建同名子目录，导致仓库根目录与应用根目录错位，影响部署检测、Preview 和后续 Agent 操作。同时 Workspace Shell 以 root 运行，而 Project 仓库属于 `cocola` 用户，Git 会以 dubious ownership 拒绝读取仓库。

## 变更内容

- `deploy/sandbox-runtime/skills/cocola-project-workspace/SKILL.md`：新增按需触发的内置 Project Workspace Skill，指导单项目脚手架直接使用现有 Git 根目录，并保留明确的 monorepo 例外。
- `deploy/sandbox-runtime/skills/manifest.json`：将 Project Workspace Skill 纳入版本化平台 Skill 集，由 Claude Code 与 Codex 共用现有 discovery 链路。
- `deploy/sandbox-runtime/Dockerfile`：system Git config 仅信任固定的 `/session/workspace/project`，不使用通配符，也不写入 Agent 用户配置。
- `apps/agent-runtime/tests/test_cocola_sandbox_cli.py`、`scripts/sandbox-runtime-verify.sh`：验证 Skill 内容、镜像内置资产、只读所有权和精确 safe.directory 配置。
- `docs/github-projects.md`：记录 Skill 与 root Shell Git 信任边界；不向每轮 Agent system prompt 注入 Project 目录规则。
