# feat: 沙箱后端收敛 —— OpenSandbox 主力化 + 退役 k8s provider

日期: 2026-06-28
关联: ADR-0014(新建)、ADR-0002 / ADR-0013(回链)、Plan
`docs/plan/sandbox-opensandbox-primary-drop-k8s.md`、task #15/#17(关闭)、#23/#24/#25

## 背景

OpenSandbox provider 已通过真 server 端到端验证(commit b224d3b)。决定将沙箱能力
收敛到以 OpenSandbox 为主力:保留 ADR-0002 可插拔架构,删除自建 k8s provider,
docker 保留为零配置兜底,停止其余沙箱方向(gVisor spike / Agent Substrate)投入。

## 变更

### 代码(删除)
- 删 `apps/sandbox-manager/internal/provider/k8s/k8s.go` 与 `k8s_test.go`(~1900 行)。
- `cmd/sandbox-manager/main.go`:移除 k8s import、工厂 `case k8s.ProviderName`、
  头注释里的 K8sGVisorProvider 行;工厂注释改为 docker(兜底)/ opensandbox(主力)。
- `go mod tidy`:移除 k8s.io/api、apimachinery、client-go 及一批间接依赖
  (go-restful、gnostic-models、json-iterator、gorilla/websocket 等)。

### 决策(文档)
- 新建 **ADR-0014**:OpenSandbox 主力 + k8s 退役 + docker 兜底;记录「运行时默认
  provider 维持 docker」的理由(opensandbox.New 强依赖 COCOLA_OPENSANDBOX_URL,
  不能作零配置默认)。
- ADR-0013:Status 索引由 Proposed 同步为 Accepted;Followups 补指向 0014。
- ADR-0002:加 superseded-note —— 抽象本身仍 Accepted,但 K8s+gVisor 具体后端预期
  由 0014 收敛。
- ADR README 索引:0013→Accepted,新增 0014 行。

### superseded 标注(不删除,保留历史 / 可逆)
- `deploy/k8s/README.md`、`deploy/helm/cocola-sandbox/Chart.yaml`、
  `docs/plan/m6-k8s-gvisor-provider.md`、
  `docs/plan/hardening-gvisor-spike-and-image-warmer.md`、
  `docs/runbook/m6-k8s-sandbox-acceptance.md` 顶部加 superseded 横幅。
  **YAML 部署文件本体不删**(超出「provider 代码」范围,属可逆资产)。

### 任务
- #15(gVisor spike)、#17(Agent Substrate 评估)→ 关闭(superseded by ADR-0014)。

## 不做(明确排除)
- 不改 `SandboxProvider` 接口(ADR-0002 铁律)。
- 不动 docker provider 行为,不改运行时默认 provider 值(仍 docker)。
- 不删 deploy/k8s、helm 的 YAML 本体。

## 验证
- `GOWORK=off go build ./...` 通过;`GOWORK=off go test ./...` 全绿,无回归。
- `grep -rn 'provider/k8s|k8s.ProviderName|k8s.New(' --include=*.go apps/` 无命中。
- `COCOLA_SANDBOX_PROVIDER=opensandbox` 工厂 case 仍在,可正常构造。
