# fix: Clean up stale development Sandbox runtime images

- 变更时间：2026-07-26 02:30 (+08:00)

## 变更理由

本地 `make dev` 每次预拉取可变的 Sandbox runtime `latest` 镜像后，containerd 会保留被替换的无标签旧版本。旧版本持续累积会让 k3d 节点的镜像文件系统反复超过 80%，即使主机仍有少量可用空间也会触发启动保护。此前提示用户执行全局 Docker 缓存清理，既需要频繁手工操作，也可能影响其他项目。

## 变更内容

- `scripts/run-stack-dev.sh`：预拉取完成后，仅查找并删除同一 Sandbox runtime 仓库下的 dangling 旧镜像；显式保留当前 tag/digest 和仍被容器引用的旧镜像，删除失败时继续运行并交由既有容量检查兜底。
- `scripts/run-stack-dev.sh`：保留 80% 安全阈值，并根据当前 Docker context 为 OrbStack 输出准确的磁盘处理提示。
- `scripts/run-stack-dev-test.sh`：覆盖镜像仓库解析、精确过滤、当前镜像保护、容器引用保护、旧镜像清理和无旧镜像场景。

## 关键取舍

- 不执行全局 `docker builder prune`，避免删除其他项目缓存。
- 不提高容量阈值，避免 kubelet 在高水位回收刚刚预拉取的 Sandbox runtime。
- 清理范围限定为当前 Sandbox runtime 仓库的无标签镜像，当前镜像和其他仓库均不受影响。
