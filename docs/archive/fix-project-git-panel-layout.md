# fix: 收敛 Project Git 面板布局与确认交互

- 变更时间：2026-08-09 10:39 (+08:00)

## 变更理由

Project 任务的 Git 面板在较窄的 Workspace 宽度下信息层级混乱：Refresh 与主操作的尺寸和边界不一致，Change Request 卡片偏松散，Commit 的 refs、时间和 SHA 分散在右侧并互相挤压；Refresh 确认还错误使用了侧边 Drawer，遮挡范围过大。

## 变更内容

- `apps/web/components/assistant-ui/workspace-panel.tsx`：压缩 Git 快照和 Change Request 卡片；统一操作按钮高度、圆角和对齐；将 Commit 重排为固定图谱列与两行内容；把 Refresh 确认改为中央 HeroUI 确认框。
- `apps/web/components/ui/action-dialog.tsx`：允许非破坏性确认操作隐藏重复的通用提示区，使中央弹窗保持紧凑。
- `apps/web/lib/workspace-dock-tabs.test.mjs`：增加 Git 操作层级、中央确认和 Commit 两行布局的回归断言。
- 保留 Refresh 为描边次操作、Squash merge 为主操作，统一几何尺寸但不抹平操作优先级。
