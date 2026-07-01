# fix(web): 附件 chip 宽度随文件名自适应(不再占满整行)

## 问题

首页 composer 里上传的文件预览 chip,无论文件名多短都撑满一整行。根因是布局:
`AttachmentPrimitive.Root` 是块级 `flex` 盒,而它的父容器 `ComposerPrimitive.Root`
是 `flex flex-col` 且未设 `items-*`,交叉轴默认 `align-items: stretch`,于是 chip
被拉伸到整行宽度,与内容无关。名字 span 上的 `truncate` 只限制了文字上限,管不到
chip 外框。

## 改动

`apps/web/components/assistant-ui/thread.tsx`:

- Composer 待发送 chip(`ComposerAttachments`):`AttachmentPrimitive.Root` 加
  `w-fit max-w-full self-start`——`self-start` 取消 stretch,`w-fit` 让宽度收缩到
  内容,`max-w-full` 防超长时溢出容器。
- 已发送消息 chip(`UserMessage` 的 `MessagePrimitive.Attachments`):同样加
  `w-fit max-w-full`(该列本就是 `items-end`,补 `w-fit` 保持两处观感一致)。
- 文件名 truncate 上限 `12rem → 16rem`,给中等长度文件名更多完整显示空间,超长仍
  省略号截断。

## 校验

`apps/web` `tsc --noEmit` 全绿;`next lint` 无告警。
