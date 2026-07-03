# fix: k3d sandbox image pull address

- 变更时间：2026-07-03 21:36 (+08:00)

## 变更理由

`make up-k8s` 启动后，OpenSandbox Kubernetes runtime 创建 sandbox 时 Pod 进入
`ImagePullBackOff`，最终返回 `KUBERNETES::POD_READY_TIMEOUT`。Kubernetes
事件显示 Pod 正在拉取 `localhost:5001/cocola/sandbox-runtime:dev`，但在 k3d
节点容器内 `localhost` 指向节点自身，不是宿主机 registry，因此连接被拒绝。

## 变更内容

- `scripts/run-stack-k8s.sh`：区分宿主机 push 地址和 Kubernetes pull 地址。宿主机继续 push 到
  `localhost:5001`，OpenSandbox sandbox pod 改为拉取
  `cocola-registry.localhost:5000/cocola/sandbox-runtime:dev`。
- `scripts/run-stack-k8s.sh`：`down-k8s` 在卸载 OpenSandbox 前先 best-effort 删除
  `BatchSandbox` 和带 `opensandbox.io/id` 标签的 sandbox Pod，避免运行中的 sandbox
  挂住 `cocola-plugins` PVC 导致 `kubectl delete pvc` 长时间卡住。
- `Makefile`：同步 `verify-opensandbox-k8s` 的默认镜像地址。
- `deploy/opensandbox-k8s/README.md`：说明本地 k3d registry 的两种地址及
  `localhost:5001` 不能直接传给 Pod 的原因。
