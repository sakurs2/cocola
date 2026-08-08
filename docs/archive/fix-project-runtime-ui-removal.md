# fix: 移除 Project 的 Runtime 配置入口

- 变更时间：2026-08-09 00:45 (+08:00)

## 变更理由

New Project 页面在 Agent Runtime 选择器关闭后仍渲染空的 `Provisioning` 卡片，并让创建按钮依赖前端 `runtimeID`。这既造成无内容的大块空白，也把已经收敛为平台内部实现的 Runtime 概念暴露给用户。Project 详情和设置中还残留了 Runtime 展示与编辑入口，产品语义不一致。

## 变更内容

- `apps/web/app/projects/new/page.tsx`：删除 Runtime 查询、状态、默认值同步与请求字段；由 Gateway 统一填入平台默认 Runtime；移除空的 Provisioning 卡片，并将取消/创建操作收敛为紧凑的底部操作区。
- `apps/web/app/projects/[id]/page.tsx`：移除 Runtime 展示和设置项，保存 Project 设置时只提交名称与描述；保留内部 `runtime_id` 供平台创建任务时路由使用。
- `apps/web/lib/project-runtime-ui.test.mjs`：增加 Project 创建与详情页面不再暴露 Runtime 配置的回归测试，同时确认任务启动仍使用项目内部 Runtime 标识。

## 关键取舍

- 不删除数据库和 API 中的 `runtime_id`：它仍是平台内部执行路由所需字段，只是不再作为 Project 的用户选项。
- `Provisioning` 仍保留为真实的仓库初始化状态文案；仅删除没有任何可配置内容的创建页表单区。
