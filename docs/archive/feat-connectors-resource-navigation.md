# feat: 收敛 Connectors 与资源导航体验

- 变更时间：2026-07-22 12:44 (+08:00)

## 变更理由

Connectors 的横幅式 GitHub 卡片信息冗余、留白过多；Projects 与 Folders 在侧边栏直接展开实例列表，也造成导航层级和间距不一致。与此同时，完成态消息依赖数字 `PartByIndex` 重组节点，在流式内容切换到折叠态时可能访问已经变化的索引并重复渲染或抛出越界错误。

## 变更内容

- `apps/web/app/connectors/page.tsx`：将 GitHub Connector 收敛为紧凑方形卡片，使用 GitHub 图标并保留当前状态对应的核心操作。
- `apps/web/components/assistant-ui/app-sidebar.tsx`、`rail.tsx`：将 Projects 与 Folders 改为对齐其他主导航的独立入口，移除侧边栏内嵌资源列表和多余间距。
- `apps/web/app/projects/page.tsx`、`apps/web/app/folders/page.tsx`、`apps/web/app/folders/new/page.tsx`：新增 Project、Folder 列表页及 Folder 创建页。
- `apps/web/app/projects/[id]/page.tsx`、`apps/web/app/folders/[id]/page.tsx`：补充返回资源列表的面包屑与删除后导航。
- `apps/web/components/assistant-ui/thread.tsx`、`apps/web/app/globals.css`：始终走原生 Parts 渲染路径，通过稳定 DOM 标记折叠过程节点，避免流式结束时的索引越界和重复输出。
