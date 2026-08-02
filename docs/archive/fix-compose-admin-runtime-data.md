# fix: connect Compose runtime data to Admin operations

- 变更时间：2026-08-02 19:10 (+08:00)

## 变更理由

正式 `cocola start` 使用单节点 Docker Compose，但 Admin 的 Nodes 与 Storage 仅接入了
k3s/PVC 数据源，Service Logs 仅读取本地开发目录 `.run-logs`。因此生产部署成功后，这
三个正式导航页面会显示未配置或空数据。与此同时，Compose 实际已经拥有 Docker 节点
信息、host-backed Session 目录和容器 stdout，只是缺少安全的产品适配层。

未配置兼容模型时，对话输入框还会把当前 Agent Runtime 的品牌名称写入提示文案，造成
模型配置问题与特定 Agent 实现绑定。

## 变更内容

- `apps/admin-api/cmd/storage-probe`：扩展为 Compose 内部 host agent，增加 Admin Key
  鉴权、单节点信息、Session 目录清单与清理、白名单容器日志读取；保留原有 k3s
  storage probe 接口。
- `apps/admin-api/internal/service`：新增 Compose 节点管理只读适配和 host-backed
  Session Storage 监控，支持容量、单卷度量、Workspace 浏览与孤儿目录清理；k3s/PVC
  实现保持不变。
- `apps/cli/internal/assets/compose.yaml`：使用现有 `cocola-storage-probe` 镜像启动内部
  `host-agent`，只在 Compose 网络内提供服务，并将数据源接入 Admin API 与 Web。
- `apps/web/app/admin`：保留现有 Nodes 页面与 Add Node 开发中弹窗；Compose 节点显示
  单节点运行状态，Storage 文案同时兼容 host volume 与 PVC。
- `apps/web/app/api/admin/component-logs`：生产模式读取白名单 Cocola 容器日志，开发模式
  继续读取 `.run-logs`。
- `apps/web/components/assistant-ui/thread.tsx`：未配置模型时统一显示模型无关的英文提示。
- 测试覆盖 host agent 鉴权、Session 目录清理、节点与容器日志响应、Compose 节点适配和
  host 路径契约。
