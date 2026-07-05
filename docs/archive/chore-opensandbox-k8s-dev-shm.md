# chore: enlarge OpenSandbox sandbox /dev/shm

- 变更时间：2026-07-06 00:47 (+08:00)

## 变更理由

用户在 `make up-k8s` 运行链路中让 agent 对 HTML 文件进行 Chromium 截图时，Chromium 进程长时间不退出，最终触发 sandbox-manager 的 OpenSandbox Exec 默认超时并报 `opensandbox: sse read: context deadline exceeded`。现场排查发现 sandbox pod 默认 `/dev/shm` 只有 64Mi，且 Chromium 多进程渲染会依赖共享内存，容易在容器环境下卡住或失败。

## 变更内容

- `deploy/opensandbox-k8s/batchsandbox-template.yaml`：新增 cocola 本地 BatchSandbox 模板，为 sandbox 容器挂载 256Mi memory-backed `/dev/shm`。
- `deploy/opensandbox-k8s/values.local.yaml`：将模板 ConfigMap 挂载到 OpenSandbox server，并让 `batchsandbox_template_file` 指向 cocola 模板。
- `scripts/run-stack-k8s.sh`：在安装 OpenSandbox 前创建 system namespace 与 BatchSandbox 模板 ConfigMap，卸载时清理该 ConfigMap。
- `deploy/opensandbox-k8s/README.md`：补充 `/dev/shm` 模板说明和资源影响。
- 关键取舍：先只影响 `make up-k8s` 本地 K8s 链路；`256Mi` 不预分配，但实际使用会计入节点内存压力。
