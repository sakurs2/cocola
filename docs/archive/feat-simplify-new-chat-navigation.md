# feat: 精简用户侧新会话导航

- 变更时间：2026-07-11 12:19 (+08:00)

## 变更理由

用户侧边栏同时提供 Workspace 和 New Chat，两个入口都指向聊天工作区，增加了不必要的导航选择。此前从 Skills 或 MCP 页面点击 New Chat 只返回主页，并不会真正创建新会话，入口语义也不一致。

## 变更内容

- `apps/web/components/assistant-ui/app-sidebar.tsx`：删除重复的 Workspace 导航项及图标引用。
- New Chat 在主页、Skills 和 MCP 页面统一创建空会话；非主页场景同时返回 `/`。
- 折叠侧边栏中的 New Chat 改为直接创建会话，不再先展开 actions 区域。
- 移除分组容器的白色 active 背景，避免 New Chat / Schedule 与当前路由同时呈现为选中状态；仅实际导航行保留选中反馈。
- 保留 Chat History 作为进入已有会话的唯一入口，不调整历史数据加载逻辑。
