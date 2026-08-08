# Projects 与统一 Change Request

Cocola 的 Project 使用两类仓库 Provider，但对用户提供同一套任务与交付流程：

- Local Project：由隐藏的内部 Forgejo 托管私有仓库，创建时以 allow-empty commit 初始化 `main`。
- GitHub Project：仓库继续位于用户绑定的 GitHub 个人账号中。

每个 Task 都在独立 Session Volume 的 `/workspace/project` 中 Clone 权威仓库，并从锁定的
`base_sha` 创建 `cocola/task-<id>` 分支。Session Volume 是可重建的工作副本，不再承担
Project 级权威数据职责。同一个 Project 可以并行创建多个 Task，不使用共享主仓库或 Git
Worktree。

`/workspace/outputs`、`/workspace/uploads` 和 `/workspace/downloads` 是平台目录，不进入 Git
工作树。Sandbox Shell 以 root 运行，而仓库由 `cocola` 用户持有，因此 Runtime 的 system Git
config 只声明精确的 `safe.directory`，不使用 `*` 通配信任。

## Change Request 生命周期

Local 与 GitHub Project 均使用以下流程：

`Working → Change request open → Checks pending / Conflict → Squash merge → Merged`

用户先在 Task 的 Changes 页面检查并提交文件，再创建 Change Request。平台验证 workspace
marker、远端、任务分支和预期 head SHA，只推送当前 Task 的精确分支；存在未提交文件时拒绝
发布，不把未知生成物自动提交。重复创建、刷新或合并会对已有 Provider PR 做幂等协调。

合并固定为 squash merge 到默认分支。成功后远端任务分支被删除，Task 与对话转为只读；继续
开发需要从最新 `main` 创建新 Task。Local 用户不会看到 Forgejo 名称或链接，GitHub 用户可跳转
原始 PR。

## 每用户 GitHub App

管理员不注册或共享平台级 GitHub App。每位用户在 `Connectors` 中完成：

1. Cocola 通过 GitHub App Manifest Flow 创建该用户的 Private App。
2. 用户把 App 安装到同一个个人账号，并选择允许访问的仓库。
3. Cocola 校验 App owner、授权用户和 installation owner 一致。

不支持组织账号、GitHub Enterprise Server、GitLab，也不启用 webhook。页面打开、手动刷新和
操作前按需查询 GitHub 或 Forgejo 状态。

## 配置

```dotenv
# base64 编码的随机 32 字节稳定密钥；生产优先使用 _FILE。
COCOLA_SCM_SECRET_KEY_FILE=/run/secrets/scm-secret-key
COCOLA_PUBLIC_ORIGINS=https://cocola.example.com
COCOLA_PROJECT_MAX_REPOSITORY_MB=512

COCOLA_FEATURE_LOCAL_PROJECTS=true
COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR=true
COCOLA_FEATURE_GITHUB_AGENT_WRITE=true

# 内部 SCM。正式 Compose 会生成独立密码并使用同一 PostgreSQL 实例中的独立 database/user。
COCOLA_FORGEJO_API_URL=http://forgejo:3000
COCOLA_FORGEJO_CLONE_URL=http://host.docker.internal:3001
COCOLA_FORGEJO_USERNAME=cocola
COCOLA_FORGEJO_PASSWORD=<deployment-secret>
COCOLA_FORGEJO_DB_PASSWORD=<deployment-secret>

COCOLA_SANDBOX_PROJECT_BROKER_URL=http://gateway:8080
```

`COCOLA_SCM_SECRET_KEY` 加密每用户 GitHub App 凭据、Local Project 的仓库限定 Token 和短期
lease。Token 不写入 Git URL、Prompt、Workspace marker、Git config、日志或 Session Volume，
只通过一次性 AskPass 环境注入 Git 子进程。

## Runtime 与 Token Broker

平台负责 Task 分支发布、PR 创建和合并等确定性操作。GitHub Project 中模型主动使用 `gh` 或
Git 写操作时，仍通过 Token Broker 获取绑定 user、Run、Project、Repository 和命令的短期
installation token；默认分支、force push、删除与设置类操作继续要求 `Approve once`。

Local Project 的 Clone/发布使用每仓库限定的 Forgejo Token，不向模型环境开放 Forgejo 管理
能力。内部 SCM 不可用时只禁用 Local Project 创建、启动与交付，GitHub Project 保持可用。

部署、备份恢复、密钥轮换和排障步骤见
[`docs/runbooks/project-connectors.md`](./runbooks/project-connectors.md)。
