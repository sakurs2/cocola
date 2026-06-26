# P0 调研:OpenSandbox REST / Go SDK / 流式 exec 摸底(#19)

## 方法说明(与原计划的合规调整)

原 P0 计划是「本机起 OpenSandbox FastAPI server 跑通 Docker runtime」。但项目硬约束
**禁止在沙箱内启动任何监听端口的进程**(即便 loopback),OpenSandbox server 必然
`bind 0.0.0.0:8090`。因此 P0 改为**静态源码 / OpenAPI 规格审查**:下载 OpenSandbox
源码 tarball(非 git clone,本机沙箱对 `.git` 写入受限),直接读其 `specs/*.yaml`
OpenAPI、`sdks/sandbox/go/` Go SDK 源码与 `server/` 部署描述。结论同样可定论三个
关键未知,且零监听进程。源码落在 cocola 仓库之外:`/Users/.../code/OpenSandbox-main`。

## 关键发现

### 1. 存在功能完整的官方 Go SDK(最大利好)

`sdks/sandbox/go/`(module `github.com/alibaba/OpenSandbox/sdks/sandbox/go`,
`go 1.20`,**`go.sum` 为空 = 零外部依赖、纯 stdlib**)。cocola 的 sandbox-manager
是 Go,可直接 import 该 SDK 包装,无需自己撸 HTTP/SSE 客户端。

### 2. 流式 exec —— 关键未知①:成立

- SDK 自带 `streaming.go`(SSE + NDJSON 解析)与 `RunCommand` / `RunInSession` /
  `ExecuteCode`,通过 `ExecutionHandlers` 回调逐事件推送:
  `OnInit / OnStdout / OnStderr / OnResult / OnComplete / OnError`。
- 这与 cocola `Exec` 返回的 `<-chan ExecEvent` 流式语义**天然契合**:在回调里把
  每个事件 `push` 进 channel 即可,无有损降级。`SkipAccumulation` 选项还能避免
  长任务的内存膨胀。

### 3. 生命周期 REST ↔ cocola 8 方法映射 —— 关键未知②:几乎一一对应

| cocola SandboxProvider | OpenSandbox Go SDK / REST |
|---|---|
| `Create`   | `LifecycleClient.CreateSandbox` → `POST /v1/sandboxes` |
| `Destroy`  | `DeleteSandbox` → `DELETE /v1/sandboxes/{id}` |
| `Pause`    | `PauseSandbox` → `POST /v1/sandboxes/{id}/pause` |
| `Resume`   | `ResumeSandbox` → `POST /v1/sandboxes/{id}/resume` |
| `Health`   | `GetSandbox` → `GET /v1/sandboxes/{id}`(读 `status.state`) |
| `Exec`     | `Sandbox.RunCommand`(SSE 流式,见上) |
| `WriteFile`| `Sandbox.UploadFile`(`io.Reader`)/ `UploadFiles` |
| `ReadFile` | `Sandbox.DownloadFile`(`io.ReadCloser`,支持 Range) |

- 鉴权:`OPEN-SANDBOX-API-KEY` 请求头(SDK `NewClient` 内置)。
- 状态机:`Pending → Running → Pausing → Paused → Stopping → Terminated / Failed`;
  Health 映射读 `SandboxStatus.State`。

### 4. 能力重叠是真实的,且 `CreateSandboxRequest` 已内建对应字段 —— 关键未知③

`CreateSandboxRequest` 字段直接覆盖 cocola 已自建的几项能力,意味着「能力归属」
冲突真实存在,需在 P2 决策(停用 cocola 自有 / 并存 / 改用 OpenSandbox):

- `NetworkPolicy{DefaultAction, Egress[]{Action,Target}}` ↔ cocola egress NetworkPolicy。
- `CredentialProxy{Enabled}` ↔ cocola #12 Vault(透明代理注入机密)。
- `Volumes[]`(支持 `Host` bind / `PVC` / `OSSFS`);其中 `PVC` 字段
  (`ClaimName / StorageClass / Storage / AccessModes / DeleteOnSandboxTermination`)
  与 cocola K8s 卷模型同构 —— 利于复用,也需对齐语义。
- `SnapshotID`:可「从快照创建沙箱」。

### 5. Pause/Resume 与 #15 RAM-kept resume 的关系

- K8s 后端 pause/resume 经 `BatchSandbox.spec.pause` + 内部 `SandboxSnapshot` 资源
  实现,外部可见状态流转 `Running → Pausing → Paused → Resuming → Running`
  [[OpenSandbox server]](https://github.com/opensandbox-group/OpenSandbox/blob/main/docs/components/server.md)。
- SDK 另有独立 `CreateSnapshot / GetSnapshot / ListSnapshots / DeleteSnapshot`
  (快照状态 `Creating/Ready/Deleting/Failed`)。这正是 #15 一直悬而未决的
  RAM-kept / 全状态 resume 的现成候选实现,留 P2 量测 resume 时延后定论。

### 6. 部署依赖(进程边界代价)

- server 是 Python FastAPI(`uvicorn`),依赖:`docker`、`kubernetes`、`redis>=5`、
  `httpx[socks]`、`websockets` 等(`server/pyproject.toml`)。
- Docker runtime 需挂 `docker.sock`,并依赖两个伴随镜像:`execd`(沙箱内执行代理)、
  `egress`(出口管控);K8s runtime 依赖其 CRD(`BatchSandbox` 等)。
- 即:启用该后端 = cocola 部署拓扑多一个 server 进程 + 两个镜像(+ K8s 下的 CRD)。
  这是 ADR-0013 已记录的「Go↔Python 进程边界」代价的具体清单。

## 对 ADR-0013 / 后续阶段的影响

- 三个关键未知(流式 exec、生命周期映射、能力重叠)**均已正向定论**,ADR-0013
  选定的「封装为可插拔 `SandboxProvider` 后端」路径成立,且因官方 Go SDK 的存在,
  P1 骨架成本比预期低(直接 import,不必手写 REST 客户端)。
- P1 调整:`provider/opensandbox` 直接依赖官方 Go SDK(纯 stdlib,无依赖膨胀风险),
  单测用 `httptest` mock REST 即可。
- P2 重点收敛为两项:① resume 时延实测(对 #15 的价值);② 能力归属决策
  (egress / Vault / 卷模型 三处与 cocola 现有实现的取舍)。
- 进程边界代价已量化(server + execd + egress 镜像 + 可选 CRD),供是否扩大决策参考。

## 产出与边界

- 本阶段零 cocola 代码改动,仅本调研记录。
- OpenSandbox 源码副本在 cocola 仓库之外,不纳入 cocola git。
- 未启动任何监听端口进程(纯静态源码 / 规格审查)。
