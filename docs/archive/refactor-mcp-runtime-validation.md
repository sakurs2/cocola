# refactor: MCP 配置改为首次 Agent 会话自然验证

- 变更时间：2026-07-10 19:33 (+08:00)

## 变更理由

Admin 保存 MCP 配置时为一次连接测试额外申请临时 sandbox，使简单的配置操作跨越 Web、Admin API、SandboxService、OpenSandbox、runtime shim 和镜像发布链路。该方案成本高、错误传播复杂，并且“保存时成功”也不能代表 MCP server 持续健康。

## 变更内容

- `apps/admin-api`：保存时仅执行字段校验、URL/Env/Header 加密和持久化；删除 MCPVerifier、临时 sandbox Acquire/Exec/Release 和专用错误映射，状态统一为 `configured`。
- `db/migrations/00026_mcp_configured_status.sql`：将历史 `verified` 状态迁移为 `configured`，避免 API 继续暗示保存时已完成连接验证。
- `apps/web/app/admin/mcps/page.tsx`：将 `Verified on save`、`Verifying…` 和工具计数改为 `Configured`、`Saving…`，明确连接在首次实际 Agent 会话中检查。
- `deploy/sandbox-runtime`：删除 `--mcp-check` 专用模式和显式 MCP Python SDK 依赖；保留正常 Agent 执行路径中的 MCP 错误脱敏与 `ExceptionGroup` 展开。
- `docs/frontend-tech-stack.md`：记录 Admin MCP 保存与运行时验证的职责边界。

## 关键取舍

- 不在 Admin API 宿主机执行任意 stdio 命令，避免扩大安全边界。
- 不新增常驻 verifier 服务，也不为保存操作申请 sandbox。
- MCP 是否真实可用由用户本来就会启动的 Agent sandbox 验证，结果与实际运行环境一致。
