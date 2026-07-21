# Project 与 GitHub Connector 运维手册

本文面向 Cocola 管理员，覆盖本地 Project、每用户 GitHub App、Token Broker 和 Runtime GitHub 能力。系统不部署 Forgejo或其他内部 Git 服务。

## 发布开关

三个功能可以独立关闭：

```dotenv
COCOLA_FEATURE_LOCAL_PROJECTS=true
COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR=true
COCOLA_FEATURE_GITHUB_AGENT_WRITE=true
```

- 关闭 `LOCAL_PROJECTS`：停止创建和运行本地 Project，不删除 Project 或 Session Volume。
- 关闭 `GITHUB_MANIFEST_CONNECTOR`：停止创建、刷新和使用 Connector；本地 Project 仍可工作。
- 关闭 `GITHUB_AGENT_WRITE`：停止签发新的 Broker Credential 和 installation token；不删除 GitHub 仓库。

修改后重启 Gateway。回滚只切换开关，不删除数据库记录或用户仓库。

## Connector 状态排查

1. 在用户的 `Connectors` 页面点击 Refresh，状态会在该次请求中重新检查，不依赖后台轮询。
2. `Installation required`：用户需要把自己的 Private GitHub App 安装到同一个个人账号，并为目标仓库授予访问权。
3. `Reauthorization required`：用户授权已过期或被撤销。Disconnect 后重新执行 Manifest Flow。
4. `Error`：通常表示保存的 App private key/client secret 无法解密或 GitHub App 已被删除。让用户重新创建 Connector。
5. 组织 installation 会被忽略；一期只接受 App owner 对应的个人账号。

Disconnect 会删除 Cocola 保存的用户 token 和 App 凭证，并尽力撤销活动 lease；不会删除 GitHub 上的 App、仓库或历史 Project。

## SCM 密钥恢复

`COCOLA_SCM_SECRET_KEY`（生产推荐 `_FILE`）必须是稳定的 32 字节密钥。它用于加密 App private key、client secret、用户 token 和短期 lease。

- 备份密钥文件时使用部署平台的 Secret 备份能力，不写入仓库或普通日志。
- 密钥丢失或被替换后，旧密文不可恢复；保留 Project 数据，让每位用户在 Connectors 中 Disconnect 并重新创建 App。
- 不要尝试直接修改密文字段或把 App private key 注入 Sandbox。

## Token Broker 与审批

- Broker Credential 绑定 user、Run、Project、Repository 和 installation；Run 不再是 `running` 后立即拒绝。
- 每条命令使用独立 `request_id`。高风险审批只允许该次精确命令获取一个 token；重新执行需要新的 `Approve once`。
- 审批五分钟过期。Gateway 使用事件唤醒和最多 25 秒的有界等待请求，不运行后台紧密轮询。
- 命令结束后 Runtime 上报 `success`/`failed` 并尽力撤销 token；Run 结束、Project 归档或 Connector 断开会再次清理活动 lease。

审计表 `scm_audit_events` 只包含用户、Project、Repository、Run、命令类别、权限、结果和耗时。排查时禁止输出以下字段：

- `scm_app_registrations.client_secret_ciphertext`
- `scm_app_registrations.private_key_ciphertext`
- `scm_connections.*token_ciphertext`
- `scm_token_leases.token_ciphertext`

## Empty Project 恢复

Empty Project 的唯一源码副本位于该 Conversation 的持久 Session Volume，固定工作在 `main`：

- Sandbox 进程回收：重新 Acquire 后挂载原 Session Volume，Git marker 校验通过即可继续。
- Session Volume 丢失：返回 `LOCAL_PROJECT_WORKSPACE_LOST`，禁止自动初始化，以免把数据丢失伪装成空项目。
- 已发布到 GitHub：GitHub 是远端备份，但当前本地 Project 仍保持唯一 Workspace；需要人工确认后再决定是否从远端恢复。
- 未发布：只能从底层存储备份恢复，不存在 Forgejo副本。

建议生产环境把 Session Volume 纳入存储平台备份，并在重要节点引导用户 commit 后发布到 GitHub。

## Runtime 发布验证

修改下列任一内容都会触发 `sandbox-runtime-image` 工作流：

- `deploy/sandbox-runtime/**`
- `scripts/sandbox-runtime-verify.sh`
- Runtime 镜像工作流自身

发布门禁检查固定版本 `gh`、认证 wrapper、`cocola-github` Skill 以及既有开发工具。生产部署使用工作流输出的不可变 digest；仅修改 Gateway 配置或用户 Connector 不需要重建 Runtime。
