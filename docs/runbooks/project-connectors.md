# Project SCM 与 GitHub Connector 运维手册

本文面向 Cocola 管理员，覆盖隐藏的内部 Forgejo、每用户 GitHub App、统一 Change Request、
Token Broker，以及权威仓库的备份恢复。

## 服务与开关

```dotenv
COCOLA_FEATURE_LOCAL_PROJECTS=true
COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR=true
COCOLA_FEATURE_GITHUB_AGENT_WRITE=true
```

- 关闭 `LOCAL_PROJECTS`：停止创建、启动和交付 Local Project，不删除 Forgejo 仓库。
- 关闭 `GITHUB_MANIFEST_CONNECTOR`：停止创建、刷新和使用 GitHub Connector；Local Project 仍可工作。
- 关闭 `GITHUB_AGENT_WRITE`：停止模型侧 GitHub 写凭据；平台 Change Request 仍按安装权限工作。

Admin 的 Architecture 页面只展示 `Internal SCM` 健康、固定版本、PostgreSQL 与持久卷状态，
不提供 Forgejo 跳转入口。健康检查为 `GET /api/healthz`。Forgejo 只绑定宿主回环端口，关闭 SSH、
开放注册、Actions、Packages、Wiki 与 Issues 等非必要能力；版本必须显式升级，不使用 `latest`。

## Connector 状态排查

1. 在用户 `Connectors` 页面点击 Refresh；状态按需查询，不依赖 webhook 或后台轮询。
2. `Installation required`：用户需要把 Private GitHub App 安装到同一个个人账号并授权仓库。
3. `Reauthorization required`：用户授权已过期或被撤销；Disconnect 后重建 Connector。
4. `Error`：常见于保存的 App 凭据无法解密或 GitHub App 已删除。
5. 组织 installation 会被忽略；当前只接受 App owner 对应的个人账号。

Disconnect 会删除 Cocola 保存的用户 Token 和 App 凭据，并尽力撤销活动 lease；不会删除 GitHub
仓库、PR 或历史 Project。

## Internal SCM 排查

- `INTERNAL_SCM_PROVISION_FAILED`：检查 Forgejo API 健康、Provisioner 账号和 PostgreSQL。
- `INTERNAL_SCM_TOKEN_FAILED`：检查 Token scope 与 repository restriction；禁止改成共享全局 Token。
- `INTERNAL_SCM_INIT_*_FAILED`：按阶段检查 Gateway 镜像中的 Git、AskPass 和内部 Clone URL。
- `INTERNAL_SCM_PROTECTION_FAILED`：确认 `main` 存在，且保护规则禁止直接 push、允许平台 squash merge。
- Sandbox Clone 失败而 Gateway API 正常：检查 `COCOLA_FORGEJO_CLONE_URL` 是否能从 Sandbox 网络访问。
- Internal SCM 端口冲突：首次安装改用 `--internal-scm-port <port>`；`cocola start` 与
  `cocola doctor` 会校验占用者确实是当前 Forgejo 容器，不会把同名残留容器或其他进程当作可复用服务。

仓库名固定为 `p-<projectUUID>`，Project 改名不会改变远端。普通用户响应、日志和 Workspace marker
不得包含 Token 密文或内部管理 URL。

## SCM 密钥与 Token

`COCOLA_SCM_SECRET_KEY` 必须是稳定的 32 字节密钥，用于加密 GitHub App 凭据、Local Project
仓库 Token 和 lease。Forgejo Provisioner 密码与 Forgejo database 密码应使用部署 Secret，不写入
仓库或普通日志。

- SCM 密钥丢失后，GitHub 用户需重建 Connector；Local Project 仓库仍在，但平台无法解密其
  repository token，应在恢复原密钥后再开放 Local Project。
- Archive 会把 Local Project 权威仓库改为只读并撤销仓库 Token；任务历史保留，操作失败可按状态安全重试。
- Token 不进入 remote URL、Git config、环境快照、模型 Prompt 或 Session Volume。

## Change Request 排查

- `PROJECT_WORKSPACE_DIRTY`：让用户先显式 commit，平台不会自动提交。
- `checks_pending`：GitHub required checks 或 mergeability 尚未就绪；刷新后重查 Provider。
- `conflict`：不要 force push 或改写其他 Task；在当前 Task 解决冲突并提交，再点击 Update branch，
  然后刷新 Provider 状态。
- 重复点击创建或合并：应返回同一个 PR/合并结果，不应产生第二个 PR 或 squash commit。
- 合并成功：远端 Task 分支删除，Change Request 为 `merged`，对话输入框只读。

## 备份与恢复

Local Project 的权威数据由三部分组成，必须作为同一个恢复点保存：

1. Cocola PostgreSQL：Project、Task、Change Request 与加密 Token 引用。
2. Forgejo PostgreSQL database：仓库和 PR 元数据。
3. `cocola_forgejodata`：Git repository object、ref 与 Forgejo 持久文件。

CLI 在应用升级前生成 owner-only 的：

- `postgres.dump`
- `forgejo-postgres.dump`
- `forgejo-data.tar.gz`

为保证 Forgejo database 与仓库卷一致，CLI 会短暂停止 Gateway、Agent Runtime 和 Forgejo，完成
备份后按依赖顺序恢复原服务。备份失败也会尝试恢复已停止服务。

恢复演练应在隔离环境执行：停止写入入口，恢复同一目录中的两个 PostgreSQL dump 和 Forgejo
数据归档，再启动 Forgejo 与 Gateway。验证 `/api/healthz`、随机 Local 仓库的 `main` SHA、PR 状态
以及 Cocola `project_change_requests` 引用一致后才能开放流量。不得把不同时间点的 database dump
与数据归档混用；不一致时回退整套恢复点，而不是自动重建空仓库。

## Token Broker 与审计

Broker Credential 绑定 user、Run、Project、Repository 和 installation。每条命令使用独立
`request_id`，高风险审批只允许该精确命令获取一次 Token，五分钟过期。Runtime 上报结果后尽力
撤销；Run 结束、Project 归档或 Connector 断开再次清理活动 lease。

审计表只包含用户、Project、Repository、Run、命令类别、权限、结果和耗时。排查时禁止输出
App private key、client secret、OAuth token、repository token ciphertext 与 lease ciphertext。
