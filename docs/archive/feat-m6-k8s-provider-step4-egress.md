# M6 Step 4：K8s Provider egress 强制（NetworkPolicy）

## 背景

ADR-0009 把"egress 锁定"列为强制安全前提。Docker provider 用
`NetworkMode=none` 默认断网。K8s 侧对应物是 **NetworkPolicy**:`Create` 时按
`SandboxSpec.Networking.EgressAllowlist` 为每个沙箱生成一条出网策略,
`Destroy` 时一并删除。

## 语义设计

策略用 `sandbox-id` label 选中沙箱 Pod,`PolicyTypes: [Egress]`:

- **空 allowlist → 拒绝所有出网**:`PolicyTypes:[Egress]` 但 egress 规则为空,
  等价于 Docker 的 `NetworkMode=none`,是安全默认。
- **非空 allowlist** 自动放行三类:
  1. **DNS**(UDP/TCP 53),否则名字都解析不了;
  2. **集群内出网**(`namespaceSelector: {}`),让 Route A 能打到 llm-gateway
     Service;
  3. allowlist 里每个 **CIDR / IP** 转成一条 `ipBlock` peer(IPv4 补 `/32`,
     IPv6 补 `/128`)。

## 已知边界:域名条目不可由原生 NetworkPolicy 强制

NetworkPolicy 只能按 IP/CIDR + label selector 匹配,**无法表达 DNS 域名**。
allowlist 里的域名条目在本层被跳过并记 warn 日志(要精确钉死域名需 Cilium 等
DNS-aware CNI)。CIDR/IP 条目则被精确强制。这一限制在代码注释与本 changelog
中显式标注,留待后续(DNS-aware CNI 或 sidecar 代理)。

## 改动文件

- `apps/sandbox-manager/internal/provider/k8s/k8s.go`
  - 新增 `ensureNetworkPolicy`(幂等:已存在则 Update)+ `egressRules`。
  - `Create` 在写 binding 后、建 Pod 前调用 `ensureNetworkPolicy`(空清单=拒绝
    全部),保证 Pod 起来即受策略约束。
  - `Destroy` 删除沙箱 NetworkPolicy。
  - 新增 `netpolName(sid)="cocola-egress-"+sid` 辅助。
  - 引入 `k8s.io/api/networking/v1`、`k8s.io/apimachinery/pkg/util/intstr`、`net`。
- `apps/sandbox-manager/internal/provider/k8s/k8s_test.go`
  - 新增 3 个用例:空 allowlist=0 规则(拒绝全部)且 selector 命中沙箱;
    非空 allowlist 生成 DNS+集群+ipBlock 三段且域名被跳过、CIDR 精确;
    Destroy 删除 NetworkPolicy。

## 验证

`golang:1.25-alpine` 容器内(`GOWORK=off`、`-mod=mod`):

- `go build ./...` / `go vet ./internal/provider/k8s/` 通过
- `go test ./...` 全绿(k8s 包 18 个用例全部 PASS)
- `gofmt -l` 干净

## 不在本步范围

main.go 接线与部署物(Step 5)、真实集群验收(Step 6)。按沙箱定制 egress 需
orchestrator 传 `Networking`(目前 binder 的 Acquire 未传),属 Provider 边界外的
后续 plumbing,本步仅以 Provider 级默认兜底。
