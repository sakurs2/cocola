# feat: sandbox node capacity routing

- 变更时间：2026-07-04 18:26 (+08:00)

## 变更理由

管理员需要在 k3s runtime 下管理集群节点，并为每个节点配置最大并发 sandbox pod 数。当某个节点的 sandbox 数达到上限后，新 sandbox 应落到其他仍有配额的节点；所有可用节点都没有配额时，对话侧应返回资源繁忙。

## 变更内容

- `apps/admin-api`：为 sandbox node 管理增加 k3s 模式开关、节点容量 annotation 读写、capacity PATCH 接口及测试。
- `apps/sandbox-manager`：新增 Kubernetes 节点/pod 读取能力和容量选择器；冷创建 sandbox 前选择有剩余配额的目标节点；容量耗尽时返回 ResourceExhausted；OpenSandbox 创建请求透传 nodeSelector。
- `apps/agent-runtime`：将 sandbox-manager 的 ResourceExhausted 映射为用户可读的资源繁忙错误。
- `apps/web`：完善 `/admin/sandbox-nodes` 页面，支持 unsupported 空态、Add node、Offline 二次确认、Max Sandbox Pods 编辑和确认弹窗。
- `scripts/run-stack-k8s.sh`：为 k8s 启动路径设置 `COCOLA_CLUSTER_MANAGER_MODE=k3s`。
- 关键取舍：第一版使用每节点 sandbox 数作为容量阈值，不做自动“最优值”写入；OpenSandbox 侧通过 `nodeSelector` 表达目标节点调度意图。
