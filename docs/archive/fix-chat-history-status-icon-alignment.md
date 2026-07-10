# fix: Chat History 状态图标对齐

- 变更时间：2026-07-10 22:33 (+08:00)

## 变更理由

Chat History 列表中，回答完成对勾与三点操作图标使用 24px 容器居中，而回答中的 loading 图标直接使用 14px 宽度，导致三种状态的视觉中心不一致，完成和操作图标明显偏左。

## 变更内容

- `apps/web/components/assistant-ui/app-sidebar.tsx`：将完成状态和操作按钮统一到与 loading 图标相同的 14px 布局槽位。
- 三点操作按钮继续保留 24px 点击区域，仅以共同的视觉中心向外扩展，不缩小交互热区。
- 不修改 Chat History 的状态判断、菜单行为和视觉语言。
