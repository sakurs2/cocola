# feat: Workspace Dock 支持同类型多实例 Tab

- 变更时间：2026-08-01 13:13 (+08:00)

## 变更理由

Agent 对话页 Workspace Dock 以页面类型或 Code 目录作为固定 Tab ID，并在打开时按 ID
去重。用户因此只能同时打开一个 Files、Shell、Preview、Git 页面，同一个目录也只能打开
一个 Code 页面，无法并行保留多个独立浏览、终端或预览上下文。

## 变更内容

- `apps/web/components/assistant-ui/workspace-panel.tsx`：将基础页面视为模板，每次从空状态
  Launcher、`+` 菜单或目录 Code 操作打开时都创建独立实例；重复实例使用递增标题区分。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：Project 状态变化时按页面类型清理全部
  Git 实例，并按实例 ID 隔离页头操作与关闭状态。
- `apps/web/lib/workspace-dock-tabs.mjs`：集中生成带模板 ID 与单调序号的实例 ID 和显示名。
- `apps/web/lib/workspace-dock-tabs.test.mjs`：覆盖同类型实例隔离、重复标题和 UI 接入约束。

## 关键取舍

- Files、Shell、Preview、Git 和 Code 均允许多实例；每个实例保留自己的 React 状态。
- 不同 Artifact 原本已支持并行 Tab；重复点击同一个 Artifact 继续聚焦并更新原 Tab，避免同一
  下载资源产生多个等价预览。
- 不修改 Workspace、Terminal、Preview、Git 或 Code Server 的后端协议。
