# feat: 建立 Sandbox Runtime 生命周期与能力契约

- 变更时间：2026-07-19 21:47 (+08:00)

## 变更理由

Sandbox Runtime 此前缺少版本化能力契约；直接 Docker 使用镜像 CMD，而
OpenSandbox 会覆盖 CMD 并在 provider 生成的 shell 中独立启动 Code Server。两条链路
对 Workspace、可选服务、重启和失败状态的语义不同，继续加入 Browser、Artifact 或
MCP 能力会进一步放大重复实现和恢复不一致。

## 变更内容

- `deploy/sandbox-runtime`：新增 schema v1 Runtime Manifest、统一
  `runtime-entrypoint.sh`、supervisord 服务托管和 guest CLI
  `cocola-sandbox info/service/workspace`；Code Server 改为 supervisor 单点托管；
  supervisor 配置与 launcher 保持 root-owned，并固定降权到 `cocola` 用户。启动失败最多
  重试 3 次；服务进入 RUNNING 后若崩溃则保持 EXITED，避免无退避的无限重启循环。
- `apps/sandbox-manager/internal/provider/opensandbox`：OpenSandbox 仅准备 Session
  目录与链接，随后进入统一 Runtime Entrypoint；新增 `coding|minimal` Profile 校验、
  平台配置防覆盖和 Profile 资源默认值。
- `apps/cli`、`scripts/run-stack.sh`、`.env.example`：正式部署与本地开发统一注入
  默认 `coding` Profile，保留显式资源和 Code Server 运维覆盖。
- `apps/agent-runtime/tests`、`scripts/sandbox-runtime-verify.sh`：覆盖 guest CLI、
  Profile 优先级、Workspace contract 和 supervisor 服务就绪状态。
- `docs/adr/0024-versioned-sandbox-runtime-contract.md`、运行时 README 与配置文档：
  记录一期边界；本期不引入 Jupyter、单 Sandbox observe、Browser daemon、HTML 发布
  或 Sandbox MCP。
