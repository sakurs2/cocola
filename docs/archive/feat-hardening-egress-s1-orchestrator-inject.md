# feat(hardening S1):编排层注入 egress Networking

## 背景

ADR-0009 上线前硬化项「egress allowlist 强制」的第 1 步(见
`docs/plan/hardening-sandbox-egress-allowlist.md`)。缺口根因之一是
`Binder.Create` 构造 `provider.SandboxSpec` 时根本不传 `Networking`,导致实跑
沙箱拿不到任何出网策略。本步把出网策略从配置注入并透传到 provider,为 S2/S3 两个
provider 的强制实装打地基。

## 改动

- 新增 `apps/sandbox-manager/internal/orchestrator/networking.go`:
  - `NetworkingFromEnv()`:读 `COCOLA_SANDBOX_EGRESS_ALLOWLIST`(逗号分隔
    域名/CIDR),trim + 去重;并从 `COCOLA_SANDBOX_LLM_BASE_URL` 解析 gateway
    host 自动并入(运维无需重复配置)。
  - `gatewayHost()`:从 base URL 提取裸 host(无 scheme/port),不可解析则返回空。
- `binder.go`:`Binder` 新增 `net provider.Networking` 字段 + `WithNetworking()`
  链式方法;`Create` 调用透传 `Networking: b.net`。零值(nil allowlist)保持各
  provider 既有默认,语义不破坏(安全默认收敛留待 S2/S3 在 provider 层做)。
- `cmd/sandbox-manager/main.go`:composition root 调用 `NetworkingFromEnv()` 并经
  `WithNetworking` 注入 binder。

## 验证

- `GOWORK=off go build ./...` 通过;`go vet` 干净;`gofmt -l` 无输出。
- 新增 `networking_test.go`:
  - `TestNetworkingFromEnv`:空→nil、逗号分隔 trim/去重、gateway host 并入、
    单独 gateway host、已列出不重复、非法 URL 忽略,共 6 例。
  - `TestBinderForwardsNetworking`:capturingProvider 断言 binder 把配置的
    allowlist 原样透传进 `provider.Create`。
- `go test ./internal/orchestrator/...` 全绿。

## 后续

- S2:Docker provider 用 iptables+ipset init-firewall 强制 allowlist(空=基线)。
- S3:K8s provider egressRules 默认并入 DNS+gateway 基线。
