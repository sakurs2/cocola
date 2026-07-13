# feat: 支持 Claude Code 与 Codex 双 Agent Runtime

- 变更时间：2026-07-13 15:25 (+08:00)

## 变更理由

Cocola 需要让用户在新对话中选择 Claude Code 或 Codex，并为后续增加 Agent Runtime
保留清晰扩展点。Runtime 会决定原生 session 格式和模型 wire protocol，因此必须在
对话首次运行时确定并保持不可变，同时继续复用现有 Sandbox、Warm Pool 和 MinIO
checkpoint 生命周期。

## 变更内容

- `packages/proto`、`apps/gateway`：增加 Runtime 目录 RPC/API、请求 Runtime、对话
  不可变校验和 `progress` 事件持久化；Gateway 启动时校验 Agent Runtime 目录。
- `db/migrations/00034_multi_agent_runtimes.sql`：为 conversation/session map 增加
  Runtime 字段，通用化原生 session ID，并删除未参与路由的模型 Runtime 字段。
- `apps/agent-runtime`、`deploy/sandbox-runtime`：增加内置 Runtime Registry、Codex
  Adapter、固定版本 SDK/CLI、统一 session-not-found 语义和取消时子进程组清理。
- `apps/sandbox-manager`：将 `.codex` 纳入 PVC/host 挂载、权限初始化和 MinIO
  checkpoint；Warm Pool 注入 Codex 所需的同一 LLM Gateway 地址。
- `apps/llm-gateway`、`apps/admin-api`：增加独立 Responses Provider 和透明
  `/v1/responses`，按 Provider 类型发布模型协议，移除管理员 Runtime 输入。
- `apps/web`：新对话增加 Runtime Selector，首条消息后锁定，并只展示协议兼容模型；
  定时任务仍只展示 Claude Code 兼容模型。
- 收尾审查：修复并发创建同一 conversation 的唯一键竞争、历史对话 Runtime 加载竞态、
  Responses 空流首帧重试和完成事件后的 usage 记账边界；Codex 非致命错误正文不进入日志。
- 代码审查修复：Skills 按 Runtime 同步到 `.claude`/`.codex`，Codex 在
  `thread.started` 时立即持久化原生 session ID；Responses 统一使用共享限流、解压后
  转发 SSE、保留安全的上游状态类别，并记录 incomplete usage 与 conversation trace；
  Web 对 Runtime 冲突回滚乐观消息且不污染新对话偏好，Shim 对未知 Runtime fail-closed。
- Codex 会话状态面板展示已配置的 MCP，并在收到结构化 MCP tool call 后将对应连接更新为
  `Connected`；不解析 stderr，也不额外启动探测进程，避免产生不可靠的连接结论。
- 验证：Agent Runtime、LLM Gateway、Gateway、Admin API、Sandbox Manager、CLI 和数据库
  测试通过；Web production build、Ruff、TypeScript/ESLint、Codex SDK/CLI 版本解析和
  Sandbox Dockerfile 静态构建检查通过；真实 Codex SDK 对本地 Responses SSE 的烟测
  验证了 provider 路径、请求头和 `start → text → result → done` 事件序列。
- 关键取舍：单一通用 Sandbox 镜像；Runtime 为内置能力，不增加灰度开关、动态配置、
  新服务或多资源池。
