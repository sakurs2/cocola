# feat: 按 workspace 目录打开 Code Server tab

- 变更时间：2026-07-20 11:01 (+08:00)

## 变更理由

Workspace Dock 原先把 Code Server 作为与 Files、Preview 同级的固定页面，只能从默认
`/workspace` 打开编辑器，无法从文件树表达“编辑这个目录”的上下文操作。用户需要在
Workspace Files 中直接选择目录，并为不同目录保留独立的编辑器 tab。

## 变更内容

- `apps/web/components/assistant-ui/workspace-panel.tsx`：移除固定 Code 页面，增加按目录
  创建、去重和关闭的动态 Code tab；在 Files 页头和目录行增加 Code Server 操作，并
  保留后台 Workbench 以避免 tab 切换丢失未保存状态。
- `apps/web/lib/code-editor-readiness.mjs`：集中生成带 `folder` 查询参数的 Code Server
  URL 和稳定 tab ID，并为环境准备探测增加四分钟等待上限。
- `apps/web/lib/code-editor-readiness.test.mjs`：覆盖根目录、嵌套及 Unicode 路径编码、
  tab ID 稳定性和等待预算。
- 不修改 Gateway、sandbox runtime 或环境配置；现有同源鉴权、Preview Proxy 与
  WebSocket 链路保持不变。
