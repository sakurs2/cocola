# feat: artifact preview and response status

- 变更时间：2026-07-03 01:32 (+0800)

## 变更理由

用户希望 agent 输出文件在右侧预览时能按内容类型渲染：图片直接呈现、Markdown 正常排版、代码带高亮；同时希望 agent 回答区域能展示当前模型名称，并在回答未结束时给出明确状态标识。

## 变更内容

- apps/web/components/assistant-ui/markdown-text.tsx：抽出可复用的 Markdown 与代码块渲染组件，保留现有 assistant-ui Markdown 渲染能力。
- apps/web/app/page.tsx：右侧 artifact 预览根据 MIME 和文件扩展名分流到图片、PDF、Markdown、代码或纯文本渲染。
- apps/web/components/assistant-ui/thread.tsx：assistant 消息头部展示模型名称，并在当前回答运行中显示状态标识。
- 关键取舍：v1 继续复用现有轻量代码高亮实现，不引入新的高亮依赖。
