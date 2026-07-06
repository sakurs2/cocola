# feat: conversation share readonly page

- 变更时间：2026-07-06 19:02 (+08:00)

## 变更理由

用户希望行为审计日志里的会话 ID 可以点击查看对应对话，同时主对话页可以复制分享链接。原有系统只支持在聊天工作台内查看会话，缺少无输入框的只读渲染页面，也没有从审计日志或当前会话复制链接的入口。

## 变更内容

- apps/web/app/conversations/[id]/page.tsx：新增只读会话页面入口，基于现有会话消息 API 渲染历史消息。
- apps/web/components/conversation-readonly.tsx：新增只读会话渲染组件，对齐主对话消息 UI，支持模型信息、复制按钮、reasoning/tool 折叠卡片、文件卡片和图片预览。
- apps/web/app/page.tsx：在主对话页右上角新增分享按钮，复制当前会话只读链接。
- apps/web/app/admin/audit/page.tsx：在审计日志中把 conversation id 渲染为可点击链接，并在 metadata 区域显式展示。
- apps/web/components/assistant-ui/thread.tsx：导出 ModelIcon，供只读会话页复用主对话模型图标渲染逻辑。
- 注意事项：只读链接复用现有会话消息接口的鉴权与所有权校验，不是匿名公开链接。
