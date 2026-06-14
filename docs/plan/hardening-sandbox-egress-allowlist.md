# Plan: 沙箱 egress allowlist 强制 —— 兑现 ADR-0009 的上线前硬化项

> 状态:规划中(2026-06-14)。本文是落地方案,尚未动代码。

## 1. 目标与动机

ADR-0009 把「egress allowlist 尚未强制(沙箱目前可任意出网)」列为**上线前必做**的
硬化项。Route A 下大脑(Claude Code CLI)与凭据都在沙箱容器内运行不可信用户代码,
若沙箱可任意出网,等于把宿主网络 / 内网 / 公网全部敞开给用户代码,是最高优先级的
安全缺口。本计划把沙箱出网从「默认全开」收敛为「默认拒绝 + 显式放行」。

**铁律(ADR-0002):** 仅在 `provider.SandboxProvider` 各实现与编排层内做改动,
对 service 层 gRPC 契约保持兼容(`egress_allowlist` 字段已存在)。

## 2. 现状(三处不一致,已核对代码)

| 路径 | 当前行为 | 问题 |
| --- | --- | --- |
| **K8s provider**(`k8s.go:242`,`ensureNetworkPolicy`) | 空/nil allowlist → NetworkPolicy 仅 `Egress` 类型零规则 = **deny-all**;非空 → 放行 DNS + 集群内(够到 llm-gateway)+ 逐条 CIDR ipBlock | 已基本强制;但**域名条目被 skip**(vanilla CNI 无法表达),且 deny-all 默认会切断 gateway 使 Route A 不可用 |
| **Docker provider**(`docker.go:172`) | 仅「非 nil 空切片 → `NetworkMode=none`」一种;nil → **全开**;**非空(域名/CIDR)allowlist 被静默忽略,仍全开** | 等同未强制;且 `none` 会切断 gateway |
| **生产编排**(`binder.go:168`,`Binder.Create`) | 构造 `SandboxSpec` 时**根本不传 `Networking`** | 所以实跑沙箱:K8s=nil(deny-all,反而切断 gateway)、Docker=nil(**全开**)。这就是 ADR-0009 缺口的根因 |

**核心张力:** Route A 沙箱**必须**能回连 llm-gateway 出网(Docker 经
`host.docker.internal:18091`,K8s 经集群内 Service),并需 DNS 解析。因此「硬化」
不能是裸 deny-all,而是 **default-deny + 始终放行 {DNS, llm-gateway} + 配置化 allowlist**。

## 3. 设计

### 3.1 统一默认姿态(语义重定义)

把 `EgressAllowlist` 的语义从「空=全断 / nil=全开」改为三方一致的**安全默认**:

- **基线放行(永远)**:DNS(53/udp+tcp)、llm-gateway endpoint。这是 Route A 的
  生命线,任何姿态下都放行。
- **空 allowlist(默认)**:= 仅基线放行,其余全拒。这是**新的安全默认**,取代
  Docker 的「全开」与 K8s 裸 deny-all 切断 gateway 的旧行为。
- **非空 allowlist**:基线 + 逐条放行(CIDR/IP 精确;域名尽力而为,见 3.4)。

### 3.2 配置化注入(编排层)

`Binder.Create` 增传 `Networking`,allowlist 来自启动配置:

- 新增 env `COCOLA_SANDBOX_EGRESS_ALLOWLIST`(逗号分隔域名/CIDR,默认空)。
- gateway endpoint 从已有的 `COCOLA_SANDBOX_LLM_BASE_URL` 解析 host,自动并入基线放行
  (无需用户重复配置)。
- 配置在 composition root 读入,经 `Binder` 透传给 provider,保持 provider 无状态。

### 3.3 Docker provider 实装(复用开源 init-firewall 思路)

Docker 无 NetworkPolicy,复用业界成熟的 **iptables + ipset 出站 allowlist** 模式
(与 Anthropic 官方 Claude Code devcontainer 的 `init-firewall` 设计同源,和 cocola
Route A「沙箱内跑 Claude Code」天然契合;另参 GitHub `harden-runner` 的出站收敛思路):

- **方案 A(推荐)**:沙箱容器以 `CAP_NET_ADMIN` 启动,entrypoint 先跑 init-firewall:
  default DROP output,放行 loopback/已建立连接/DNS,解析 allowlist 域名 + gateway host
  写入 ipset,放行目标集。随后再 drop NET_ADMIN(或用单独 init 容器)避免用户代码改规则。
