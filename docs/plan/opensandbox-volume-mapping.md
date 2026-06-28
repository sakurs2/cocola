# Plan: 把 cocola 双卷文件系统模型映射到 OpenSandbox volumes

日期: 2026-06-28
关联: ADR-0008(持久化分层与双卷模型)、ADR-0014(OpenSandbox 主力)、ADR-0002(provider 接口)
状态: 已完成并真 server 验收通过(2026-06-28 本机 OpenSandbox server `-persist` 全绿:用户卷/.claude subPath/会话卷跨 destroy-recreate 持久化 + uid 写权限均通过;Docker named volume 多 subPath 挂载确认可用)

## 目标

让 opensandbox provider 在 `Create` 时把 cocola 的双卷文件系统理念(ADR-0008)
翻译成 OpenSandbox 的 `volumes` 字段,使「用户持久 / 会话工作区 / 系统只读 / 默认不持久」
四条语义在 OpenSandbox 后端上等价生效。当前 provider 一个 volume 都没传(只发了
image/env/resourceLimits/networkPolicy),持久化语义完全缺失。

## 事实依据(已核对源码)

### cocola 侧(docker provider 现状,作为对照基准)
docker provider 在 Create 时挂 4 个点(`docker.go`):

| 宿主目录(root 下) | 容器内路径 | 语义 | 读写 |
|---|---|---|---|
| `userdata/<userID>` | `/data/userdata/<userID>` | 用户长期文件(T2) | RW |
| `workspace/<sessionID>` | `/workspace/<sessionID>` | 会话工作区(T1b) | RW |
| `plugins/` | `/data/plugins` | 平台 skill | **RO** |
| `claude/<userID>` | `/home/cocola/.claude` | Claude Code 会话/记忆(T2,--resume 依赖) | RW |

非 root 用户 uid=10001(`sandboxUID`),`~/.claude` 需 chown 到该 uid 才能写。

### OpenSandbox 侧(schema.py + docker runtime,已确认可用)
`CreateSandboxRequest.volumes: List[Volume]`。每个 Volume:
- 三选一后端:`host`(host bind-mount,受 `storage.allowed_host_paths` 前缀 allowlist)
  / `pvc`(命名卷:K8s=PVC,Docker=named volume,支持 createIfNotExists/storageClass/
  storage/accessModes/deleteOnSandboxTermination)/ `ossfs`(阿里云 OSS)。
- 公共字段:`mountPath`(容器内绝对路径)、`readOnly`、`subPath`(后端路径下子目录)。
- docker runtime 已实现 `DockerVolumesMixin`:named volume 自动创建、随沙箱销毁清理
  (managed volumes label)、host allowlist 校验。

## 映射方案

### 后端选型
- **生产 / K8s**:用 `pvc` 后端。`claimName` 按 user / session 命名,天然对上
  ADR-0008 的 per-user PVC + per-session PVC。
- **本机 docker runtime**:同样用 `pvc`(在 Docker 下即 named volume),避免依赖
  `host` 后端(host 后端要求服务端配 `allowed_host_paths` 且路径在 server 容器与宿主间
  同构,运维约束重)。`subPath` 用于在单卷内按 userID/sessionID 分目录。

### Volume 清单(provider.Create 生成)

| cocola 语义 | OpenSandbox Volume | mountPath | readOnly |
|---|---|---|---|
| 用户长期文件(T2) | pvc `claimName=cocola-user-<userID>`,createIfNotExists | `/data/userdata/<userID>` | false |
| Claude 配置/会话(T2) | **同一用户卷 `cocola-user-<userID>`,`subPath=.claude`**(已定:合到用户卷,不另开卷) | `/home/cocola/.claude` | false |
| 会话工作区(T1b) | pvc `claimName=cocola-session-<sessionID>`,createIfNotExists,deleteOnSandboxTermination=false | `/workspace/<sessionID>` | false |
| 平台 skill | pvc `cocola-plugins`(共享,预置) | `/data/plugins` | **true** |

- **.claude 合卷实现注记(已定)**:不另开 PVC,而是把用户卷 `cocola-user-<userID>`
  挂两次 —— 一条 Volume 条目 `mountPath=/data/userdata/<userID>`(卷根),另一条
  同 `claimName`、`subPath=.claude`、`mountPath=/home/cocola/.claude`。物理上
  `.claude` 落在卷内 `<vol>/.claude`,与长期文件不重叠。同一卷被多条 Volume 引用:
  K8s 下同 PVC 多挂载点合法;Docker named volume 的多 subpath 挂载需在真 server
  实测确认(列为实现期验收子项)。好处:用户态数据(文件 + Claude 记忆/会话)
  归一到单卷,备份/迁移/配额按用户一处管理。
- 命名遵守 OpenSandbox 校验:`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`、≤253;userID/sessionID
  需 sanitize(复用 docker provider 的 `safe()` 同等规则:小写化 + 非法字符替换)。
