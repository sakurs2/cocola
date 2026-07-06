# feat: MCP management

- 变更时间：2026-07-06 22:42 (+08:00)

## 变更理由

管理员需要在控制面配置 MCP server，并允许用户在个人侧选择是否启用。由于 cocola 当前 Route A 已把 Claude Code brain 放进 sandbox 内运行，本次 MCP 能力应走 sandbox 内 Claude SDK 原生 `mcp_servers` 配置，而不是恢复旧的宿主侧 MCP 转发缝合层。

## 变更内容

- apps/admin-api：新增 MCP server 与用户偏好模型，支持 `stdio`、`http`、`sse`，并对 env/header 敏感值做加密存储和 masked 展示。
- apps/admin-api：HTTP/SSE URL 支持 `${VAR}` 模板；真实 URL query key 可放入加密的 `url_vars`，runtime effective 配置下发前再替换，避免普通 UI/DB 响应暴露 URL secret。
- db/migrations/00022_mcp_management.sql：新增 `mcp_servers` 与 `user_mcp_preferences` 表。
- db/migrations/00023_mcp_url_vars.sql：为已部署/已迁移环境补充 URL 变量密文字段与 masked hint 字段。
- apps/agent-runtime：新增 MCP catalog loader，按用户拉取 effective MCP 配置，并通过 shim request 注入 sandbox 内 Claude SDK。
- deploy/sandbox-runtime/shim/agent_shim.py：将请求里的 `mcp_servers` 传给 `ClaudeAgentOptions`。
- apps/web：新增管理员 MCP 管理页、用户 MCP 开关页及对应 API proxy；用户侧复用 workspace shell。

## 关键取舍

- v1 只允许管理员发布 MCP，用户只做开关；用户自定义 MCP 暂不引入。
- 普通 Web API 不代理 runtime-only effective endpoint，避免浏览器侧获得解密后的 env/header。
- `COCOLA_CONFIG_SECRET_KEY` 用于配置类 secret 加密；未配置时兼容回落到 `COCOLA_MODEL_SECRET_KEY`。
