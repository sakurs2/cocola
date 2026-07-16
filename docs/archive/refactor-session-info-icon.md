# refactor: 优化 Session Info 入口图标

- 变更时间：2026-07-17 00:49 (+08:00)

## 变更理由

Session Info 按钮原先直接使用环境阶段图标，ready 时显示勾选符号，容易被理解为不可点击的成功状态，而不是查看当前会话能力详情的入口。

## 变更内容

- `apps/web/components/assistant-ui/session-status-panel.tsx`：按钮主体改为固定的 Info 图标，明确“查看信息”的交互语义。
- 保留现有状态圆点表达 preparing、ready 和 degraded，不改变按钮尺寸、Tooltip、键盘焦点或面板内容。