- 「默认不持久」:不为任何其它路径声明 volume —— 与 OpenSandbox「不声明即 overlay」一致,
  天然满足,无需额外动作。
- T1b 不随沙箱销毁删除(会话结束由 cocola 编排层 GC,不交给 deleteOnSandboxTermination),
  以保证 hibernate(destroy 容器)后工作区仍在 —— 对齐 ADR-0008「destroy 容器 + 留卷」。

### 为什么不把会话工作区(T1b)也合进用户卷(只合了 .claude)
有人会问:既然 `.claude` 能合进 user 卷,会话工作区为何不也用 `subPath=<sessionID>`
压进同一个 user 卷?结论:不合。`.claude` 与会话工作区在三个维度上不同,合卷弊大于利:

1. **生命周期 / GC 冲突(最关键)**:T2 永久留存,T1b 由 cocola 编排层 GC。合卷后
   删一个会话 = 进卷删一个 subPath,而**不能删卷**(卷归用户、还装着别的会话和 .claude),
   GC 从「一条删卷命令」退化成「起清理容器挂卷 rm -rf subPath」或被迫依赖
   deleteOnSandboxTermination(而我们为 hibernate 留工作区刻意没用它)。分卷时
   每会话一卷,GC 就是删卷,干净。
2. **并发挂载约束**:同一用户可能同时开多个会话。合卷 = 多沙箱并发挂同一 user PVC;
   K8s `ReadWriteOnce` 只允许单节点挂载,多并发会话直接调度失败,要支持就得上
   `ReadWriteMany`(NFS/CephFS 类,运维成本陡增)。分卷时每会话独立 RWO 卷无此问题。
3. **配额 / 备份语义混淆**:T2 是要长期计费、备份、迁移的用户资产;T1b 是临时垃圾。
   混在一卷里,配额与备份策略无法分别施加。

`.claude` 之所以能合:它本质是 T2、per-user、与用户文件**同生命周期同 owner**,合进去
只省一个卷且利于整体备份。会话工作区是 T1b、per-session、**生命周期与并发模型都不同**,
合进去是把两套语义硬塞进一个卷。故:`.claude` 合入 user 卷,会话工作区保留独立的
`cocola-session-<sessionID>`。

### uid/权限
- OpenSandbox 不暴露 chown 钩子。两条候选,择一在实现期验证:
  1. 镜像入口脚本里 chown `~/.claude` 到 10001(推荐,自包含);
  2. 若 OpenSandbox 镜像默认就以目标 uid 运行,则新建卷首次写入即归属正确。
- 此项需在真 server 上实测确认,作为实现期的验收子项。

## 代码改动面(实现期,本 Plan 不含编码)

1. `opensandbox.go`:
   - `createSandboxRequest` 增 `Volumes []volumeSpec \`json:"volumes,omitempty"\``。
   - 新增 wire 类型:`volumeSpec{Name, PVC *pvcBackend, Host *hostBackend, MountPath, ReadOnly, SubPath}`、
     `pvcBackend{ClaimName, CreateIfNotExists, ...}`。
   - `Create` 内新增 `mapVolumes(spec) []volumeSpec`,按上表生成。
   - 复用 / 移植 docker provider 的 `safe()` 命名 sanitize。
2. 常量:把 guest 路径(`/data/userdata`、`/workspace`、`/data/plugins`、
   `/home/cocola/.claude`)抽为 provider 共享常量或在 opensandbox 包内重声明,保持与
   docker provider 一致(同一 brain 镜像,路径契约必须相同)。
3. 单测:`TestCreate_*` 断言 volumes 落到 wire(claimName / mountPath / readOnly);
   新增 `TestMapVolumes`(含 userID/sessionID sanitize、RO 平台卷)。
4. e2e:`cmd/opensandbox-verify` 增一段「跨 Create 的持久化」校验 —— 建沙箱写文件 →
   destroy → 同 user/session 再建 → 读回,验证 T2/T1b 卷复用。

## 与 WriteFile/ReadFile 缺口的关系
本 Plan 解决「持久卷挂载」缺口,与 WriteFile/ReadFile(execd upload/download)是
两件独立的事。卷映射不依赖 WriteFile;但完成后,WriteFile/ReadFile 仍建议补齐以满足
8 方法接口契约完整性(单列任务)。

## 验收
- `go build ./... && go test ./...` 绿;新单测覆盖 volumes 映射。
- 真 server e2e:跨 destroy-recreate 的文件持久化(T2 用户卷 + T1b 会话卷)通过。
- 平台 skill 卷确为 readOnly;非声明路径不持久。
- `~/.claude` 在 OpenSandbox 沙箱内可被非 root brain 写入(uid 方案二选一验证通过)。

## 不做
- 不接 `ossfs` 后端(对象存储是后续可选增强,非双卷模型必需)。
- 不引入 OpenSandbox snapshot 作 resume(ADR-0008 坚持从盘重建,不用 RAM 快照)。
- 不改 docker provider(它已是双卷模型的参考实现)。
