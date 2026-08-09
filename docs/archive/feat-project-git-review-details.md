# feat: 完善 Project Git 审查详情

- 变更时间：2026-08-09 12:17 (+08:00)

## 变更理由

Project Git 面板的 Change Request 三阶段没有占满卡片宽度，提交详情只能平铺文件路径且缺少每个文件的真实增删统计；对话框中被截断的任务分支也依赖不稳定的浏览器原生 title。与此同时，Squash merge 会直接触发合并，未在操作前明确说明提交压缩和任务只读的结果。

## 变更内容

- `apps/web/components/assistant-ui/workspace-panel.tsx`：改为覆盖整行的 Changes / Review / Main 进度轨道；用紧凑目录分组展示提交文件和增删统计；点击文件继续进入现有 diff；Squash merge 前使用居中的 HeroUI 确认弹窗。
- `apps/web/components/assistant-ui/project-branch-control.tsx`：使用无边框 HeroUI Tooltip + ghost Button 展示完整分支、base ref 和 base SHA，补齐键盘与悬浮交互。
- `apps/web/lib/git-history.mjs`：新增纯函数负责稳定的目录和文件排序，并增加行为测试。
- `apps/agent-runtime/cocola_agent_runtime/project_git.py`：复用提交详情已有的单次 `git --numstat`，在计算总数时同步生成真实的文件级 additions/deletions；重命名文件合并新旧路径统计，不增加额外 Git 进程。
- `packages/proto/cocola/agent/v1/agent.proto`、Agent Runtime/Gateway/Web 类型链路：透传文件级 additions/deletions，并重新生成 Go/Python Proto 桩。
- 相关 Python、Web 和 Gateway 测试覆盖文件统计、Proto 映射、目录分组、完整宽度进度条、分支 Tooltip 与合并确认。

## 关键取舍

- Squash merge 仍由 Provider 原子执行；UI 仅增加明确确认，不提供对 `main` 的 force reset。
- 已合并变更如需撤销，应通过新的 revert commit 和 Change Request 交付，保留审计记录并继续遵守分支保护。
