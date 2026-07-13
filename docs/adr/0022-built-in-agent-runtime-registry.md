# ADR-0022: 内置 Agent Runtime Registry 与不可变会话 Runtime

- Status: Accepted
- Date: 2026-07-13
- Deciders: @cocola-maintainers

## Context

Cocola 的首个 Agent Runtime 是 Claude Code，模型路由、原生 session 索引、Sandbox
Shim 和 Web 选择器都隐含了这一前提。产品需要同时支持 Codex，并允许未来继续增加
Runtime，但不希望把 Runtime 变成管理员动态配置，也不希望为每个 Runtime 建立独立
Sandbox 资源池。

Runtime 选择会改变原生会话格式和模型协议。若同一 conversation 中途切换 Runtime，
已有 `.claude` 或 `.codex` 状态无法安全转换，也容易让 `session_map` 指向错误的原生
session。Codex 的模型通道使用 OpenAI Responses wire protocol，不能复用现有的
Anthropic-normalized `/v1/messages`。

## Decision

1. Agent Runtime 是产品内置、进程启动时不可变的能力。Agent Runtime 服务维护唯一
   `RuntimeRegistry`，稳定 ID 为 `claude-code` 和 `codex`；Gateway 启动时通过
   `ListRuntimes` 获取并缓存目录，失败则拒绝启动。不增加 feature flag、灰度开关或
   管理员 Runtime 配置。
2. `conversations.runtime_id` 在首个 Run 的事务中确定，历史数据回填为
   `claude-code`，之后不可修改。请求显式选择不支持的 Runtime 时在任何写入前返回
   400；与已有 conversation 不一致时返回 409。
3. `session_map` 保存通用的 `runtime_session_id` 和 `runtime_id`，所有恢复、更新和
   删除同时校验用户、conversation 与 Runtime。`.claude` 和 `.codex` 都属于同一
   session checkpoint；冷启动和 Warm Pool claim 走相同恢复路径。
4. 继续使用一张通用 Sandbox 镜像，构建期固定安装 Claude Code 与 Codex SDK/CLI。
   通用 Shim 只负责分派和统一 NDJSON 终态；Adapter 负责各自 SDK 配置与事件映射。
   Codex 关闭嵌套 Sandbox 和交互审批，安全边界仍由 Cocola 的 OpenSandbox、非 root
   用户和网络策略提供。
5. 模型兼容性由协议表达，而不是由管理员填写 Runtime 字符串。Anthropic 和
   OpenAI-compatible providers 发布 `anthropic-messages`；独立的
   `openai_responses` provider 发布 `openai-responses`。Web 根据 Runtime 协议过滤
   模型；定时任务本轮固定使用 Claude Code 和 `anthropic-messages`。
6. LLM Gateway 新增透明 `/v1/responses` 通道和独立 `ResponsesProvider` 协议。
   它复用 Cocola Token 的认证、吊销、配额、alias 路由和 Ledger，但不把 Responses
   SSE 转换成内部 Chat 事件；只允许首个上游事件前重试。

## Alternatives Considered

- **使用 Codex app-server**：适合长期双向连接、审批交互和订阅管理，但 Cocola 的
  一个 Sandbox Exec 对应一个 Turn，不需要额外 JSON-RPC 生命周期。
- **每个 Runtime 一张 Sandbox 镜像和一套 Warm Pool**：隔离更直接，但会复制
  生命周期、checkpoint、容量和发布逻辑；两套 CLI 可以稳定共存于通用镜像。
- **沿用 `llm_model_routes.runtime`**：看似灵活，实际由管理员输入的字符串无法保证
  协议或 Adapter 存在，而且此前不参与执行路由。
- **允许对话中途切换 Runtime**：需要转换原生 session，或丢弃记忆后制造一个看似
  连续但实际断裂的对话，行为不可靠。

## Consequences

- 新 Runtime 通过增加 Registry entry、Adapter 和模型协议实现，不改变 conversation
  不可变约束和 Sandbox 生命周期。
- Claude Code 历史会话在迁移后继续恢复；Codex thread 状态随 MinIO checkpoint 在
  Sandbox 删除、Warm Pool claim 和服务重启后恢复。
- 管理员只管理模型 Provider、alias 和密钥，不承担 Runtime 组合正确性。
- 整栈必须在数据库迁移后统一升级；不支持新旧 Agent Runtime/Gateway 混部。
