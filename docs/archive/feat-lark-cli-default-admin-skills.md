# feat: 预装 lark-cli Admin Skills 并注入单轮飞书应用身份

- 变更时间：2026-07-27 19:17 (+08:00)

## 变更理由

cocola 需要让 Agent 直接通过 lark-cli 使用飞书 OpenAPI，并在新部署启动后默认具备官方
Skills；同时必须复用当前用户自己的飞书 Connector，避免把 App Secret、TAT 或在线文档
正文写入 Prompt、会话存储和 Sandbox 持久配置。

## 变更内容

- `apps/admin-api/internal/defaultskills/`：内嵌与 Sandbox `lark-cli v1.0.77` 锁定的
  27 个官方 Skills、manifest 和上游 MIT LICENSE。
- `apps/admin-api/internal/service/defaultskills.go`、`cmd/admin-api/main.go`：增加启动期
  默认 Admin Skill 幂等对账，保留管理员禁用状态和人工接管内容；资产或对象存储异常时
  启动失败，支持 `COCOLA_DEFAULT_SKILLS_ENABLED=false` 跳过自动对账。
- `scripts/update-lark-cli-skills.sh`：增加显式升级脚本，以官方 tag 生成确定性 ZIP、摘要、
  Skill ID 清单和 LICENSE。
- `apps/gateway/internal/channel/feishu/`：把附件下载器内的 TAT 获取抽为共享、有界
  `TenantTokenProvider`，实现 60 秒提前刷新、singleflight、最多 1024 条 LRU 缓存和
  Connector 变更失效。
- `apps/gateway/internal/agent/`、`internal/httpapi/simple_chat.go`：按已验证用户解析
  Connector；只用内部 gRPC metadata 传递本轮凭证，不修改 protobuf，失败不阻塞普通对话。
- `apps/agent-runtime/cocola_agent_runtime/`：Execute 模式只依据本轮 Connector 凭证
  是否 ready 且完整，把 TAT 映射到当前 Shim 进程的五个 `LARKSUITE_CLI_*` 环境变量，
  不再用 Skill ID 作为凭证开关；同时固定 bot 严格模式，增加与管理员 System Prompt、
  用户 AGENTS.md 共存的平台安全指令，并为受限 Sandbox 出口加入 Feishu/Lark 官方
  OpenAPI 域名。
- `.env.example`、`docs/configuration.md`、`docs/plan/lark-cli-default-admin-skills.md`：
  补充配置、运行边界、升级和回滚说明。
- Go 与 Python 单元测试覆盖默认 Skill 对账、版本/摘要/LICENSE 校验、TAT 缓存边界与并发、
  metadata 隔离、Plan 模式隔离、System Prompt 和单轮环境变量注入。
