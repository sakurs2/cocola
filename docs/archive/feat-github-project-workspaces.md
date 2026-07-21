# feat: GitHub 项目与稳定用户身份工作区

- 变更时间：2026-07-21 19:21 (+08:00)

## 变更理由

Cocola 需要以独立 Project 领域承载 GitHub 仓库，并允许用户从零创建或导入个人仓库。每个 Project Task 应在独立、可恢复的持久 Workspace 中工作，同时向用户提供只读 Git 状态和 Diff。Git 提交身份必须来自 Cocola 用户资料，而不是 GitHub 登录名；因此资源归属也需要从可变邮箱切换到稳定的 `auth_users.id`。项目仍处于测试阶段，旧身份体系下的用户历史数据不做迁移，升级时直接清理。

## 变更内容

- `db/migrations/00041_projects.sql`：新增 SCM 连接、Project、Project Workspace 数据模型以及 Conversation 绑定约束。
- `apps/gateway/internal/project/`、`apps/gateway/internal/httpapi/projects.go`：实现 GitHub App 用户授权、个人仓库创建/导入、Project CRUD、幂等恢复、短期 Clone Token 和只读 Git 快照接口。
- `apps/agent-runtime/cocola_agent_runtime/project_git.py`：在 Session PVC 的 `/workspace` 中安全 Clone、锁定 base SHA、创建 Task 分支、配置 Cocola Git author，并提供受限 status/diff。
- `apps/sandbox-manager/`、Proto 与 Gateway Agent Client：支持 Project 额外 GitHub egress、Workspace reset 状态、Git inspect RPC 和运行终态快照。
- `apps/web/app/projects/`、Workspace Dock 与侧边栏：新增 Project 创建/导入、Task 页面、Git Tab、离线快照和显式 Inspect 交互。
- `apps/admin-api/`、`packages/go-common/token/`、`apps/web/app/profile/`：允许用户自助修改显示名称、用户名、邮箱和密码；使用乐观锁和当前密码校验；JWT subject 改为稳定用户 ID并携带签名资料 claims。
- `db/migrations/00042_stable_user_identity.sql`：增加账户版本，并直接删除旧邮箱身份下的用户级历史记录，不保留双身份兼容路径。
- `.env.example`、CLI Compose、开发脚本与 `docs/github-projects.md`：补充可选 GitHub App 配置、密钥文件和仓库大小限制说明。
- 测试覆盖 Project Git bootstrap/inspect、GitHub 客户端与加密、稳定 JWT、账户资料/密码、乐观锁、Git author 和 Web Task intent；修复非法邮箱与 bcrypt 超长密码边界。

## 验证

- Go：`packages/go-common`、`apps/admin-api`、`apps/gateway` 执行 `go build ./...` 通过。
- Go 聚焦测试：token、账户 store/service/httpapi、Gateway auth、Project Git author 通过。
- Web：TypeScript、lint 和生产构建通过。
- 通用：`git diff --check` 通过。
