# feat: 允许 Artifact HTML 执行外部资源

- 变更时间：2026-07-22 16:04 (+08:00)

## 变更理由

Artifact HTML 预览会删除脚本、外部资源地址和事件处理器，同时注入禁止网络与脚本的 CSP。Agent 生成的 React 页面依赖 unpkg 等 CDN 时因此无法加载并显示空白。

## 变更内容

- `apps/web/components/assistant-ui/file-preview.tsx`：将 HTML 原样交给 iframe，不再重写或清理 HTML。
- iframe 允许脚本、表单、同源、弹窗、模态框和下载，使外部 CDN 与交互脚本能够运行。
- 保留 iframe 边界和 `no-referrer` 策略；Workspace Preview 代理链路不受影响。
