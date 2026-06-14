# docs(hardening):落地沙箱 egress allowlist 强制 Plan

## 背景

ADR-0009 把「egress allowlist 尚未强制(沙箱目前可任意出网)」列为上线前必做的
硬化项。Route A 下大脑(Claude Code CLI)与凭据都在运行不可信用户代码的沙箱容器内,
沙箱若可任意出网,等于把宿主 / 内网 / 公网全部敞开给用户代码,是最高优先级安全缺口。
本次先产出落地方案(Plan-before-coding),尚未动业务代码。

## 现状诊断(已核对代码)

三处不一致导致缺口:

- **K8s provider**(`k8s.go:242` `ensureNetworkPolicy`):空/nil allowlist →
  NetworkPolicy 零规则 = deny-all;非空 → 放行 DNS+集群内+逐条 CIDR,但域名条目被
  skip。deny-all 默认反而切断 gateway 使 Route A 不可用。
- **Docker provider**(`docker.go:172`):仅「非 nil 空切片 → NetworkMode=none」;
  nil → 全开;非空(域名/CIDR)allowlist 被静默忽略,仍全开 = 等同未强制。
- **生产编排**(`binder.go:168` `Binder.Create`):构造 SandboxSpec 时根本不传
  Networking。这是 ADR-0009 缺口的根因。

核心张力:Route A 沙箱必须能回连 llm-gateway 出网并需 DNS,故「硬化」不能是裸
deny-all,而是 default-deny + 始终放行 {DNS, llm-gateway} + 配置化 allowlist。

## 改动

- 新增 `docs/plan/hardening-sandbox-egress-allowlist.md`(113 行,待评审):
  - 统一默认姿态:基线永远放行 DNS+llm-gateway;空 allowlist=仅基线(新安全默认,
    取代 Docker 全开与 K8s 裸 deny-all 切断 gateway);非空=基线+逐条放行。
  - 配置化注入:新增 env `COCOLA_SANDBOX_EGRESS_ALLOWLIST`(逗号分隔域名/CIDR),
    gateway host 从 `COCOLA_SANDBOX_LLM_BASE_URL` 解析自动并入基线,经 Binder 透传。
  - Docker 实装复用业界成熟 iptables+ipset init-firewall 模式(与 Anthropic 官方
    Claude Code devcontainer init-firewall 同源),CAP_NET_ADMIN init 后 drop;
    空 allowlist 改用 firewall 基线而非 NetworkMode=none。
  - K8s 收口:egressRules 默认并入 DNS+gateway 基线;域名精确放行留 Cilium toFQDNs
    扩展点与文档,不强制引入。
  - proto 无需改(`egress_allowlist` 字段已存在),仅补注释/ADR 语义说明。
  - 分 4 步交付:S1 编排层注入 → S2 Docker 强制 → S3 K8s 收口 → S4 契约/文档。

## 验证

- 仅新增 Plan 文档与本 changelog,无代码/部署改动。
- 文件无 tab,prettier 友好。

## 后续

- 评审通过后按 S1→S2(单机主路径,缺口最大)→S3→S4 实施,每步配单测与 changelog。
