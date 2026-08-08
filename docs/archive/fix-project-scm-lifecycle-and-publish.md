# fix: 收敛 Project SCM 生命周期与交付链路

- 变更时间：2026-08-08 23:46 (+08:00)

## 变更理由

多任务 Project 第一版已经打通 Local Forgejo、GitHub 与 Change Request，但复查发现仍有几类长期风险：Internal SCM 的三个网络视角和宿主机端口使用隐式默认值，端口占用检查可能把不相关进程误认为 Cocola 服务；Project provisioning 与 archive 缺少可恢复、可并发保护的操作状态；GitHub 临时 Token 在对话和检查完成后没有立即撤销；Change Request 的“刷新”同时承担发布分支职责，存在额外 Git 调用、状态竞态和用户语义不清；任务列表逐条读取 Change Request 形成 N+1 查询。上一轮远端 CI 还暴露了 Go lint 问题。

## 变更内容

- `apps/cli/internal/config`、`apps/cli/internal/command`、`apps/cli/internal/compose`、`apps/cli/internal/doctor`：将 Internal SCM API、宿主机端口和 Sandbox Clone URL 建模为同一个配置对象；升级配置 schema；校验端口冲突、外部 Sandbox 可达地址以及被占端口的精确容器映射；Doctor 纳入 Forgejo、初始化任务和 host-agent。
- `apps/cli/internal/assets/compose.yaml`、`deploy/docker-compose/docker-compose.dev.yml`、`scripts/run-stack.sh`：移除 3001 的散落硬编码，统一由 `COCOLA_FORGEJO_HOST_PORT` 派生开发与安装配置。
- `db/migrations/00060_project_operation_lifecycle.sql`、`apps/gateway/internal/project`：为 provisioning/archive 引入 attempt ID、超时接管和 CAS 完成/失败；Local Project archive 先归档仓库、撤销仓库 Token 与租约，再提交数据库状态；失败可显式重试；Forgejo Token 清理支持分页与幂等；任务与 Change Request 改为单次联表查询。
- `apps/gateway/internal/httpapi`、`apps/gateway/internal/agent`、`apps/agent-runtime`：GitHub 安装 Token 在检查、Plan 校验、对话和发布结束后立即撤销；创建/更新 Change Request 使用 Sandbox 内一次原子快照与精确 SHA push；Refresh 只读取 Provider 状态；运行时错误保留稳定错误码。
- `apps/web`：用现有 HeroUI 紧凑卡片区分创建、更新分支、刷新状态、冲突、检查中和 Squash Merge；只为 GitHub 显示外部链接；Project 页面展示 archiving/archive_failed 并支持安全重试。
- `apps/admin-api/internal/service/architecture.go`：在现有 Internal SCM 服务节点上补充 Sandbox clone/push 数据流，不把 Forgejo 伪装成计算节点，也不增加普通用户入口。
- `docs/cli.md`、`docs/github-projects.md`、`docs/runbooks/project-connectors.md`：补充端口配置、外部 Sandbox 网络、发布/刷新语义以及 archive 恢复说明。
- 测试覆盖 Internal SCM 配置迁移和端口归属、Provision CAS、Forgejo Token 分页/归档幂等、Sandbox 原子发布、Admin 架构边；同时修复 CLI 无 TTY 测试依赖开发机默认安装目录的问题。

## 关键取舍

- Forgejo 仍是内部有状态服务，不作为 Sandbox/Node 容量节点；默认仅发布到宿主机回环地址，端口可配置且必须与其他 Cocola 端口唯一。
- 不引入后台同步 Worker。用户显式发布本地提交，Refresh 保持纯读；这降低第一版运维和状态一致性复杂度。
- 失败状态保留可恢复上下文，不用“尽力而为后直接标记成功”；过期 attempt 可被安全接管，活动 attempt 不允许并发覆盖。
