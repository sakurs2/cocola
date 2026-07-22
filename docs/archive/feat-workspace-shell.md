# feat: Workspace 增加交互式 Shell

- 变更时间：2026-07-22 22:48 (+08:00)

## 变更理由

用户需要在 Agent 对话页的 Workspace 侧栏中直接打开 Sandbox Shell，并使用 root 身份检查和操作当前会话环境。该能力还需要正确处理 Sandbox 准备、回收、浏览器切换与 WebSocket 断线，避免遗留无连接的 PTY。

## 变更内容

- `apps/gateway/internal/httpapi/terminal.go`：增加归属校验后的终端创建、查询、删除与 WebSocket 代理；Project Shell 通过 bootstrap marker 确认工作区就绪；使用一次性失联租约回收无连接 PTY。
- `apps/gateway/internal/httpapi/terminal_test.go`：覆盖普通会话、Project 就绪检查、可信请求头、WebSocket 代理、终端 ID 校验和失联回收。
- `apps/web/components/assistant-ui/shell-page.tsx`：增加 xterm Shell 页面、环境准备状态、有限退避重连、手动重试和 Sandbox 回收提示。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：在 Workspace Dock 中增加固定 Shell 页签。
- `apps/web/app/api/conversations/[id]/terminal`：增加同源终端 HTTP 与 WebSocket 路由。
- `apps/web/lib/terminal-protocol.mjs`、`apps/web/lib/preview-ws-routing.mjs`：增加终端帧协议及 WebSocket Upgrade 路由。
- `apps/web/package.json`、`pnpm-lock.yaml`：引入固定版本范围的 xterm 与 fit addon。
- 终端运行超过稳定窗口后才重置重连预算；Sandbox 准备计时按创建周期重置，不使用后台轮询或无限重试。
