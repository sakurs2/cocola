# refactor: 删除核心服务半可用模式与旧兼容层

- 变更时间：2026-07-12 23:27 (+08:00)

## 变更理由

核心服务仍允许在缺少鉴权、MinIO、checkpoint 或 Agent Runtime 必需依赖时启动，
导致服务表面健康、实际在大附件、会话恢复、Skill 同步或模型调用阶段才失败。同时，
仓库仍保留 pre-GA Scheduled Task、MCP URL、Provider 类型、审计表和旧部署工具兼容。
这些分支增加了中间态和维护成本，不符合 Cocola 简单、可靠、出错时明确失败的要求。

## 变更内容

- Gateway、Admin API、LLM Gateway：鉴权密钥改为启动必需项，删除匿名和 auth-off
  composition 分支；Gateway 始终为每个 Run 签发用户 token。
- Gateway、Admin API、Agent Runtime、Sandbox Manager：MinIO/checkpoint 配置改为必需，
  客户端或 bucket 健康检查失败时拒绝启动，不再降级到 inline-only、metadata-only 或
  checkpoint-disabled 模式。
- Agent Runtime：Admin catalog、Sandbox 地址、runtime image、LLM URL/model alias 和
  Postgres 改为显式必需依赖，删除空 catalog、provider default 和无 binder/executor 路径。
- Secret：显式配置 `_FILE` 后文件不可读即失败，不再回退可能过期的环境变量。
- Sandbox Manager：修复手写 kubeconfig parser 无法读取 k3d 标准
  `- context: ... / name: ...` 列表结构的问题，capacity guard 不再静默禁用。
- Admin/Web：runtime token 只接受持久化用户 email，tenant 始终从用户记录推导；删除
  id-only/tenant fallback、明文 bootstrap 密码日志和旧 `/admin/prompts` 重定向。
- `00032_remove_superseded_schema.sql`：删除已迁移的 audit/trace 旧表，标准化 Provider
  类型，移除 unsupported interval/cron task 与旧 MCP 记录，并增加 schedule kind 约束。
- Scheduled Task：删除 interval/cron parser、legacy UI 状态和
  `scheduler.min_interval_secs` 配置，只保留五种产品化日历频率。
- 部署与工具：统一要求 Docker Compose v2；调试用 `sandbox-cli`/`admin-mint` 不再进入
  正式镜像；删除已被自动测试覆盖的手工 revocation/quota E2E 脚本。

## 关键取舍

- 本地数据库实施前只读核验：legacy Scheduled Task 和旧 MCP 均为 0，现有数据不受
  影响。升级迁移会删除无法无损映射的 pre-GA interval/cron 与未完成加密迁移的 MCP。
- Warm Pool、Scheduler enabled、OTel 和 OpenSandbox topology 是运行/运维控制，不是
  灰度开关，继续保留。
- Memory/Fake 实现继续作为显式测试替身存在，但生产 composition root 不选择它们。
