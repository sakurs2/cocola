# feat: 精简 Admin MCP 配置并在保存时验证

- 变更时间：2026-07-10 15:50 (+08:00)

## 变更理由

原 Admin MCP 页面同时暴露发布、启用、详情、URL Variables 等多层概念，管理员无法在不发起 Agent 对话的情况下确认配置是否真正可用。远程 URL 还可能携带 query token 或 userinfo，继续把 URL 模板或明文序列化到数据库和 Web API 会扩大 secret 暴露面。

## 变更内容

- `apps/admin-api/internal/service/mcp.go`、`mcp_verifier.go`：将完整远程 URL 写入保留的加密变量，公开接口只返回安全 `url_hint`；Create/Patch 在写库前通过临时 sandbox 执行 MCP initialize 和 `list_tools()`，验证失败不修改数据，并提供历史 URL 幂等迁移。
- `apps/admin-api/internal/httpapi/`、`cmd/admin-api/main.go`：接入验证错误响应、启动迁移和 SandboxService verifier；保持 Agent/Sandbox proto 与数据库结构不变。
- `deploy/sandbox-runtime/`：显式固定 MCP Python SDK，并为 shim 增加 `--mcp-check` 的 stdio、HTTP、SSE 检查模式及错误脱敏。
- `apps/web/app/admin/mcps/`：移除统计、详情和发布概念，改为紧凑卡片网格与 Admin Drawer；卡片直接提供 Edit、Enable/Disable、Delete，明确展示 URL 或 Command。保存时显示 `Verifying…`，严格校验 `KEY=value`，支持保留或清除已保存的 env/header。
- `apps/web/app/mcps/`：用户侧只展示 `url_hint`；旧 Admin MCP 详情 URL 重定向回列表。
- `apps/admin-api/internal/service/mcp_test.go`、`apps/agent-runtime/tests/test_agent_shim_mcp_check.py`：覆盖加密、失败不写库、迁移幂等、sandbox 释放、三种 transport 和 secret 脱敏。

## 关键取舍

- 不新增 Agent RPC、Sandbox RPC、数据库列、独立连接测试按钮或持续健康状态。
- `Verified on save` 只表示保存时完成握手并发现工具，不表示 MCP server 持续在线。
- 验证在真实 sandbox runtime、非 root 用户和现有网络策略中执行，验证结束始终释放临时 sandbox。
