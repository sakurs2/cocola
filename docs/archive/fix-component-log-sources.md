# fix: 收敛 Component Logs 日志源

- 变更时间：2026-07-11 18:58 (+08:00)

## 变更理由

Component Logs 原先枚举 `.run-logs` 下全部 `*.log`，导致构建、测试和临时排障日志混入 Admin 产品界面。Tail 实现还会先把完整文件读入内存，日志持续增长时会放大 Web 进程的内存占用。`cleanup.log` 在多次本地启动间持续追加，成为当前增长最快的文件。

## 变更内容

- `apps/web/app/api/admin/component-logs/route.ts`：日志源改为六个核心组件固定白名单，并将 Tail 读取限制为文件末尾 2 MiB。
- `apps/web/app/admin/component-logs/page.tsx`：下拉框展示友好组件名，移除本地日志目录路径。
- `scripts/run-stack.sh`：每次启动重置累计型 `cleanup.log`，核心组件日志继续沿用启动时截断语义。
- `docs/frontend-tech-stack.md`：记录 Component Logs 的来源和读取边界。
