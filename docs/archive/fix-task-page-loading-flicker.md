# fix: 消除 Tasks 页面快速加载闪烁

- 变更时间：2026-07-24 00:39 (+08:00)

## 变更理由

Tasks 页面每次进入都会以空状态重新请求 Task 和模型目录，并立即渲染灰色 Skeleton。
接口响应较快时，Skeleton 只出现一瞬间，形成明显闪烁；模型目录请求还会不必要地阻塞
Task 列表展示。

## 变更内容

- `apps/web/app/tasks/page.tsx`：按认证用户复用最近一次页面数据并在后台重新校验；
  Task 和模型目录分别加载，Task 返回后即可展示列表。
- `apps/web/app/tasks/page.tsx`：冷启动不再渲染卡片 Skeleton；快速请求期间保持稳定留白，
  超过短暂阈值后仅显示轻量加载指示。保存、暂停、恢复和删除后的刷新保留现有内容。
- `apps/web/lib/scheduled-task-page-cache.mjs`：新增用户隔离的内存快照缓存，不写入持久化
  浏览器存储，避免跨账号复用数据。
- `apps/web/lib/scheduled-task-page-cache.test.mjs`：覆盖账号隔离、数组快照、分资源更新和
  定向清理。
