# Changelog: 按需分配端到端验收 + 修复非池化 create 缺省 resourceLimits 导致 422

日期: 2026-06-28
关联: task #28、ADR-0015、ADR-0008、ADR-0002

## 背景
ADR-0015 把「按需冷启动分配」定为默认主路径。本次实跑验证该路径在真 OpenSandbox
server 上闭环,过程中发现并修复了一个会让 /v1/chat 必然 422 的真实 bug。

## 发现的 bug
binder 的按需慢路径用 `AcquireSpec` → `provider.SandboxSpec` 调 `Create`,而 SandboxSpec
不带 Resources,导致 `mapResources` 返回空 → 请求体无 `resourceLimits`。OpenSandbox 对
非池化(无 poolRef)的 create 强制要求 resourceLimits,服务端返回:
`422 "resourceLimits is required when poolRef is not provided."`。
opensandbox-verify harness 因为带了 `-cpu/-mem` flag 才一直没暴露此问题;真正的
binder→provider 路径(即 chat 走的路)从不设 Resources,所以线上必崩。这正是真 server
集成测试相对单测的价值。

## 改动
### apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go
- 新增缺省常量 `defaultCPU="500m"`、`defaultMemory="512Mi"`。
- `mapResources` 改为:Resources 为零时回退到缺省 floor,而非返回空 map;
  可经 `COCOLA_OPENSANDBOX_DEFAULT_CPU` / `COCOLA_OPENSANDBOX_DEFAULT_MEMORY` 覆盖
  (原始 resourceLimits 字符串,如 "500m"/"512Mi")。
- 新增 `envOr(key, def)` 小工具。
- 效果:provider 永不向服务端发出缺 resourceLimits 的非池化 create,按需路径不再 422。

### apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go
- `TestMapResources` 改为断言「零 Resources → 缺省 floor」(旧断言「→ 空」已随契约失效)。
- 新增 `TestMapResources_EnvOverride` 验证环境变量覆盖缺省值。

### apps/sandbox-manager/internal/orchestrator/binder_opensandbox_live_test.go(新增)
- 真 server 集成测试,`COCOLA_OPENSANDBOX_URL` 未设则 `t.Skip`(不影响离线套件)。
- 用 fake KV + 真 opensandbox provider 装配 binder,跑 chat 实际驱动的按需路径:
  - 断言主后端**未实现** `provider.Adopter`(印证 ADR-0015:warm pool 在此只会空转)。
  - 慢路径:`AcquireWithOutcome` → 加锁 → `Create`(mapVolumes 挂双卷) → bind,Reused=false。
  - 快路径:同 session 二次 Acquire 复用同一箱,Reused=true 且 ID 一致。
  - 轮询 Health 至 healthy 后,在绑定箱内 Exec,断言三处挂载点
    (/data/userdata/<uid>、/workspace/<sid>、/home/cocola/.claude)均可见且 exit=0。

## 验收
- 真 server:`COCOLA_OPENSANDBOX_URL=http://localhost:8090/v1 go test ./internal/orchestrator/
  -run TestLive_OnDemandAllocation_OpenSandbox` → PASS(slow-create+mount → fast-reuse →
  exec 看到双卷)。
- 离线全量:`GOWORK=off go test ./...` 绿(live 测试自动 skip)。
- gofmt 干净。

## 不含
- warm pool 行为变更(仍默认关闭,见 ADR-0015)。
- gateway/agent-runtime 进程级在线联调(受「不在沙箱内起监听端口」约束;按需分配的实质
  链路 binder→provider→真 server 已由 live 测试覆盖)。
