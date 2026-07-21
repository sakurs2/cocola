# feat: Project Connectors 与本地工作区

- 变更时间：2026-07-22 00:53 (+08:00)

## 变更理由

Project 不应因管理员未配置共享 GitHub App 而整体不可用；用户需要能直接创建 Empty Project，并在同一个持久工作区的默认分支上持续开发。同时，GitHub 接入应由每位用户自行配置，Agent 使用 GitHub 时只能获得短期、仓库级凭证。引入 Forgejo 会增加额外服务、数据库、备份和运维成本，因此本期明确不引入内部 Git 托管服务。

## 变更内容

- `apps/gateway/internal/project`、`apps/gateway/internal/chatrun`：将 Project 领域拆分为本地与 GitHub 两种来源；Empty Project 只绑定一个 Conversation Workspace，固定使用 `main`，并增加个人 GitHub App Manifest Connector、短期 Token Broker、命令审批和本地项目发布链路。
- `apps/agent-runtime/cocola_agent_runtime/project_git.py`、`packages/proto`：在持久 `/workspace` 内幂等初始化本地 Git 仓库，保留用户修改；支持受控发布到 GitHub，并通过 gRPC metadata/进程环境传递临时凭证。
- `apps/web/app/connectors`、`apps/web/app/projects`、`apps/web/components`：新增 Connectors 导航和 GitHub 配置向导，移除不可用的 Search、Notes 及 Profile 旧入口；Empty Project 始终可创建并继续同一个工作区。
- `deploy/sandbox-runtime`：固定版本预装 `gh` wrapper 与 Cocola GitHub Skill；禁止本地持久化登录，认证统一经 Gateway Broker 获取。
- `db/migrations/00043_project_connectors.sql`、`db/migrations/00044_scm_broker_run_lifecycle.sql`、`.env.example`、CLI Compose 与开发脚本：加入 Connector、审批、审计和可恢复的 Broker Run 生命周期数据结构及功能开关；未引入 Forgejo、内部 Git 服务、Promote API 或相关运维配置。
- `docs/github-projects.md`：记录本地 Project 的数据边界、GitHub Manifest 接入、发布流程和运行时安全模型。
- `docs/runbooks/project-connectors.md`：补充功能开关、Connector 恢复、SCM 密钥、审批审计、本地卷恢复和 Runtime 发布手册。

## 关键取舍

- Empty Project 的源代码只存在于该 Conversation 的持久 Session Volume；Sandbox 进程回收后可恢复，底层卷真实丢失时明确报错，不静默创建空仓库覆盖状态。
- 本地 Project 发布前要求工作区已提交且干净；远端创建、推送与数据库完成采用可重试状态，避免外部成功但本地状态丢失。
- 不引入 Forgejo，因此没有项目级内部 origin、Task 分支合并或 Promote/Apply 流程；需要跨工作区或远端备份时由用户显式发布到 GitHub。
- 高风险 GitHub 命令以每次调用的 `request_id` 绑定单次审批，命令完成后记录脱敏结果与耗时；过期审批会持久化为终态。
- Project Composer 等待新 Task 会话完成初始化后再挂载，避免路由切换时把输入发送到旧会话。
- GitHub 仓库创建重试会在确认仓库不存在后继续创建，并刷新 provisioning 尝试时间；Broker Run 改为数据库持久化并在 Run 终态撤销，Gateway 重启后仍可正确判断有效性。
- GitHub 功能关闭或 SCM 密钥缺失时统一返回 Disabled，并允许本地回收租约，避免 nil 加密器导致 Gateway panic。
