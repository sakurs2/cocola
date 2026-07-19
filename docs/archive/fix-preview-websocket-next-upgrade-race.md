# fix: 避免 Next.js 提前关闭 Preview WebSocket

- 变更时间：2026-07-19 19:36 (+08:00)

## 变更理由

新对话的 sandbox 和 code-server 均已正常启动，但 VS Code Workbench 开始发送协议数据后连接断开，浏览器报告 WebSocket 1006。追踪 socket 关闭栈发现，Next.js custom server 在第一次 HTTP 请求后向同一 HTTP server 追加了自己的 `upgrade` listener；Cocola 接管 `/api/preview/...` 后，该 listener 仍把同一个 URL 匹配到 App Router 的 HTTP Preview route，并在异步鉴权完成前调用 `socket.end()`。

## 变更内容

- `apps/web/server.mjs`：Preview upgrade 被 Cocola 同步认领后立即暂停 socket，并在 Next listener 执行前隐藏原始 Preview URL；非 Preview upgrade 继续由 Next 自己处理，避免重复调用 Next upgrade handler。
- `apps/web/lib/preview-ws-routing.mjs`：使用一个不存在的 API 路径遮蔽已认领的 Preview upgrade，使 Next 忽略该连接，同时保留 Cocola 提前解析出的真实 Gateway 路径。
- `apps/web/lib/preview-ws-routing.test.mjs`：增加回归测试，确保遮蔽后的 URL 不再匹配 `/api/preview/`。

关键取舍：不放宽 `trusted-origins`，不修改 OpenSandbox；修复只隔离两个同服务器 upgrade listener 的所有权，现有鉴权、Origin 校验和 Gateway WebSocket 隧道保持不变。
