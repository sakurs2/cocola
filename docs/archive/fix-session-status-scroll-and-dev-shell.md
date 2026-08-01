# fix: 修复 Session Status 滚动与开发环境 Shell 连接

- 变更时间：2026-08-01 12:54 (+08:00)

## 变更理由

对话页 Session Status 展开 Skills 后，桌面端侧栏只有最大高度而没有确定高度，
内部 `overflow-auto` 容器按内容高度展开，超出部分最终被侧栏的
`overflow-hidden` 裁掉，无法下滑查看完整列表。

本地热更新启动路径直接使用 `next dev`，绕过了负责 Workspace WebSocket
Upgrade 的自定义 `server.mjs`。终端创建请求可以成功返回 `201`，但浏览器端
WebSocket 无法进入 Gateway，页面因而一直停留在 Connecting 状态。

## 变更内容

- `apps/web/app/page.tsx`：让桌面端 Session Status 侧栏在聊天主布局中纵向拉伸，
  为面板内部滚动区域提供确定高度。
- `scripts/run-stack.sh`、`Makefile`：热更新开发栈统一通过 `server.mjs` 启动，
  同时保留 Next.js 开发模式与 HMR，并确保 Workspace WebSocket Upgrade 生效。
- `apps/web/components/assistant-ui/shell-page.tsx`：增加 15 秒 WebSocket 握手上限，
  代理异常时展示可重试错误，不再无限停留在 Connecting。
- `apps/web/lib/preview-ws-routing.test.mjs`、`apps/web/lib/session-status-ui.test.mjs`：
  增加开发启动入口、Session Status 滚动布局和终端握手超时的回归约束。

## 关键取舍

- 不改变 `environment_status` / Session Status 的事件、持久化或历史恢复协议。
- 不增加新的终端 API；只修正开发服务器入口，并为已有 WebSocket 状态机补充有界失败。
