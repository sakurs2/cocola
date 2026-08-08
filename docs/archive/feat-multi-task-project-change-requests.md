# feat: 统一多任务 Project 与 Change Request 链路

- 变更时间：2026-08-08 17:58 (+08:00)

## 变更理由

Local Project 原先把首个任务工作区当作项目数据源，只能创建一个任务，也无法与 GitHub Project 共用发布、审查和合并流程。任务 Volume 一旦损坏，项目级代码也会受影响；让模型直接执行任意 push/merge 还会扩大凭据泄露和误操作风险。

本次将 Local Project 的权威仓库迁移到隐藏的内部 Forgejo，并让 Local/GitHub Project 都采用“每任务独立 Clone + 平台确定性 Change Request”流程。历史 Local Project 无需兼容，迁移时直接清理；已有 GitHub Project 保留。

## 变更内容

- `deploy/docker-compose/docker-compose.dev.yml`、`apps/cli/internal/assets/compose.yaml`、`scripts/run-stack.sh`：增加固定版本 Forgejo、独立 PostgreSQL database/user、持久卷、健康检查和幂等管理员初始化；关闭注册、SSH、Actions 等非必要能力。
- `db/migrations/00059_project_change_requests.sql`：删除历史 Local Project/任务和单任务约束，扩展项目仓库凭据字段，新增任务级 Change Request 记录及状态约束。
- `apps/gateway/internal/project/`：增加 Forgejo/GitHub `RepositoryProvider` 适配器；Local Project 创建私有仓库、allow-empty `main`、仓库限定 Token 和分支保护；任务锁定 `base_sha` 后在独立 Volume Clone，并提供幂等创建、刷新、Squash Merge 和分支清理。
- `apps/gateway/internal/httpapi/`、`apps/gateway/internal/chatrun/`：新增任务 Change Request API；发布及合并前校验任务、分支、预期 head SHA 和活动运行；合并后的任务转为只读，终态不会被并发刷新回退。
- `apps/agent-runtime/cocola_agent_runtime/project_git.py`：收敛平台 Git 发布能力，清理继承的 `GIT_*` 环境，仅允许精确任务分支，并通过一次性 AskPass 注入凭据，避免 Token 写入 remote、Git config、marker 或日志。
- `apps/web/`：移除旧 Publish 接口，在 Changes 区域增加紧凑的 HeroUI Change Request 卡片；展示检查、冲突和合并状态；合并后禁用输入框，GitHub 项目保留外部 PR 链接，Local Project 不暴露 Forgejo。
- `apps/admin-api/internal/service/architecture.go`：Admin Architecture 只展示 Internal SCM 的健康、版本、数据库和存储状态，不提供内部服务入口。
- `apps/cli/internal/`：升级备份同时覆盖 Cocola PostgreSQL、Forgejo PostgreSQL 和 Forgejo 数据卷；Doctor 校验内部 SCM 存储卷。
- `docs/adr/0028-hidden-forgejo-project-change-requests.md`、`docs/github-projects.md`、`docs/runbooks/project-connectors.md`、`docs/cli.md`：记录架构取舍、统一用户流程、故障边界、备份恢复和运维方法。

## 关键取舍

- 不使用 Git Worktree：独立 Clone 会多占用部分磁盘，但更符合跨节点 Session Volume 的隔离方式，清理、锁和故障边界更简单。
- 第一版不增加 Webhook/常驻同步 Worker：页面打开、手动刷新及操作前按需查询 Provider，减少部署和一致性维护成本。
- Push、创建 Change Request 与 Merge 由平台执行；模型仍负责代码和提交，但不获得可自由操作其他仓库或分支的长期凭据。
- Local SCM 不可用时只影响 Local Project 的创建、启动与交付，GitHub Project 和普通对话保持可用。

## 验证

- Go：Gateway、Admin API、CLI、数据库和生成 Proto 的相关测试通过。
- Python：Agent Runtime 项目 Git 与服务测试共 90 项通过，Ruff 检查通过。
- Web：Next lint 与 TypeScript `tsc --noEmit` 通过。
- 配置：Buf lint、开发/正式 Compose 配置解析和 `git diff --check` 通过。
- 实机：Local Project 成功创建私有仓库和仓库限定 Token；`main` 存在初始提交且受保护；Admin 显示健康的 Internal SCM；Cocola 与 Forgejo 健康检查通过。
