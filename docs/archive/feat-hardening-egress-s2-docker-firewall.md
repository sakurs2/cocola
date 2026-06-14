# feat(hardening S2):Docker provider egress 强制(iptables+ipset 防火墙)

## 背景

ADR-0009 egress 硬化第 2 步(见 `docs/plan/hardening-sandbox-egress-allowlist.md`)。
S1 已让编排层把 `Networking` 透传进 provider;本步在 Docker provider 落地实际的
出网强制,堵上「Docker 全开 / 非空 allowlist 被静默忽略」缺口。

复用业界成熟的 **iptables + ipset 默认 DROP + allowlist** 模式(与 Anthropic 官方
Claude Code devcontainer 的 init-firewall 同源,且 Route A 本就在沙箱内跑 Claude
Code,天然契合),不引第三方守护进程,符合「复用开源、避免造轮子」。

## 改动

- 新增 `deploy/sandbox-runtime/init-firewall.sh`:root 启动期运行一次的防火墙脚本。
  fail-closed 顺序:先放行 loopback/established/DNS,再把 OUTPUT 默认翻为 DROP,最后
  解析 allowlist(DNS 已放行)写入 ipset `cocola-allow`。域名 best-effort 解析,CIDR/
  裸 IP 精确放行。空 allowlist 仍装 DNS 基线(secure by default,绝不全开,绝不用
  NetworkMode=none 切断 gateway)。
- 新增 `deploy/sandbox-runtime/firewall-entrypoint.sh`:仅当 `COCOLA_EGRESS_ALLOWLIST`
  存在时以 root 跑 init-firewall,然后 keep-alive。未设置则退回旧 keep-alive 行为。
- `deploy/sandbox-runtime/Dockerfile`:base 加 `iptables ipset`;COPY 两个脚本;容器
  主进程改为以 **root** 跑 firewall-entrypoint(装防火墙需 NET_ADMIN)。注明这不是
  提权:用户/agent 代码从不作为主进程运行,而是经 exec 进入,由 provider 钉到非 root
  的 cocola 用户。
- `apps/sandbox-manager/internal/provider/docker/docker.go`:
  - 新增 `applyEgressPolicy()` 纯函数(可单测):nil allowlist→旧全开 keep-alive;
    非 nil→加 `CAP_NET_ADMIN`、映射 `host.docker.internal:host-gateway`(供容器内解析
    gateway)、经 `COCOLA_EGRESS_ALLOWLIST` 下发、命令切到 firewall-entrypoint。
  - `applyEgressPolicy` 同时返回 exec 用户:防火墙路径钉非 root 的 `cocola`,旧镜像
    路径返回空(用镜像默认用户,不破坏 alpine/M1)。该用户经 `cocola.exec_user` label
    持久化,使跨副本从 label 重建 record 时仍能正确钉用户。`Exec` 用 `rec.execUser`。
  - 删除旧的 `NetworkMode=none` 逻辑。
- `deploy/docker-compose/docker-compose.full.yml`:sandbox-manager 增 `COCOLA_SANDBOX_
  LLM_BASE_URL`(自动并入 gateway host)与 `COCOLA_SANDBOX_EGRESS_ALLOWLIST` 开关。
- `deploy/sandbox-runtime/README.md`:补 egress firewall 章节与目录说明。

## 验证

- `GOWORK=off go build ./...` 通过;`go vet` 干净;`gofmt -l` 无输出;`bash -n` 两个脚本通过。
- 新增 docker provider 单测 3 例:nil→全开 keep-alive 无 cap/env/NetworkMode;空
  (非 nil)→firewall 入口 + NET_ADMIN + 空 env,且非 NetworkMode=none;非空→firewall +
  NET_ADMIN + ExtraHosts + 正确 allowlist env。
- compose.full YAML 校验通过;既有 docker/orchestrator 单测全绿。

## 后续

- S3:K8s provider egressRules 默认并入 DNS+gateway 基线。
- 端到端验证(沙箱内 curl gateway 通 / curl 任意公网被拒)需 Linux+Docker 环境实跑,
  随 S2/S3 合并后在目标机执行(本机为 macOS,容器化构建路径见 Makefile)。
