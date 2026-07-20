# refactor: Session Status 使用 Activity 图标

- 变更时间：2026-07-20 11:11 (+08:00)

## 变更理由

Session Status 入口原先使用 Info 图标，更容易被理解为静态说明。用户从候选方案中选择
Activity，希望入口更直接地表达当前 Agent 会话和 sandbox 的运行状态。

## 变更内容

- `apps/web/components/assistant-ui/session-status-panel.tsx`：将 Session Status 按钮主体
  从 Info 替换为 Activity 图标。
- 保留现有状态圆点、按钮尺寸、Tooltip、键盘焦点和状态面板内容。
