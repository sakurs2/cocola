# feat: 支持 Agent 创建、校验与发布个人 Skill

- 变更时间：2026-07-26 23:21 (+08:00)

## 变更理由

用户希望 Agent 能在 Sandbox 中创建或下载 Skill，经过服务端权威校验后发布到当前用户的 Personal Skill Catalog，并在下一轮对话及 Web Skill Tab 中生效。同时，Sandbox 镜像需要内置完整的 `skill-creator` 和固定版本的飞书官方 CLI。

## 变更内容

- `deploy/sandbox-runtime/`：内置 `skill-creator@1.0.0` 与 `@larksuite/cli@1.0.77`，增加 Skill manifest、运行环境说明及 `cocola-sandbox skill validate/publish` 命令。
- `apps/gateway/`：增加 Run 级 HMAC Skill Credential、内部 scan/import Broker 接口及 Admin API 转发，限制为当前用户 Personal Skill。
- `apps/agent-runtime/`：仅向当前 Agent 子进程注入 Broker 地址和 Credential，并在 Preview、后台进程与日志环境中移除凭证。
- `apps/admin-api/`：复用现有个人 Skill 导入语义，确保发布结果默认启用并支持同 ID 更新。
- `.env.example`、CLI Compose、配置文档和本地启动脚本：增加默认关闭的 `COCOLA_SKILL_PUBLISH_ENABLED` 及 Broker 配置。
- 相关 Go、Python 测试：覆盖凭证校验、身份和 Run 状态约束、路径与归档安全、Broker 不可用及发布成功场景。

## 关键取舍

- 首版只支持 Personal Skill，不允许 Sandbox 指定共享范围或其他 owner。
- 发布凭证与运行中的 Run 绑定，默认 12 小时有效；Gateway 不持久化 ZIP，也不向 Sandbox 暴露 Admin Key 或用户 Web Token。
- 新发布 Skill 不在当前 Run 热加载，从下一轮 Agent Run 起通过既有 effective catalog 生效。
