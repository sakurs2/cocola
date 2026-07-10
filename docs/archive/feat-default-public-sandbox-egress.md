# feat: Sandbox 默认允许访问公网

- 变更时间：2026-07-11 00:41 (+08:00)

## 变更理由

远程 MCP 配置可以正常下发到 Agent SDK，但默认 egress policy 只允许模型网关，导致任意远程 MCP 域名在 sandbox 内被 DNS 代理拒绝。要求管理员为每个 MCP 重启服务并手动维护全局 allowlist，违背 Cocola 简单易用的产品目标。

## 变更内容

- `apps/sandbox-manager/internal/orchestrator/networking.go`：未配置 `COCOLA_SANDBOX_EGRESS_ALLOWLIST` 时返回 nil policy，保留公网访问；显式配置后继续自动加入模型网关并启用 default-deny。
- `apps/sandbox-manager/internal/orchestrator/networking_test.go`：覆盖“空配置默认开放”和“显式配置自动加入网关”语义。
- `README.md`、`deploy/sandbox-runtime/README.md`、`deploy/docker-compose/docker-compose.full.yml`、`docs/adr/0009-agent-runtime-in-sandbox.md`：记录默认策略调整、生产收紧方式和 OpenSandbox DNS+nft enforcement 边界。

## 关键取舍

- 不使用 `*` 规则，不修改 OpenSandbox，也不删除 allowlist 能力。
- 默认优先远程 MCP、网页和包管理的开箱即用；生产环境可通过一个环境变量恢复严格出网控制。
- 网络策略在 sandbox 创建时确定，现有 sandbox 需重建后才能应用新默认值。
