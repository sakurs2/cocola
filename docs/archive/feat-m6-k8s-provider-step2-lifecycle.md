# M6 Step 2：K8s Provider 生命周期（Pause / Resume / Health）

## 背景

Step 1 完成了 K8s+gVisor Provider 的骨架：`Create` / `Destroy` / PVC 生命周期 /
`resolve`，以及 fake clientset 测试。Step 2 补齐 `SandboxProvider` 接口里与
Docker 差异最大的三个方法——`Pause` / `Resume` / `Health`，并解决由此引出的
跨副本 resolve 问题。

## 核心设计：休眠语义与 Docker 的本质差异

Docker provider 用 cgroup freezer 原地冻结/解冻容器，容器对象始终存在，label
查询永远能命中。K8s 没有等价的"原地冻结"，因此 cocola 选择 **scale-to-zero
休眠**：

- **Pause** = 删除 Pod，保留两个 PVC（user + session）。休眠态零 CPU/内存占用，
  只留磁盘，比 Docker freeze 更省资源。幂等：删除已不存在的 Pod 视为成功。
- **Resume** = 用同一份描述重建 Pod，重新挂载同样的 PVC，使 `~/.claude` 会话
  文件原样回到原位，`claude --resume` 得以续接对话。幂等：Pod 已存在则空操作。
- **Health** = `Get` Pod，判断 `Phase==Running` 且 `PodReady=True`。休眠态（无
  Pod）返回 `Healthy:false` 且**不报错**，让调用方能区分"睡着了"和"坏了"。

## 关键问题：删 Pod 之后如何跨副本 resolve

Step 1 的 `resolve` 依赖 Pod 上的 label 做 List 查询。但 Pause 删除了 Pod，
label 查询将查不到休眠态沙箱，导致另一副本无法 Resume——直接违背 sandbox-manager
水平扩展的承诺。

解决方案：引入**持久化 binding ConfigMap**。`Create` 在建 Pod 的同时写一份带
四个 cocola label 的 ConfigMap，内含重建 Pod 所需的全部信息（image / env /
resources / user / session）。它在休眠期间存活，是跨副本、可休眠的唯一可信源：

- `resolve` 改为：内存缓存命中 → 否则读 binding ConfigMap（不再 List Pod）。
- `Resume` 从 binding 重建 Pod，因此 `Create` 与 `Resume` 产出的 Pod 字节一致
  （二者都调用同一个由 `binding` 驱动的 `podSpec`）。
- `Destroy` 在删 Pod 的同时删除 binding；user PVC 仍按 ADR-0008 保留。

## 改动文件

- `apps/sandbox-manager/internal/provider/k8s/k8s.go`
  - 新增 `binding` 结构体 + `record` 改为持有 `binding`。
  - `podSpec` 重构为由 `binding` 驱动，Create/Resume 共用。
  - 新增 `Pause` / `Resume` / `Health`。
  - 新增 `writeBinding` / `readBinding`，`resolve` 改读 ConfigMap。
  - `Destroy` 增加删除 binding ConfigMap。
  - 引入 `k8s.io/apimachinery/pkg/api/errors` 做 NotFound/AlreadyExists 判断。
- `apps/sandbox-manager/internal/provider/k8s/k8s_test.go`
  - 新增 6 个用例：Pause 删 Pod 留 PVC+binding、Pause 幂等、Resume 从 binding
    重建（保留 gVisor/label/env）、Resume 幂等、Health 三态（running/ready、
    paused/absent）、**跨副本 resolve**（空缓存的副本 B 仅凭 binding 即可 Resume）。

## 验证

`golang:1.25-alpine` 容器内（`GOWORK=off`、`-mod=mod`）：

- `go build ./...` 通过
- `go vet ./internal/provider/k8s/` 通过
- `go test ./...` 全绿（k8s 包 10 个用例全部 PASS）
- `gofmt -l` 干净

## 不在本步范围

Exec 流 / 文件 IO（Step 3）、egress NetworkPolicy（Step 4）、main.go 接线与部署物
（Step 5）、真实 gVisor 集群验收（Step 6）。
