# feat: 重组 Admin 导航并新增 Toolbox

- 变更时间：2026-07-11 19:31 (+08:00)

## 变更理由

Admin 侧边栏原先混用了产品功能、技术层级和页面类型，Overview、AI、Logs 等分组无法准确表达管理员任务。System Prompt 又是单一轻量配置，却长期占用独立一级入口，后续同类小工具缺少简单一致的承载位置。

## 变更内容

- `apps/web/components/admin/admin-shell.tsx`：导航收敛为 Configuration、Operations、Infrastructure，Overview 固定顶部，Settings 固定底部，并统一 Tasks、Agent Runs、Sandboxes、Nodes 和 Service Logs 命名。
- `apps/web/app/admin/page.tsx`：Overview 使用相同分组与最新业务文案，不再展示独立 Settings 分组。
- `apps/web/app/admin/toolbox/`：新增轻量工具卡片页，System Prompt 通过现有 AdminDrawer 编辑并支持查询参数深链接。
- `apps/web/app/admin/prompts/page.tsx`：删除旧 Prompt UI，改为重定向至 Toolbox，后端接口和 Runtime 加载逻辑保持不变。
- `docs/frontend-tech-stack.md`：记录 Admin 信息架构和 Toolbox 扩展边界。
