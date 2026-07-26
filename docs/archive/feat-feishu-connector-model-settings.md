# feat: 飞书 Connector 支持选择 Agent 模型

- 变更时间：2026-07-26 19:36 (+08:00)

## 变更理由

飞书消息已经能进入 Cocola Agent 链路，但 Connector 没有携带模型配置，会回退到部署默认别名
`cocola-default`。当该别名不存在或当前用户无权访问时，机器人只能返回模型不可用错误。用户需要在
Connector 卡片内明确选择一个当前可用、且与默认 Agent Runtime 协议兼容的模型。

## 变更内容

- `db/migrations/00049_feishu_connector_model.sql`：为用户级飞书连接保存模型路由 ID 和别名，并约束二者成对存在。
- `apps/gateway/internal/channel/feishu`：持久化模型配置；每条新消息读取最新配置并传给现有聊天链路；未配置时直接回复可操作提示，不再调用无效默认模型。
- `apps/gateway/internal/httpapi`：增加用户鉴权的飞书设置更新接口。
- `apps/web/app/api/connectors/feishu`：代理飞书设置更新请求。
- `apps/web/components/connectors/feishu-connector-card.tsx`：在卡片右上角增加设置入口和模型选择弹窗，只展示与默认 Agent Runtime 协议兼容的模型。
- 相关 Go 与 Node 测试：覆盖模型参数透传、数据库持久化、未配置拦截，以及设置弹窗与代理路由。
- 关键取舍：继续使用显式保存，不增加自动保存；配置在下一条新消息生效，无需重建飞书长连接。
