# fix: 恢复 Project Plan 交互并统一确认弹窗

- 变更时间：2026-08-09 11:30 (+08:00)

## 变更理由

Project 新任务仍从 Project 历史字段读取 Agent Runtime，与当前只使用平台默认 Runtime 的产品逻辑不一致；当运行时配置尚未加载或历史配置失效时，Project 对话框无法进入可用状态，Plan mode 和后续 Plan Card 操作也随之不可用。与此同时，对话框里的任务分支标签存在多余边框，截断后无法查看完整内容；部分二次确认仍使用右侧 Sheet，与全局居中确认规范不一致。

## 变更内容

- `apps/web/app/runtime-provider.tsx`、`apps/web/app/projects/[id]/page.tsx`：Project 新任务等待模型与 Runtime 配置就绪后，统一选择平台默认 Runtime，不再依赖 Project 的历史 `runtime_id`；Workspace 空目录恢复改用居中确认弹窗。
- `apps/web/components/assistant-ui/project-branch-control.tsx`、`apps/web/app/projects/[id]/tasks/[conversationId]/page.tsx`：移除对话框分支标签外框，并为截断分支补充完整悬浮信息。
- `apps/web/components/wiki/wiki-workspace.tsx`：Wiki 丢弃未保存修改改用居中危险确认弹窗。
- `apps/web/app/admin/sandbox-nodes/page.tsx`：节点下线与容量变更的最终确认改用居中 HeroUI 弹窗；容量数值编辑仍保留右侧编辑器。
- `apps/web/lib/*ui.test.mjs`：补充默认 Runtime、分支展示以及确认弹窗语义的回归测试。
- 关键取舍：右侧 Sheet 继续用于编辑、录入、选择与详情；只有不承载输入的二次确认统一迁移到页面中央，避免把两种交互语义混为一谈。
