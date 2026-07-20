# feat: 将 Agent 产物预览接入侧边栏 tab

- 变更时间：2026-07-20 14:27 (+08:00)

## 变更理由

Agent 回复中生成、供用户下载的文件原先使用独立的右侧 Artifact 面板预览，无法与
Workspace Files、Preview 和 Code Server 共用侧边栏 tab。用户希望点击文件预览后，
每个产物都作为侧边栏中的独立 tab 打开和切换。

## 变更内容

- `apps/web/app/page.tsx`：移除独立 Artifact aside；选择产物时自动打开统一的 Workspace
  Dock，并保留状态面板返回最近产物的入口。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：增加动态 Artifact tab，支持按产物
  去重、激活、关闭、下载以及 HTML 预览/源码切换；后台文件预览卸载以控制浏览器资源。
- `apps/web/lib/artifact-preview-tab.mjs`：使用会话 ID 与 Artifact ID 生成稳定且隔离的 tab
  ID，避免不同会话或不同产物发生冲突。
- `apps/web/lib/artifact-preview-tab.test.mjs`：覆盖稳定性、跨会话隔离、Unicode 编码和非法
  标识校验。
- 不修改 Artifact 下载 API、Agent runtime 或 Workspace Files 内部文件预览行为。
