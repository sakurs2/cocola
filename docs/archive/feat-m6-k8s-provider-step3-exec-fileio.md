# M6 Step 3：K8s Provider Exec 流 + 文件 IO + 自愈

## 背景

Step 2 补齐了生命周期（Pause/Resume/Health）与跨副本 resolve。Step 3 实现
`SandboxProvider` 接口剩余的核心数据面方法——`Exec` / `WriteFile` / `ReadFile`，
并把 Docker provider 那条 `context deadline exceeded` 自愈逻辑迁移到 K8s。

## 关键设计

### 1. Exec：Pod exec 子资源 + SPDY 流

K8s 没有 Docker 那样的 `ContainerExecAttach`。命令通过 Pod 的 `exec` 子资源执行，
连接升级为双向 SPDY 流（`remotecommand.NewSPDYExecutor`）。stdout/stderr 通过
`chanWriter` 适配到 `ExecEvent` 通道，与 Docker provider 的流式契约一致。

- **Cwd/Env**：exec 子资源不接受工作目录/环境参数，故用 `env -C <cwd> K=V ...`
  前缀包裹命令，避免引入自定义 shell 约定。
- **退出码语义**:`remotecommand` 把非零退出暴露为 `utilexec.CodeExitError`。
  用 `errors.As` 捕获，转成正常的 `ExecEventExit{Exit: code}`,只有真正的流
  错误才发 `ExecEventError`——与 Docker 的 `ContainerExecInspect` 行为对齐。

### 2. 自愈:Exec 前透明唤醒休眠 Pod

这是 Docker `thawIfPaused` 的 K8s 对应物。reaper 一阶段回收会**删 Pod**休眠沙箱;
后续轮次复用同一沙箱时,`Exec` 不能直接报 "pod not found"。`ensureRunning`:

1. `Health` 判活;已 Running+Ready 直接返回。
2. 否则 `Resume`(从 binding 重建 Pod),记 info 日志。
3. 轮询 `Health` 直到 Ready 或 `readyTimeout`(默认 30s)超时。

`WriteFile`/`ReadFile` 同样先 `ensureRunning`,保证文件操作不会打在已休眠的沙箱上。

### 3. 文件 IO:tar over exec

exec 子资源没有 Docker 的 `CopyToContainer`/`CopyFromContainer`。改用经典做法:

- **WriteFile**:把单文件打成 tar,作为 stdin 喂给 Pod 内的 `tar -x -m -f - -C <dir>`。
- **ReadFile**:在 Pod 内跑 `tar -c -f - -C <dir> <base>`,从 stdout 读回 tar 再解包。

### 4. 可测性:注入式 podExecutor

fake clientset 无法承载流式 exec 协议。抽象出 `podExecutor` 接口,生产用
`spdyExecutor`,测试用 `WithExecutor` 注入 `fakeExecutor`(可脚本化 stdout、错误、
以及 tar handler 做 Write/Read 往返)。`buildClientset` 改为同时返回 `*rest.Config`
供 SPDY 流构建。

## 改动文件

- `apps/sandbox-manager/internal/provider/k8s/k8s.go`
  - 新增 `Exec` / `WriteFile` / `ReadFile` / `ensureRunning` / `chanWriter`。
  - 新增 `podExecutor` 接口 + `spdyExecutor` 实现 + `WithExecutor` Option。
  - `Provider` 增加 `restConfig` / `exec` / `readyTimeout` 字段;`New` 与
    `buildClientset` 相应调整(后者返回 `*rest.Config`)。
  - 新增 `defaultReadyTimeout = 30s`。
- `apps/sandbox-manager/internal/provider/k8s/k8s_test.go`
  - 新增 `fakeExecutor` / `markRunning` / `collect` 辅助。
  - 新增 5 个用例:Exec 流式 stdout+exit、非零退出转 Exit 事件、**自愈唤醒休眠
    Pod 后成功 exec**、WriteFile/ReadFile tar 往返。
- `apps/sandbox-manager/go.mod` / `go.sum`:`go mod tidy` 拉入 SPDY 流所需的间接
  依赖(moby/spdystream、gorilla/websocket 等)。

## 验证

`golang:1.25-alpine` 容器内(`GOWORK=off`、`-mod=mod`):

- `go build ./...` 通过
- `go vet ./internal/provider/k8s/` 通过
- `go test ./...` 全绿(k8s 包 15 个用例全部 PASS)
- `gofmt -l` 干净

## 不在本步范围

egress NetworkPolicy(Step 4)、main.go 接线与部署物(Step 5)、真实 gVisor 集群
端到端验收(Step 6)。
