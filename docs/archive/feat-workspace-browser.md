# feat: Agent 对话页增加 Workspace 文件浏览与预览

- 变更时间：2026-07-16 22:48 (+08:00)

## 变更理由

用户需要在 Agent 对话页直接查看当前 Conversation 的持久化 Workspace，即使 Sandbox 已被回收也不应重新 Acquire。该能力必须只读、限定在 `/workspace`，并复用现有节点级 Storage Probe，避免引入新服务、后台扫描或 MinIO checkpoint。

## 变更内容

- `apps/admin-api/cmd/storage-probe`：增加按请求执行的 Workspace 目录分页和文件读取接口，使用 Go `os.OpenRoot` 限制根目录，拒绝路径逃逸与符号链接，并实现类型、敏感文件、大小、超时和并发限制。
- `apps/admin-api/internal/service`：复用 Session PVC/PV/Storage Probe 定位逻辑，校验 Conversation 所有权、当前 generation、节点和 StorageClass，向用户接口返回稳定错误语义。
- `apps/admin-api/internal/httpapi`：增加 `/me/workspaces/{session_id}/entries` 与 `/file`，输出只读安全响应头，并对 SVG 增加 sandbox CSP。
- `apps/web/app/api/conversations/[id]/workspace`：增加使用现有 Runtime Token 的同源代理，不触发 Sandbox Acquire。
- `apps/web/components/assistant-ui`、`apps/web/app/page.tsx`：增加右侧 Workspace Dock、懒加载文件树、手动刷新、桌面双栏与移动端单栏预览；复用只读 Monaco、Markdown、图片和 PDF 预览，不提供编辑、下载或轮询。
- `go.work`、`apps/admin-api/go.mod`、GitHub Actions：将相关 Go 工具链基线调整为 1.24 以使用 `os.OpenRoot`，升级对应 golangci-lint v1 工具，并让 CI 显式检查 `go.work` 内的模块。
- 后端测试覆盖目录排序/分页/上限、敏感文件、路径与 Symlink 逃逸、预览限制、并发限制、身份透传、错误码和安全响应头。

## 关键取舍

- 仅支持 managed k3s/local-path PVC；legacy-docker 返回功能未配置。
- 文件访问完全按请求触发，没有定时器、后台循环、递归目录扫描或 Workspace 状态迁移。
- Sandbox 不在线时直接读取原节点 Session PVC；节点不可用只提示重试，不创建空 Workspace。
