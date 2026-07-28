# fix: Agent Runtime 接受 Lark Office 知识链接

- 变更时间：2026-07-28 19:00 (+08:00)

## 变更理由

Agent 配置页和 Gateway 已允许 `larkoffice.com` 飞书知识链接，但 Agent Runtime 的独立安全白名单缺少该域名。用户能够保存链接，却会在对话运行时收到 `INVALID_AGENT_CONTEXT`，导致知识无法读取。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/server.py`：将 `larkoffice.com` 加入远程飞书知识引用的域名白名单，与 Web 和 Gateway 保持一致。
- `apps/agent-runtime/tests/test_server.py`：增加 `bytedance.larkoffice.com/wiki/...` 的端到端 Runtime 上下文回归测试。
- 继续要求 HTTPS、无用户信息、无自定义端口、固定文档路径和合法资源 token，不放宽其他 URL 安全校验。
