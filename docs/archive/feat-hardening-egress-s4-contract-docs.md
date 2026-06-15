# docs(hardening S4):egress 契约与文档收口

## 背景

ADR-0009 egress 硬化第 4 步(末步),见 `docs/plan/hardening-sandbox-egress-allowlist.md` §4。
S1–S3 已把出网管控代码落地(编排注入 / Docker iptables / K8s NetworkPolicy),本步
统一对外契约注释与项目文档,使「nil=未配置全开,非 nil=基线放行」的语义有据可查。

## 改动

- `packages/proto/cocola/sandbox/v1/sandbox.proto`:为 `egress_allowlist = 6` 补充
  字段文档:区分 unset / set(含空)语义;说明基线(DNS+集群内 gateway)始终放行、
  CIDR/IP 加宽、域名需 DNS-aware CNI;并提示 proto3 无法在 wire 上区分「空 repeated」
  与「未设置」,故启用强制语义须显式下发 allowlist(编排层恒并入 gateway host,
  实践中恒非空)。**纯注释改动,不改 wire/codegen 行为**;生成桩沿用既有内容,
  目标机用 `make proto-gen`(buf)刷新注释即可(本机未装 buf,留待目标机)。
- `docs/adr/0009-agent-runtime-in-sandbox.md`:勾掉 Follow-ups 中
  「Enforce Networking.EgressAllowlist end-to-end」与「egress allowlist 尚未强制」
  两条;新增「实现进展(2026-06-15)egress 硬化」小节,串联 S1–S4 与统一语义。
- `README.md`:新增「安全:沙箱出网模型(egress)」章节:default-deny + 基线放行
  模型、Docker/K8s 两 provider 强制机制对照表、域名级放行的 Cilium `toFQDNs`
  扩展点、以及 nil/非 nil 配置语义说明。

## 验证

- 文档/注释类改动;`git diff` 复核三处改动语义准确、与 S1–S3 代码一致。
- proto 为纯注释改动,既有 Go/Python 生成桩功能不变,无需重新构建即正确。

## 后续(本计划范围外)

- proto 注释刷新到生成桩:目标机 `make proto-gen`(buf 本机被沙箱路径过滤拦截)。
- 端到端实跑:Linux+Docker / K8s 集群上验证「沙箱 curl gateway 通 / curl 公网拒」。
- 域名级精确放行:按需启用 DNS-aware CNI(Cilium `toFQDNs`),扩展点已在 Helm
  values 留好。
