# fix(web): 多个附件 chip 同行排列 + 自动折行(不再各占一行)

## 问题

上一处修好了单个 chip 宽度自适应,但传两个文件时它们仍各占一行。根因:
`ComposerPrimitive.Attachments` / `MessagePrimitive.Attachments` 只把 chip 作为
并列子节点渲染,自身不带布局容器;而它们的父级——composer 根是 `flex flex-col`、
用户消息那列是 `flex flex-col items-end`——都是纵向堆叠,于是每个 chip 换行。

## 改动

`apps/web/components/assistant-ui/thread.tsx`,给两处 `Attachments` 各套一个
横向 flex-wrap 容器:

- Composer(`ComposerAttachments`):外套 `<div className="flex flex-wrap
  gap-1.5 empty:hidden [&:not(:empty)]:pb-1.5">`。chip 并排,一行放不下自动折行;
  `empty:hidden` 让无附件时不占空间也不留 padding。
- 用户消息(`UserMessage`):外套 `<div className="flex flex-wrap justify-end
  gap-1.5 empty:hidden">`,`justify-end` 保持附件靠右(与气泡同侧对齐)。

配合上一处 chip 的 `w-fit`,每个 chip 宽度贴合文件名,同行能放下就并排、放不下
才折行。

## 校验

`apps/web` `tsc --noEmit` 全绿;`next lint` 无告警。
