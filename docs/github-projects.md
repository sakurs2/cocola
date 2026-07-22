# Projects 与 GitHub Connector

Cocola 的 Project 支持两类工作区：

- Empty Project：不依赖 GitHub 或内部 Git 服务，在一个持久化 Conversation Workspace 中直接使用本地 Git `main` 分支。每个 Project 只允许一个 Workspace。
- GitHub Project：由用户从自己的 GitHub 个人账号创建或导入仓库；每个 Task 使用独立的 `cocola/task-*` 分支。

两类 Project 的 Git 工作树都固定在 `/workspace/project`，Agent、Code Server、
Git Inspect 和默认 Preview cwd 使用该目录。项目名称和 GitHub 仓库名称属于元数据，
不参与本地目录命名。`/workspace/outputs`、`/workspace/uploads` 和
`/workspace/downloads` 是平台目录，不进入 Git 工作树。

Empty Project 可在 Workspace 中完成本地 commit。用户连接 GitHub 后，可从 Project 页面明确执行 `Publish to GitHub`，把干净且已提交的 `main` 推到新建的个人仓库。发布后的 Project 仍保持单 Workspace/`main` 模型，Agent 后续通过短期 Token Broker 使用 `gh` 或推送；默认分支写入需要逐次确认。

## 每用户 GitHub App

管理员不再注册或共享一个平台 GitHub App。每位用户在侧边栏 `Connectors` 中完成以下流程：

1. Cocola 使用 GitHub App Manifest Flow 创建仅属于该用户的 Private App。
2. 用户把 App 安装到自己的个人账号，并选择允许访问的仓库。
3. 用户授权 App；Cocola 校验 App owner、授权用户和 installation owner 是同一个个人账号。

不支持组织账号、GitHub Enterprise Server、GitLab，也不启用 webhook。

Manifest Flow 的回调 Origin 必须在 `COCOLA_PUBLIC_ORIGINS` 中。Manifest/OAuth state 绑定 Cocola 用户、回跳路径和过期时间，并在 PostgreSQL 中一次性消费。

## 配置

```dotenv
# base64 编码的随机 32 字节稳定密钥；生产优先使用 _FILE。
COCOLA_SCM_SECRET_KEY_FILE=/run/secrets/scm-secret-key
COCOLA_PUBLIC_ORIGINS=https://cocola.example.com
COCOLA_PROJECT_MAX_REPOSITORY_MB=512

COCOLA_FEATURE_LOCAL_PROJECTS=true
COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR=true
COCOLA_FEATURE_GITHUB_AGENT_WRITE=true

# Agent Runtime 到 Gateway 内部 Broker 的地址。
COCOLA_SANDBOX_PROJECT_BROKER_URL=http://gateway:8080
```

生成 SCM 密钥：

```bash
openssl rand -base64 32
```

密钥用于 AES-256-GCM 加密每用户 App 的 client secret、private key 和 OAuth token。密钥丢失或更换后，用户需要在 Connectors 中重新创建并授权 App。App 凭证、长期 token 和私钥不会进入 Agent Runtime 或 sandbox。

`make dev` 使用 `.env.example` 的本地默认值，并把 Broker 地址改为 sandbox 可达的宿主桥接地址。修改 `.env` 后重启服务即可生效；不需要重新构建 Runtime 镜像。只有 `gh`、GitHub Skill 或 Runtime manifest 变化才需要发布新的 Runtime 镜像。

## Runtime 与 Token Broker

Runtime 固定版本预装 `gh` 和 Cocola GitHub Skill，但禁止 `gh auth login` 和本地凭证持久化。只有已绑定 GitHub 仓库的 Project Run 才会收到 run-scoped Broker Credential。

Broker 在每条命令执行时签发限定到当前仓库和所需权限的短期 installation token：

- 仓库、PR、Issue、Actions 等读取自动执行。
- Task 分支 push、PR/Issue 普通写入使用最小权限。
- `main`/默认分支或 force push、merge、删除、设置、Secrets、通用写入型 `gh api` 必须由用户 `Approve once`。
- 命令结束后 best-effort revoke token；审批五分钟过期，Run 结束、Project 归档或 Connector 断开会撤销现存 lease。

Token 不写入 Git URL、Prompt、Workspace marker、Git credential helper 或 `gh` 配置。审计只保存用户、Project、Repository、命令类别、权限、结果和耗时，不保存 token、secret 或完整请求正文。

部署、故障恢复、密钥轮换、审批审计和 Runtime 发布步骤见
[`docs/runbooks/project-connectors.md`](./runbooks/project-connectors.md)。

## Empty Project 的边界

- 只允许一个 Conversation Workspace，直接工作在 `main`，没有 Task 分支或 Promote/Apply 流程。
- Session Volume 正常回收后仍保留文件；若底层持久卷丢失且尚未发布到 GitHub，本地内容无法从远端恢复。
- 发布前必须先创建 Workspace，并把所有改动 commit；Cocola 不会隐式提交文件。
- 第一次发布由用户在 Project 页面触发。仓库创建或推送中断时 Project 记录为 `pending`，修复 GitHub App 仓库访问后可安全重试同一仓库。
- 发布后 Agent 可以使用 `gh`；直接写 `main` 始终属于高风险操作并要求单次确认。
