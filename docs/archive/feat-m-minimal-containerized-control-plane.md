# feat: M-minimal 纵向切片 —— 全容器化控制面 + 沙箱 + 持久化

## 背景

cocola 此前已实现 sandbox-manager(分配/绑定/销毁)、docker provider(三层卷
挂载 + 销毁保留宿主卷)、sandbox-cli,但**没有任何应用服务的 Dockerfile**,
docker-compose 只起 pg/redis/minio 基础设施。部署者必须在宿主机手动装 Go
(且因传递依赖 otel/trace@v1.44.0 要求 go≥1.25)再手动 build,部署摩擦高,
也不符合"所有节点都能跑在容器内、像 RocketMQ 一键起"的目标。

经评审(docs/plan/m-minimal-vertical-slice.md),确定方向:**不重写,收口纵向
切片**——把最核心的一条线(控制面分配沙箱 → 沙箱外挂卷持久化)做成
`docker compose up` 一键可跑、`down && up` 数据不丢的最小 demo。

## 改动

### Step 1 — docker provider 支持 COCOLA_SANDBOX_ROOT
`apps/sandbox-manager/internal/provider/docker/docker.go`:`New()` 读取
`COCOLA_SANDBOX_ROOT`,非空则覆盖默认卷根 `$HOME/.cocola/sandboxes`(复用已有
`WithRoot`,<10 行,向后兼容)。这是 DooD 路径同构的关键接缝,也是未来指向
NFS/NAS 挂载点的入口。

### Step 2 — sandbox-manager 多阶段 Dockerfile(新增)
`apps/sandbox-manager/Dockerfile`:build 阶段 `golang:1.25-alpine` 把 go 工具链
锁进镜像(部署者宿主机无需装 go);build context 为仓库根以满足 go.mod 的相对
replace;`GOWORK=off CGO_ENABLED=0` 产出静态二进制;runtime 阶段 `alpine:3.20`
仅含二进制 + ca-certificates。

### Step 3 — 全栈 docker-compose(新增)
`deploy/docker-compose/docker-compose.yml`:redis + sandbox-manager 两服务。
sandbox-manager 挂 docker.sock(DooD)调用宿主 daemon 创建用户沙箱;卷根用
**路径同构**(`${COCOLA_DATA_ROOT}:${COCOLA_DATA_ROOT}` source==target)+
`COCOLA_SANDBOX_ROOT` 注入,确保容器内算出的 bind-mount Source 与宿主一致。
不破坏既有 docker-compose.dev.yml。

### Step 4 — make demo-minimal 一键脚本 + 验收(新增)
`scripts/demo-minimal.sh` + Makefile `demo-minimal` target。脚本完成 §6 全部
验收:compose up → demo loop(DEMO OK)→ 持久化证明(A 写/销毁/B 读回)→
重启证明(down && up 后仍可读回)。

## 关键设计:DooD 路径同构

sandbox-manager 自身在容器内,但它创建的用户沙箱用 bind mount,Source 由宿主
Docker daemon 解析。若容器内外路径不一致,卷会挂空、持久化静默失效。对策:
卷根在宿主和容器内是同一绝对路径(source==target 挂载)+ COCOLA_SANDBOX_ROOT
指向它。验收脚本的持久化/重启两条专门验证此点。

## 存储演进(见 plan §9)

- demo/单机:本地 bind mount。
- 生产:**自建 NFS server**(复用开源镜像,不自研),把导出目录设为
  COCOLA_SANDBOX_ROOT —— 代码零改动。
- K8s:RWX PVC(nfs-subdir-external-provisioner),走 K8s provider。
- MinIO/S3 不做沙箱主存储(缺 POSIX 语义),仅作 T2 版本快照/归档层。

## 验证

- `gofmt -l` 干净;`bash -n scripts/demo-minimal.sh` 通过。
- `docker build -f apps/sandbox-manager/Dockerfile .` 成功产出镜像。
- `make demo-minimal` 端到端:DEMO OK + 持久化 + 重启全部通过。

## 不在本次范围

gateway/agent-runtime/llm-gateway/admin-api/web 的容器化、NFS HA/备份 ADR、
K8s provider —— 留待后续里程碑。本次为纯加法,无破坏性变更,可单点回滚。
