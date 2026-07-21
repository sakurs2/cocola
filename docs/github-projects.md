# GitHub Projects 配置

Cocola Project 一期只支持 GitHub.com 个人账号。全部 GitHub 环境变量留空时，功能关闭且不影响普通 Chat；只要设置了任意一项，Gateway 就会要求配置完整。

## 注册 GitHub App

1. 在 GitHub 的 Developer settings 中创建 GitHub App。
2. Homepage URL 指向 Cocola Web 地址。
3. User authorization callback URL 设置为 `COCOLA_GITHUB_CALLBACK_URL`，例如 `https://cocola.example.com/projects/new`。浏览器先回到该页面，再由同一 Cocola 登录态调用 Gateway callback；不要将 Gateway 暴露为浏览器回调地址。
4. 开启 expiring user authorization tokens，以便 Cocola 使用 refresh token 续期。
5. Repository permissions 只配置：
   - Administration: Read and write（创建用户个人仓库）；
   - Contents: Read-only（列仓库、Clone 和只读 Git 工作区）。
6. Where can this GitHub App be installed 选择 Any account。Cocola 产品层只接受安装在授权用户个人账号下的 installation，组织 installation 会被忽略。
7. 生成 RSA private key，记录 App ID、App slug、Client ID 和 Client secret。

不需要 webhook URL，也不要启用 webhook 订阅。

## Gateway 配置

```dotenv
COCOLA_GITHUB_APP_ID=123456
COCOLA_GITHUB_APP_SLUG=cocola-example
COCOLA_GITHUB_CLIENT_ID=Iv1.example
COCOLA_GITHUB_CLIENT_SECRET_FILE=/run/secrets/github-client-secret
COCOLA_GITHUB_PRIVATE_KEY_FILE=/run/secrets/github-private-key.pem
COCOLA_GITHUB_CALLBACK_URL=https://cocola.example.com/projects/new
COCOLA_SCM_SECRET_KEY_FILE=/run/secrets/scm-secret-key
COCOLA_PROJECT_MAX_REPOSITORY_MB=512
```

`COCOLA_SCM_SECRET_KEY` 是用于 AES-256-GCM 加密用户 access/refresh token 的稳定密钥。生成方式：

```bash
openssl rand -base64 32
```

密钥轮换一期不提供在线重加密。丢失或更换该密钥后，已有用户必须重新连接 GitHub。生产环境优先使用 `_FILE` 变量。CLI 部署会把 `${COCOLA_HOME}/secrets` 只读挂载为 Gateway 的 `/run/secrets`；将文件放入该目录后，再把 `_FILE` 指向对应的 `/run/secrets/...` 路径。该目录不会挂载给 Agent Runtime 或 sandbox。

## GitHub OAuth 与安装

用户从 Profile 或 `/projects/new` 发起连接。OAuth state 绑定 tenant、用户、返回路径和十分钟有效期，并在 PostgreSQL 中一次性消费。授权完成后，如果个人账号尚未安装 App，页面会继续引导安装；组织安装不会让连接进入 Ready。

创建新仓库时 Cocola 默认选择 Private，可改为 Public，并使用 GitHub `auto_init` 创建 README 和首个默认分支提交。导入只展示当前个人 installation 已授权的仓库。

## 安全边界

- OAuth access/refresh token 只以 AES-GCM 密文保存在 PostgreSQL；API 与日志不返回 token。
- Clone 使用限定单仓库、`contents:read` 的短期 installation token。
- token 通过内部 gRPC metadata 和临时 `GIT_ASKPASS` 进入 bootstrap 进程，不写入 Clone URL、Prompt、Workspace marker 或远端配置。
- bootstrap 完成后 Agent 不持有 GitHub 凭证，因此私有仓库的主动 fetch/push 会失败。
- bootstrap 会在当前任务仓库的 `.git/config` 中写入提交作者身份，因此 Agent 可以创建本地 commit；不会修改 sandbox 的全局 Git 配置。
- 一期 Git UI 只有保存的 status 和显式触发的 status/diff，没有 Stage、Commit、Push 或 PR。

## 本地开发

把上述变量写入仓库根目录 `.env` 后重启 `make dev`。本地 callback 通常为：

```dotenv
COCOLA_GITHUB_CALLBACK_URL=http://localhost:3000/projects/new
```

GitHub 不会回调任意临时端口；callback URL 必须与 App 配置精确匹配。若全部 GitHub 变量为空，Profile 显示 Disabled，Projects 普通列表为空。
