# Plan: M-minimal —— 最小可部署纵向切片(控制面 + 沙箱 + 外挂卷,全容器化)

- 状态: Proposed(待评审,先过一遍再动手)
- 日期: 2026-06-11
- 关联: ADR-0002(SandboxProvider 抽象)、ADR-0003(session↔sandbox 绑定)、ADR-0008(三层持久化)
- 决策人: @王佳辉

## 1. 目标(一句话)

`docker compose up` 一条命令,拉起**控制面(sandbox-manager)**;用 `sandbox-cli`
发一次请求 → 控制面分配一个**沙箱容器** → 沙箱**外挂卷**做持久化 → 在沙箱里执行 →
销毁沙箱后 `docker compose down` 再起,**用户数据仍在**。

这是一条端到端纵向切片,刻意只保留主干,验证 cocola 最核心的形态
(类比 RocketMQ:nameserver = sandbox-manager;broker + store = 沙箱 + 外挂卷)。

## 2. 范围:砍枝,只留主干

| 保留(主干) | 暂时旁路/不动(枝叶) |
| --- | --- |
| sandbox-manager(控制面:Create/Acquire/Exec/Destroy/Release) | gateway(BFF) |
| docker provider(已实现三层卷 + 持久化) | agent-runtime / shim / Route A·B |
| sandbox-cli(直连控制面的入口) | llm-gateway / 计费 / 配额 |
| Redis(Acquire 绑定所需) | admin-api / Skill Market |
| 外挂卷(userdata 跨会话持久化) | web 前端 |

> 入口决策:**用 sandbox-cli 直连控制面**,不经 gateway/agent-runtime。最快出 demo,
> 且能独立验证"控制面 + 沙箱 + 持久化"这条线本身是否成立。

## 3. 现状盘点(代码已就绪 vs 缺失)

| 能力 | 现状 | 证据 |
| --- | --- | --- |
| 控制面分配/绑定/销毁 | ✅ 已实现 | `internal/server/server.go` + `orchestrator/binder.go` |
| 沙箱三层卷挂载 | ✅ 已实现 | `provider/docker/docker.go` Create():userdata(RW)/workspace(RW)/plugins(RO) bind mount |
| 销毁保留宿主卷(持久化) | ✅ 已实现 | docker.go Destroy() 注释 "Host volumes are intentionally retained" |
| CLI 入口 | ✅ 已实现 | `cmd/sandbox-cli/main.go` demo/create/exec/destroy |
| **sandbox-manager 容器化** | ❌ 缺 | `apps/sandbox-manager/` 无 Dockerfile |
| **全栈 compose 编排** | ❌ 缺 | `docker-compose.dev.yml` 只起 pg/redis/minio,不含应用 |
| go 工具链锁定 | ❌ 缺 | go.mod 要求 go≥1.25(传递依赖 otel/trace@v1.44.0),部署者被迫装 go |

结论:**业务逻辑基本齐全,缺的只有"打包成容器 + 编排"这一层。** 不重写,只补口。

## 4. 关键技术难点:DooD 路径映射(必须解决)

docker provider 用 **bind mount**,`mount.Source` 是宿主机绝对路径
(`$HOME/.cocola/sandboxes/...`)。当 sandbox-manager 自己跑进容器、挂载
`/var/run/docker.sock` 调用宿主 Docker daemon(DooD 模式)时:

> sandbox-manager 进程在**容器内**计算出的路径,会被宿主 daemon 当成**宿主机路径**
> 解析。两者不一致 → 沙箱卷挂错目录或挂成空目录,持久化静默失效。

**对策(标准做法:路径同构)**——让卷根目录在宿主机和容器内是**同一个绝对路径**:

1. 在宿主机选一个固定卷根,例如 `${COCOLA_DATA_ROOT:-./.cocola-data}`(compose 里解析为绝对路径)。
2. compose 里把它**用相同的目标路径**挂进 sandbox-manager 容器:
   `- ${COCOLA_DATA_ROOT}:${COCOLA_DATA_ROOT}`(source 与 target 一致)。
3. 给 docker provider 增加一个 env `COCOLA_SANDBOX_ROOT`,让它用这个**绝对路径**作卷根,
   而不是 `$HOME/.cocola/sandboxes`(容器内 `$HOME` 与宿主不同,正是坑源)。
   - docker.go 已有 `WithRoot()` Option,只需在 `New()` 里读 `COCOLA_SANDBOX_ROOT` 注入,
     **改动 < 5 行,不碰 provider 接口。**

这样容器内算出的 `Source` == 宿主机真实路径,DooD bind mount 正确生效。

## 5. 实施步骤

### Step 1 — 给 docker provider 增加 COCOLA_SANDBOX_ROOT(<5 行)
在 `docker.New()` 里:若 `os.Getenv("COCOLA_SANDBOX_ROOT")` 非空,作为 root 覆盖默认值
(复用已有 `WithRoot`)。保持向后兼容:未设时仍用 `$HOME/.cocola/sandboxes`。

