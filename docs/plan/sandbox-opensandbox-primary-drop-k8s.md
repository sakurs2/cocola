# Plan: 沙箱后端收敛 —— OpenSandbox 主力化 + 删除 k8s provider

日期: 2026-06-28
关联: ADR-0002(可插拔铁律,保留)、ADR-0013(OpenSandbox 可插拔后端)、task #15/#17
状态: Draft(待评审后执行)

## 决策(用户拍板)

1. **沙箱仍是可插拔后端架构(ADR-0002 不变)**:`SandboxProvider` 8 方法接口、
   工厂选择、自注册机制全部保留。
2. **OpenSandbox 为主力后端**:作为生产推荐 / 默认投入方向。
3. **provider 代码:删除 k8s,保留 docker**。docker 零配置、是本地单进程调试与
   降级兜底;k8s(client-go 重依赖,~1900 行含测试)是「其余沙箱能力」的大头,删除。
4. **停止其余沙箱方向投入**:#15(gVisor spike)、#17(Agent Substrate 评估)关闭。

## 关键约束 / 已知事实

- `opensandbox.New()` **强依赖** `COCOLA_OPENSANDBOX_URL`,缺失即报错;
  `docker.New()` 零配置可起。
- 因此**运行时默认 provider 维持 docker**(`COCOLA_SANDBOX_PROVIDER` 默认值不改),
  避免无 env 的本地 `go run` / CI 直接启动失败。「主力 OpenSandbox」体现在 ADR 定位、
  文档推荐与生产部署默认,而非把一个强依赖外部 server 的后端钉成进程默认值。
- k8s provider 在 Go 代码里**只被 `cmd/sandbox-manager/main.go` 工厂引用一处**
  (`k8s.ProviderName` / `k8s.New`),删除点收敛、低风险。

## 影响面与处置

### A. 删除(本次执行)
- `apps/sandbox-manager/internal/provider/k8s/k8s.go`
- `apps/sandbox-manager/internal/provider/k8s/k8s_test.go`
- `cmd/sandbox-manager/main.go`:移除 `k8s` import、工厂 `case k8s.ProviderName`、
  注释里的 K8sGVisorProvider 行。
- 若删 k8s 后 `go.mod` 出现仅 k8s 使用的 client-go 等依赖,`go mod tidy` 清理。

### B. 改定位(本次执行,文档/ADR)
- 新建 **ADR-0014**:在 ADR-0002 可插拔前提下,确立 OpenSandbox 为主力后端、
  docker 降级为本地/兜底、k8s provider 退场;supersede ADR-0013 中"仅作可插拔后端、
  暂不表态主次"的措辞,引用真 server e2e 结论(commit b224d3b)。
- ADR-0002:加 superseded-note 指向 0014(k8s+gVisor 这一具体后端预期作废,
  抽象本身仍 Accepted)。
- ADR-0013:Followups 补一句指向 0014 的主力化决策。

### C. 标记 superseded(本次执行,不删除 —— 可逆 / 保留历史)
- `deploy/k8s/*`、`deploy/helm/cocola-sandbox/*`:不在「provider 代码」范围,
  仅在各自 README / Chart 顶部加 superseded 提示,指向 ADR-0014。**不删文件**
  (大规模删 deploy 超出用户指令范围,且属可逆资产)。
- `docs/plan/m6-k8s-gvisor-provider.md`、`hardening-gvisor-spike-and-image-warmer.md`:
  顶部加 superseded 横幅。
- `docs/runbook/m6-k8s-sandbox-acceptance.md`:同上。

### D. 任务收口
- #15 gVisor spike → 取消(superseded by 本计划)。
- #17 Agent Substrate 评估 → 取消(收敛到单一主力后端,不再并行评估外部 substrate)。

## 不做(明确排除)
- 不改 `SandboxProvider` 接口(ADR-0002 铁律)。
- 不动 docker provider 行为。
- 不改运行时默认 provider 值(仍 docker,理由见上)。
- 不删 `deploy/k8s` / helm 的 YAML 文件本体(仅标注)。

## 执行步骤
1. 删 k8s provider 两文件 + main.go 接线;`GOWORK=off go build ./... && go test ./...` 绿。
2. `go mod tidy`(若有悬挂依赖),复核 diff。
3. 写 ADR-0014 + 回链 0002/0013。
4. 给 deploy/k8s、helm、相关 plan/runbook 加 superseded 横幅。
5. 写 docs/archive changelog。
6. review-before-commit(排除 .claude/),不带 --no-verify 提交。
7. 关闭 #15/#17。

## 验收
- `go build ./... && go test ./...` 全绿,无 k8s 残留引用(`grep -rn 'provider/k8s'` 仅命中文档历史)。
- `COCOLA_SANDBOX_PROVIDER=opensandbox` 仍可经工厂构造(已有 case)。
- ADR-0014 落地、0002/0013 回链一致。
