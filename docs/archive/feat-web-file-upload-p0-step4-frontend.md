# feat: 聊天附件上传 P0 前端(Base64 内联 adapter + 回形针 UI + onNew 组装)

日期：2026-07-01 · 关联 Plan：docs/plan/web-file-upload.md(Step 4/6)· 将新增 ADR-0017

## 背景
P0 附件上传采用「后端预置(push)」模型:前端把文件读成 base64 随 chat 请求带下去,
gateway 解码为字节透传 gRPC,agent-runtime 在 agent 运行前把文件写进会话沙箱
`/workspace/<session_id>/uploads/`。Step 1-3(proto/gateway/agent-runtime)已完成,
本步补齐前端:复用 assistant-ui 的 attachment adapter/primitive 体系,不自造上传组件。

## 改动(apps/web)
### 新增
- `lib/base64-attachment-adapter.ts` —— `Base64AttachmentAdapter implements AttachmentAdapter`。
  为何自写:内置 `SimpleTextAttachmentAdapter` 会把文本包进 `<attachment>` 信封、
  `SimpleImageAttachmentAdapter` 产出 `data:` URL,二者异构且不适合按路径落盘。
  本 adapter 让文本/代码/图片统一产出**单个 `FileMessagePart`,`data` 携带裸 base64**,
  便于 onNew 还原为 `{filename, content_b64, mime}` 上线。
  - `accept`:文本/代码/常见图片白名单(text/*、image/* 及 .md/.py/.ts/.go/... 后缀)。
  - `add()`:1 MB 内联上限,超限抛错;返回 `status:{type:"requires-action",reason:"composer-send"}`
    的 PendingAttachment,`type` 按 mime 前缀判 image/file。
  - `send()`:`file.arrayBuffer()` → Uint8Array → 分块(0x8000)`fromCharCode` → `btoa`,
    产出 `content:[{type:"file",filename,data,mimeType}]` 的 CompleteAttachment。
  - `remove()`:no-op(内联附件无服务端资源)。

### 修改
- `app/runtime-provider.tsx`
  - 引入 `Base64AttachmentAdapter`,模块级单例 `attachmentAdapter`(无状态,免重建抖动)。
  - `onNew` 签名扩展接收 `message.attachments`;把每个附件的 `FileMessagePart`
    扁平化为 push 线格式 `{filename, content_b64, mime}`,仅当非空时并入 POST body。
  - `useExternalStoreRuntime` 增 `adapters:{ attachments: attachmentAdapter }`。
- `components/assistant-ui/thread.tsx`
  - Composer 改 `flex-col`:上方 `<ComposerAttachments />` 展示待发送 chip,
    下方一行放回形针 `ComposerPrimitive.AddAttachment`(TooltipIconButton+PaperclipIcon)、
    输入框、发送/取消按钮。
  - 新增 `ComposerAttachments`:`ComposerPrimitive.Attachments` 自定义 Attachment 渲染
    (回形针 + 文件名 + `AttachmentPrimitive.Remove` 的 X 按钮)。
  - `UserMessage`:改为 `flex-col items-end`,先渲染 `MessagePrimitive.Attachments`
    附件 chip,再用 `MessagePrimitive.If hasContent` 包裹文本气泡(无正文时不显示空泡)。
  - 头部注释更新为「支持 send/cancel + 内联文件附件」。

## 校验
- `npx tsc --noEmit` → 退出码 0(修正:`AttachmentPrimitive.Name` 的 Props 为
  `Record<string,never>` 不接受 className,改为外层 `<span className=...>` 包裹)。
- `pnpm lint` → No ESLint warnings or errors。

## 非目标
- 端到端联调(Echo→真模型)留 Step 5;ADR-0017 留 Step 6。
- B. 按需拉取(pull)、OSS 持久化、分片/断点续传均为后续阶段(见 Plan 非目标)。