### Step 2 — 写 sandbox-manager 多阶段 Dockerfile
`apps/sandbox-manager/Dockerfile`:
- build 阶段 `FROM golang:1.25-alpine`,把 go 版本锁进镜像(部署者无需本机装 go)。
- 需要 monorepo 上下文(依赖 `packages/proto/gen/go`、`packages/go-common`)→
  build context 设为仓库根,`GOWORK=off go build` 单模块构建。
- runtime 阶段 `FROM alpine`,只拷二进制 + 装 `docker-cli`(若后续需要 exec 进沙箱可选;
  当前走 Engine API,不强依赖 cli)。`ENTRYPOINT ["/usr/local/bin/sandbox-manager"]`。

### Step 3 — 扩 docker-compose,加入 sandbox-manager(全容器化)
新增 `deploy/docker-compose/docker-compose.yml`(或扩 dev 版),加 service:
- `sandbox-manager`:build 指向仓库根 + Dockerfile;`depends_on: redis`;
  挂 `/var/run/docker.sock:/var/run/docker.sock`;挂 `${COCOLA_DATA_ROOT}:${COCOLA_DATA_ROOT}`;
  env:`COCOLA_SANDBOX_ROOT=${COCOLA_DATA_ROOT}`、`COCOLA_REDIS_ADDR=redis:6379`、
  `COCOLA_SANDBOX_ADDR=:50051`;`ports: 50051:50051`。
- 复用现有 redis service(Acquire 绑定需要)。
- minio/postgres 在最小切片里**非必需**,可保留但不阻塞。

### Step 4 — 一键脚本 / Makefile target
`make demo-minimal`:`docker compose up -d sandbox-manager redis` → 等端口就绪 →
跑 `sandbox-cli -addr :50051 demo`。

## 6. 验收标准(可执行、二元判定)

1. `docker compose up -d sandbox-manager redis` 成功,容器 healthy。
2. `sandbox-cli -addr :50051 demo` 输出 `DEMO OK`(create→exec→destroy 全通)。
3. **持久化证明**:
   a. `sandbox-cli create -user u1 -session s1` 得 sbx-A;
   b. `exec sbx-A -- sh -c 'echo persisted > /data/userdata/u1/proof.txt'`;
   c. `destroy sbx-A`;
   d. 再 `create -user u1 -session s2` 得 sbx-B;
   e. `exec sbx-B -- cat /data/userdata/u1/proof.txt` 输出 `persisted`。
4. **重启证明**:`docker compose down && up` 后重复 3e,数据仍在。
5. **部署者零 go 依赖**:全程只用到 docker / docker compose,未在宿主机装 go。

## 7. 风险与回滚

| 风险 | 缓解 |
| --- | --- |
| DooD 路径映射出错(最高风险) | Step 4 验收 3 专门验证持久化;路径同构方案见 §4 |
| 挂 docker.sock 的安全性(容器逃逸面) | demo 阶段接受;正式部署在后续 ADR 评估 rootless/sysbox |
| go.mod 要 go≥1.25 | 由 build 镜像 `golang:1.25` 承担,部署者无感 |
| 改动影响现有 Route A/B | 本切片不碰 agent-runtime;provider 改动向后兼容(env 缺省即旧行为) |

回滚:删除新增 Dockerfile / compose service / env 读取即可,无破坏性变更。

## 8. 锁定的决策

1. 入口 = **sandbox-cli 直连控制面**(不经 gateway/agent-runtime)。
2. 最小切片**只容器化 sandbox-manager**;其余服务的 Dockerfile 留到后续里程碑。
3. DooD 路径问题用**路径同构 + COCOLA_SANDBOX_ROOT** 解决,不改 provider 接口。
4. minio/postgres 不进最小切片必需路径(redis 必需,因 Acquire 绑定)。
5. 不重写、不动既有 ADR 抽象;纯加法。

## 9. 存储演进路径与选型(NAS / NFS / 对象存储)

### 9.1 概念澄清:外挂存储是"静态盘"还是"服务节点"?

这是个伪二选一。物理上它们**都是对外提供服务的节点**;真正的分界是
**对外暴露成什么接口语义**:

| 后端 | 暴露接口 | 沙箱怎么用 | 沙箱视角 |
| --- | --- | --- | --- |
| 本地盘 / 块存储 | 文件系统(内核) | 直接读写路径 | 静态目录,无网络 |
| NFS / NAS | 文件系统(NFS 协议) | `mount` 成路径后像本地盘读写 | **看着是静态目录,背后是服务节点** |
| MinIO / S3 | HTTP 对象 API | 必须 S3 SDK 显式 Put/Get | **明确是要调用的服务** |

