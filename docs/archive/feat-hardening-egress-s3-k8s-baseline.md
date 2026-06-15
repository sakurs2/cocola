# feat(hardening S3):K8s provider egress 收口(空 allowlist 改为基线放行)

## 背景

ADR-0009 egress 硬化第 3 步(见 `docs/plan/hardening-sandbox-egress-allowlist.md` §3.4)。
S1 让编排层透传 `Networking`,S2 在 Docker provider 落地 iptables+ipset 防火墙并
确立了 nil / 非 nil 语义。本步把 K8s provider 对齐到同一语义,堵上「空 allowlist =
deny-all 会连 gateway 一并切断」的不一致。

K8s 侧继续复用原生 `NetworkPolicy`(IP/CIDR + label selector),不引第三方组件,
符合「复用开源、避免造轮子」。域名精确放行需 DNS-aware CNI,留作扩展点(见下)。

## 改动

- `apps/sandbox-manager/internal/provider/k8s/k8s.go`:
  - `Create()`:按 nil / 非 nil 判定,与 Docker provider 一致。**nil allowlist** =
    「未配置 egress 策略」(遗留全开,不创建任何 NetworkPolicy);**非 nil**(含空)=
    启用防火墙,基线放行 DNS + 集群内 llm-gateway,其余默认 DROP。
  - `egressRules()`:删除 `len(allowlist)==0 -> nil(deny all)` 分支,基线(DNS UDP/TCP
    53 + 集群内 NamespaceSelector{})**始终返回**,空 allowlist 不再切断 gateway;
    CIDR/裸 IP 仍作为 ipBlock peer 追加,域名 entry 仍 skip + `slog.Warn`。
  - 更新 `ensureNetworkPolicy` / `egressRules` 文档注释,明确「安全默认 = 基线,而非
    全 deny」,并指向 Helm values 中的 Cilium `toFQDNs` 扩展点。
- `apps/sandbox-manager/internal/provider/k8s/k8s_test.go`:替换旧的
  `TestCreate_EmptyAllowlistDeniesAllEgress`,新增:
  - `TestCreate_NilAllowlistCreatesNoPolicy`:nil → 不创建 NetworkPolicy。
  - `TestCreate_EmptyAllowlistAppliesBaseline`:空(非 nil)→ 2 条基线规则(DNS+集群内)、
    无 ipBlock peer、PolicyType=Egress、按 sandbox-id 选 Pod。
  - 既有 `TestCreate_AllowlistAddsDNSClusterAndCIDR`(基线 + ipBlock)保持不变,验证
    非空路径行为未回归。
- `deploy/helm/cocola-sandbox/values.yaml`:新增 `sandbox.egressAllowlist`(默认 `""`,
  即仅基线);补充注释说明因 `llmBaseURL` 恒被设置,编排层总会并入 gateway host,故
  per-sandbox NetworkPolicy 总会以安全基线创建;并给出 Cilium `CiliumNetworkPolicy`
  `toFQDNs` 的域名精确放行扩展点示例(本 chart 不强制引入 Cilium)。
- `deploy/helm/cocola-sandbox/templates/sandbox-manager.yaml`:注入
  `COCOLA_SANDBOX_EGRESS_ALLOWLIST` 环境变量(取自 `sandbox.egressAllowlist`)。

## 验证

- `apps/sandbox-manager` 下 `GOWORK=off go build ./...` 通过;`go vet` 干净;
  `gofmt -l internal/ cmd/` 无输出;`GOWORK=off go test ./...` 全绿(含新增/改写的
  k8s 单测)。
- Helm 本机未安装,模板沿用与同文件其余 env 一致的 `{{ ... | quote }}` 语法,无新语法风险。

## 后续

- S4:proto 注释更新(nil / 非 nil 语义)、ADR-0009 Follow-ups 勾选与进度补记、
  README 安全章节(egress 模型)、changelog 与提交。
- 域名级精确放行:vanilla CNI 不支持,需启用 DNS-aware CNI(Cilium `toFQDNs`),
  扩展点已在 values 注释中留好;本计划不强制引入。
