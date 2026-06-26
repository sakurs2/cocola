# P1:provider/opensandbox 最小骨架(Create/Health/Destroy)+ 单测(#20)

## 目标

按 ADR-0013,把 OpenSandbox 封装为 cocola 的一个可插拔 `SandboxProvider` 后端,
先落最小骨架(Create/Health/Destroy 三方法端到端打通 REST),验证「新后端=新包+
一处 case」的接缝成立,**核心接口与 docker/k8s 后端零改动**(ADR-0002 铁律)。

## 改动

### 新增包 `apps/sandbox-manager/internal/provider/opensandbox/`

- `opensandbox.go`(~10.9KB):
  - `Provider` 实现 `provider.SandboxProvider` 全 8 方法;编译期断言
    `var _ provider.SandboxProvider = (*Provider)(nil)`。
  - **已实现**(打通 OpenSandbox REST 生命周期 API):
    - `Create` → `POST /v1/sandboxes`,映射 image/resourceLimits/env/metadata,
      并把 cocola egress allowlist 映射为 OpenSandbox `networkPolicy`
      (default-deny + per-domain allow);记录 cocola-sid → opensandbox-id。
    - `Health` → `GET /v1/sandboxes/{id}`,`state==Running` 判定 healthy,
      其余状态(Pending/Pausing/Paused/Stopping/Terminated/Failed)报 unhealthy
      并带 state 作 detail。
    - `Destroy` → `DELETE /v1/sandboxes/{id}`,并清除本地 id 映射。
  - **故意延迟到 P2**(返回 `errNotImplemented` 哨兵,非 panic):
    Exec(SSE/NDJSON 流式)、WriteFile(UploadFile)、ReadFile(DownloadFile)、
    Pause/Resume(snapshot)。返回清晰哨兵而非崩溃,使该后端注册后是安全的——
    选中它只会让「尚未实现的操作」失败,绝不打挂进程。
  - 客户端为 **stdlib-only**(`net/http` + `encoding/json`):三个生命周期调用
    无流式,薄客户端让 PoC 自包含、可离线构建;`OPEN-SANDBOX-API-KEY` 头鉴权。
    P2 再评估是否为流式 Exec 引入官方 Go SDK(其 SSE/NDJSON 处理在那时才划算)。
  - 连接配置:env(`COCOLA_OPENSANDBOX_URL` / `COCOLA_OPENSANDBOX_API_KEY`)
    + Option(`WithBaseURL/WithAPIKey/WithHTTPClient`)。
- `opensandbox_test.go`(~7.3KB,10 个用例,全绿):
  - 用注入的 `http.RoundTripper` stub 桩 REST 服务,**不开任何 socket**
    (遵守「沙箱内禁起监听进程」)。
  - 覆盖:New 缺 baseURL 报错、env 默认值(尾斜杠裁剪)、Create happy path
    (断言 method/path/鉴权头/Content-Type + body 里 image/cpu/memory/egress 映射
    全部落地)、Create 空 id 失败、5xx 透传、Health 健康/不健康、Destroy 删除并
    清映射、延迟方法返回 errNotImplemented 且不发 HTTP、mapResources 换算。

### 接缝 `cmd/sandbox-manager/main.go`(+2 行,wiring only)

- import 新包;`newProvider` 工厂加 `case opensandbox.ProviderName`。
- 符合该函数注释「新后端 = 一处 case + 一个包,无其他文件改动」;
  `provider.go` 核心接口、docker、k8s 后端**全部未触碰**。

## 验证(本机 go1.25.0 darwin/arm64,GOWORK=off)

- `go build ./...` 通过;`go vet ./...` 通过;`gofmt -l` 无差异。
- `go test ./internal/provider/...`:docker / k8s / opensandbox 全绿
  (opensandbox 10/10 pass)。

## 边界与遗留(交 P2)

- 仅静态 + 单测验证,未对真实 OpenSandbox server 跑端到端(server 必监听端口,
  受沙箱约束;留 P2 在合规环境验)。
- 三个待 P2 决策/验证项不变:① Exec 流式映射 `<-chan ExecEvent`;
  ② Pause/Resume(snapshot)resume 时延对 #15 的价值;③ egress / Vault / 卷模型
  能力归属(本骨架仅映射 egress 字段以「跑通」,不代表归属已定)。