- **NFS = 伪装成静态目录的服务节点**:后台是 daemon + 网络协议,但内核 NFS 客户端
  把它挂成一个路径,应用层无感,POSIX 语义齐全(open/seek/mmap、git、软链都行)。
- **S3/MinIO = 诚实的服务节点**:无挂载、无文件系统语义,只能按对象整存整取。

### 9.2 MinIO 能否当沙箱外挂存储?

**不能当"活工作目录",可当"快照/归档层"。** 沙箱跑 Claude Code,需要 **POSIX 文件
系统语义**:写 `~/.claude`(SQLite 会话库/memory)、`git clone`、随机读写 workspace。
对象存储**没有目录、不能原地改写、无文件锁、无 mmap**;SQLite 直接跑在 S3 上会损坏。
`s3fs`/`goofys` 之类把 S3 伪装成 FS 的方案性能差、原子 rename/锁不可靠,重 IO 负载会
出问题。因此 MinIO 不做主存储。

### 9.3 cocola 的存储选型(与 ADR-0008 三层对齐)

| 用途 | 选型 | 理由 |
| --- | --- | --- |
| 沙箱活工作目录(`~/.claude`、workspace) | **NFS / NAS**(对齐 Mira) | 唯一能跨节点共享 + 满足 POSIX |
| 版本快照 / 大文件归档 / 产物分享 | **MinIO / S3** | 异步快照、便宜、天然带版本与分享链接 |
| demo / 单机 | 本地 bind mount | 同一条路,换 NAS 只改 `COCOLA_SANDBOX_ROOT` |

> 结论:沙箱外挂用 **NFS server**,不用 MinIO 当主存储;MinIO 留作 T2 的版本/归档层
> (compose 里已有的 MinIO 正好物尽其用,角色是快照层而非挂载盘)。

### 9.4 demo → 生产 平滑演进(为什么不用改代码)

docker provider 用 **bind mount**,其 source 是"宿主机的一个绝对路径"——
**这个路径是不是 NFS 挂载,provider 根本不关心**。因此:

| 阶段 | 后端 | 落地方式 | 代码改动 |
| --- | --- | --- | --- |
| demo / 单机 | 本地目录 | `COCOLA_SANDBOX_ROOT=./.cocola-data` | 无 |
| 小规模自托管 | **NFS / NAS** | 宿主机挂好 NAS,`COCOLA_SANDBOX_ROOT=/mnt/nas/cocola` | **零改动** |
| K8s 生产 | **RWX PVC**(NFS/CephFS CSI) | 走 K8s provider,不再用 bind mount | 换 provider(ADR-0008 已规划) |

`COCOLA_SANDBOX_ROOT`(§4/Step 1 引入)正是预留给"指向 NAS 挂载点"的接缝:demo 设本地
目录,生产设 NFS 挂载点,K8s 阶段再让位给 PVC。这印证 ADR-0002 把存储后端藏在
`SandboxProvider` 抽象后的决策。

### 9.5 生产存储决策(已定)

- **决策:生产自建 NFS server**(不依赖企业既有 NAS)。理由:cocola 目标是
  "企业一次性自部署、数据不出企业",自建 NFS 让存储栈随项目一起交付,部署者
  无需预先具备 NAS 基础设施,符合开箱即用的自托管定位。
- **落地方式(复用开源,不造轮子)**:NFS server 用成熟开源镜像
  (如 `erichough/nfs-server` 或基于内核 `nfs-kernel-server` 的官方方案),
  以独立容器 / 独立节点部署,导出一个目录作为 cocola 卷根。**cocola 不自研
  NFS 实现**,只负责把导出目录对接到 `COCOLA_SANDBOX_ROOT` / K8s PVC。
- **拓扑**:
  - 单机 demo:本地 bind mount,不起 NFS。
  - 小规模:一个 NFS server 容器导出 `/export/cocola` → 各 sandbox 宿主机
    `mount` 到本地路径 → 设为 `COCOLA_SANDBOX_ROOT`。
  - K8s 生产:NFS server(或 NFS-Ganesha)+ `nfs-subdir-external-provisioner`
    动态供给 RWX PVC,对接 ADR-0008 的 T1b/T2 卷。
- **快照层分工不变**:NFS 承载"活"数据;MinIO/S3 承载 T2 版本快照与归档。
- **后续 ADR**:需要一份独立 ADR 记录"自建 NFS 的高可用与备份策略"
  (单点 NFS 是 SPOF;生产需评估 DRBD/Ganesha 集群或定期快照到对象存储)。

> demo 阶段不引入 NFS,但 §9.4 的演进路径保证从本地目录切到 NFS 挂载点
> **零代码改动**(只改 `COCOLA_SANDBOX_ROOT`),自建 NFS 的接入工作集中在
> 部署编排层,不影响 sandbox-manager / provider 代码。
