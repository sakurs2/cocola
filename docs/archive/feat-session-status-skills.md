# feat: Session Status 展示已加载 Skills

- 变更时间：2026-07-11 02:06 (+08:00)

## 变更理由

Session Status 只展示 MCP 连接，用户无法确认当前 Agent 会话已经加载了哪些 Skill。随着可用 Skill 数量增加，直接把两类能力放在一个长列表中也会降低状态面板的可读性。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/server.py`：Skill 同步成功后生成不含内容与密钥的 `id/name/version` 环境元数据。
- `apps/agent-runtime/cocola_agent_runtime/shim_provider.py`：在转发 sandbox shim 的真实 MCP 快照时补入已加载 Skill，保持单一完整 `environment_status`，不修改 sandbox runtime。
- `apps/web/app/runtime-provider.tsx`：环境组件支持 `skill/loaded` 和可选版本。
- `apps/web/components/assistant-ui/session-status-panel.tsx`：状态面板按 Skills 与 MCP servers 分组，支持独立折叠；Skills 默认收起，MCP 默认展开。
- 更新相关 Python 测试和前端技术说明；不新增 Proto、数据库结构、依赖或 Agent prompt 内容。
