# fix: 开发栈预拉取后检查 sandbox 镜像盘占用

- 变更时间：2026-07-19 19:36 (+08:00)

## 变更理由

本地 `make dev` 会先向 k3d 节点预拉取 sandbox runtime 镜像。当 Docker 磁盘占用过高时，预拉取虽然成功，kubelet 随后仍可能回收该镜像，造成日志已经显示 runtime ready、创建 sandbox 却长时间 Pending 并最终 `POD_READY_TIMEOUT`，诊断信息与真实原因脱节。

## 变更内容

- `scripts/run-stack-dev.sh`：预拉取完成后检查 k3d 节点 containerd 文件系统占用。
- 当占用超过 80% 时立即终止 `make dev`，输出镜像可能被 GC 的原因以及清理缓存、扩容和人工检查命令。
- 检查命令失败或无法解析占用率时同样 fail-fast，避免以不可信的 runtime ready 状态继续启动。
