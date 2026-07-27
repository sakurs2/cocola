# docs: lark-cli 默认 Admin Skills 技术方案

- 变更时间：2026-07-27 18:12 (+08:00)

## 变更理由

cocola 计划让 Agent 通过 lark-cli 使用飞书能力，并要求官方 Skills 在系统启动后以
Admin 身份默认存在，而不是烘焙到 Sandbox。方案还需要明确在线飞书文档不走 Gateway
预下载、应用身份权限不足时如何反馈，以及如何避免把 App Secret 暴露给 Agent。

## 变更内容

- `docs/plan/lark-cli-default-admin-skills.md`：新增完整技术方案。
- 方案复用现有 Admin Skill Catalog 和 Sandbox Skill 摘要同步，设计启动时幂等注入默认
  Admin Skills 的通用机制。
- 方案使用与 Sandbox lark-cli 相同的 `v1.0.77` 官方 Skill 资产，并定义版本锁定、
  并发启动、管理员接管、测试和回滚规则。
- 方案复用现有飞书 Connector，以 TAT 作为第一版 bot 身份，通过单次 Agent Run 环境变量
  注入；App Secret 保持在 Gateway。
- 明确飞书在线链接由 Agent 直接调用 lark-cli，飞书聊天直接附件继续走现有附件链路。
- 明确第一版不接飞书 MCP、不实现 UAT/OAuth、不新增文档索引或专用产品 UI。
