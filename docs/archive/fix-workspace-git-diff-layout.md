# fix: 优化 Workspace Git Diff 布局

- 变更时间：2026-07-23 16:52 (+08:00)

## 变更理由

Project 对话 Workspace 的 Git Diff 页面在 Unified 和 Split 模式下存在行号列被压缩、行号与变更颜色条相互拥挤的问题。Diff 内容区还重复展示文件名，Git 快照头部占用空间过多，滚动条样式也与代码审阅界面不协调。

## 变更内容

- `apps/web/components/assistant-ui/workspace-panel.tsx`：将 Git 快照头部收敛为单行信息布局；删除 Diff 内容区重复文件标题；根据当前 Diff 最大行号动态计算 gutter 宽度。
- `apps/web/app/globals.css`：固定 Diff gutter 的列契约，使用不参与表格列宽计算的内部颜色轨道；优化 Unified/Split 最小宽度、代码字体、语法配色和细圆角滚动条。
- `apps/web/lib/git-history.mjs`：新增基于最大行号计算 Diff gutter 宽度的纯函数。
- `apps/web/lib/git-history.test.mjs`：覆盖三位、四位及更大行号的 gutter 自适应行为。
- 保持 `react-diff-view` 的单表格同步滚动模型，不使用伪造的双滚动条或运行时 DOM 测量。
