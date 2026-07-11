# fix: 对齐聊天模型图标与消息 Rail

- 变更时间：2026-07-11 19:53 (+08:00)

## 变更理由

Agent 回答 Header 使用普通 flex 布局，模型图标中心位于距左侧 10px；Environment、Reasoning、Answer 共用的 Rail 图标列宽为 28px，竖线中心位于 14px，导致模型图标视觉上向左偏移。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`：模型 Header 改用与 RailRow 相同的 28px 图标列和 10px 列间距，模型图标、Rail 图标与竖线共享同一水平轴线。
- 保持模型名称、消息正文缩进、Rail 节点布局和移动端行为不变。
