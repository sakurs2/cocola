# P2:provider/opensandbox 流式 Exec + Pause/Resume 映射 + 单测,回填 ADR-0013(#21)

## 目标

承接 P1 骨架,落地 ADR-0013 三个关键未知里风险最高的一项——**Exec 流式语义**,
并补齐 **Pause/Resume** 生命周期映射。受「沙箱内禁止起监听进程」约束,本阶段做
**不依赖真 server 的部分**(route A):实现代码 + `RoundTripper`/SSE stub 单测验证
channel 桥接逻辑,真 server 端到端实测(resume 延迟、能力归属)留作「待环境」。

## 改动

### `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`

- **Exec(流式,核心)**:两步实现,无损桥接到 cocola `<-chan ExecEvent`。
  - 第一步 `resolveExecd`:经 lifecycle `GET /v1/sandboxes/{id}/endpoints/44772`
    (`execdPort`,镜像官方 SDK `DefaultExecdPort`)解析出沙箱内 execd 的可达 URL +
    鉴权/路由头(`X-EXECD-ACCESS-TOKEN`,缺失时回落到 provider API key)。
  - 第二步:对 `{execd}/command` 发 `Accept: text/event-stream` 的 SSE POST,
    body 为 `runCommandRequest`(cocola `req.Cmd` 以空格拼成单条 shell 命令,
    对齐 docker/k8s 后端的 shell-exec 契约;带 cwd/envs/timeout)。
  - `bridgeExecSSE` + `processSSEPayload`:在独立 goroutine 里读流并翻译事件——
    `stdout`/`stderr` 直映 `ExecEvent{Stdout|Stderr}`;`error` 的 evalue 能 `Atoi`
    时映射为 `ExecEventExit{Exit:n}`,否则为 `ExecEventError`;`execution_complete`
    映射 `ExecEventExit`(无错补 0);流自然结束且无显式终止事件时**合成** exit 0,
    使调用方总能见到终止事件。同时兼容两种线格式:标准 `data:` 前缀 SSE 与裸
    NDJSON;scanner 缓冲扩到 4MiB 防大块 stdout 截断。channel 恰好关闭一次。
  - 生命周期:Exec 用独立 `stream` 客户端(无 client 级 timeout),由每次 Exec 的
    `context` + `req.Timeout`(0 时回落 `defaultExecTimeout=5m`)界定;goroutine 退出
    时 `cancel()` + `resp.Body.Close()` 必定释放连接。
- **Pause** -> `POST /v1/sandboxes/{id}/pause`;**Resume** -> `POST .../resume`。
- **WriteFile / ReadFile** 仍为 `errNotImplemented`:映射 execd multipart 上传 /
  ranged 下载,不在流式 exec 关键路径上,留作后续。
- 新增 `stream *http.Client` 字段;`WithHTTPClient` 同时注入 lifecycle 与 stream
  两个客户端(单测据此用同一 RoundTripper 桩两类请求)。
- 新增常量 `execdPort=44772`、`execdAuthHeader`、`defaultExecTimeout`、
  `execEventBuffer=32`;新增 wire 类型 `endpointInfo`/`runCommandRequest`/
  `ssePayload`。客户端与 SSE 桥接均 **stdlib-only(零外部依赖)**,未引入官方 SDK。

### `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`

- 原 `TestDeferredMethods_ReturnNotImplemented` 收窄为
  `TestDeferredFileMethods_ReturnNotImplemented`(仅 WriteFile/ReadFile)。
- 新增:`TestPause_PostsPauseEndpoint`、`TestResume_PostsResumeEndpoint`(断言
  POST `.../pause`、`.../resume`)。
- 新增 Exec 系列(SSE stub,**不开 socket**):
  - `TestExec_BridgesSSEStream`:端到端走两步——先答 endpoints 解析、再服 NDJSON
    流;断言两跳命中、`Accept: text/event-stream`、execd token 头来自 endpoint
    headers、command body(`echo hi`/cwd/env),并校验 stdout/stderr/exit 三事件。
  - `TestExec_ErrorEventMapsToExitCode`:`error` evalue=数字 -> `ExecEventExit`。
  - `TestExec_NonNumericErrorMapsToErrorEvent`:非数字 error -> `ExecEventError`。
  - `TestExec_EmptyCommandRejected`:空命令在发 HTTP 前即报错。
  - `TestExec_StreamEndSynthesizesExit`:流无显式终止 -> 合成 exit 0。

## 验证(本机 go1.25.0 darwin/arm64,GOWORK=off)

- `gofmt -l` 无差异;`go build ./...`、`go vet ./...` 通过。
- `go test ./internal/provider/opensandbox/`:**17/17 全绿**;`-race` 干净
  (覆盖 Exec 桥接 goroutine)。

## 回填 ADR-0013

- 新增「PoC Findings(#18 / P0-P2)」节:三个关键未知全部正向解除——① Exec 流式
  无损桥接(execd SSE/NDJSON);② 生命周期一一对应;③ 能力重叠属实、字段层面可承接。
- Exec 流式风险项标注「PoC 已解除」(代价:Exec 多一跳 endpoint 解析)。
- Followups 更新:六方法已落地;Status 仍 Proposed,**待真 server 端到端实测**
  (resume 延迟对照 #15、Vault/egress 归属、是否补 WriteFile/ReadFile)后转 Accepted。

## 边界与遗留

- 仅离线(静态 + SSE stub 单测)验证,**未对真 OpenSandbox server / execd 跑端到端**
  (二者均必监听端口,受沙箱约束;留合规环境)。
- 待真环境实测项:resume RAM-kept 时延(对 #15 的价值)、Vault/egress/卷模型能力
  归属、WriteFile/ReadFile 是否补齐。