- **方案 B(回退)**:沙箱接入一个 default-deny 的自定义 bridge 网络,sandbox-manager
  在 host 侧维护放行规则。DooD 拓扑下较复杂,作为 A 不可行时的备选。
- **空 allowlist 不再用 `NetworkMode=none`**:改为「装上仅放行基线的 firewall」,
  从而既默认安全又不切断 gateway。

> 取舍:方案 A 把 enforcement 放在沙箱镜像启动脚本,改动集中在 `sandbox-runtime`
> 镜像 + Docker provider 的 HostConfig(加 cap、传 allowlist+gateway 经 env),
> 不引第三方守护进程,最契合「复用开源、避免造轮子」。

### 3.4 K8s provider 收口

- deny-all 默认改为 3.1 的安全默认:基线放行 DNS + llm-gateway(集群内 Service)
  始终生效,空 allowlist 不再切断 gateway。
- **域名 allowlist**:vanilla CNI 仍无法表达,维持 skip + 告警的现状;在 values 中
  提供可选开关说明:启用 DNS-aware CNI(如 Cilium `CiliumNetworkPolicy` 的
  `toFQDNs`)后方可精确放行域名。本计划不强制引入 Cilium,只留扩展点与文档。

### 3.5 proto / 契约

`egress_allowlist` 字段已存在,无需改 proto。仅在 `.proto` 注释与 ADR 补充新语义
(空=仅基线放行,而非全断/全开)。

## 4. 分步交付

- **S1 — 编排层注入**:`Binder.Create` 传 `Networking`;新增 `COCOLA_SANDBOX_EGRESS_ALLOWLIST`
  解析 + gateway host 并入基线;composition root 透传。单测覆盖配置解析与默认值。
- **S2 — Docker 强制**:`sandbox-runtime` 镜像加 init-firewall 脚本;Docker provider
  HostConfig 加 `CAP_NET_ADMIN` + 经 env 下发 allowlist/gateway;空 allowlist 走
  firewall 基线而非 `none`。单测(fake daemon)+ 本地 compose 端到端验证(沙箱能连
  gateway、不能连任意外网)。
- **S3 — K8s 收口**:`egressRules` 默认并入 DNS + llm-gateway 基线;补域名/Cilium
  扩展点文档与 values 注释;补单测(空 allowlist 仍放行 gateway)。
- **S4 — 契约/文档**:`.proto` 注释更新;ADR-0009 Follow-ups 勾掉该项并补「实现进展」;
  README 安全章节补一段 egress 模型;每步配 `docs/archive/` changelog。

> 步骤可独立交付:S1 是地基;S2/S3 是两个 provider 的并行强制;S4 收口文档。
> 建议先 S1+S2(单机路径是当前 demo/自托管主路径,缺口最大),再 S3,最后 S4。

## 5. 测试与验收

- **单测**:配置解析(默认/逗号分隔/非法项跳过)、gateway host 自动并入、Docker
  HostConfig 含 cap+env、K8s egressRules 含基线、空 allowlist 不切 gateway。
- **端到端(compose.full)**:沙箱内 `curl llm-gateway` 通、`curl 任意公网域名` 被拒、
  DNS 可解析、Route A 全链路对话正常。
- **回归**:既有 K8s 单测(deny-all、CIDR、域名 skip、Destroy 删 netpol)保持绿。
- **验收**:默认配置(空 allowlist)下,沙箱**仅能**访问 DNS + llm-gateway,任意其他
  出网被拒;ADR-0009 该 Follow-up 标记完成。

## 6. 风险与缓解

- **沙箱内 firewall 被用户代码改写**:用户代码以非 root 跑,且 NET_ADMIN 在 init 后
  drop / 用独立 init 容器,规则在用户代码可干预前已固化。
- **DooD 网络拓扑**:Docker 沙箱落 host bridge,firewall 在容器内生效不依赖 host 网络
  改动,拓扑无关;方案 B(host 侧规则)仅在 A 不可行时启用。
- **gateway host 解析漂移**(`host.docker.internal` / Service IP 变动):放行按域名/Service
  而非固定 IP;ipset 在 init 时解析,resume 重建沙箱即重新解析。
- **过度收敛误伤**:allowlist 可经 env 增量放行;基线只含 DNS+gateway,最小必要集。
