# chore: dev startup and OpenSandbox provider cleanup

- 变更时间：2026-07-06 12:54 (+08:00)
- 关联提交：待提交

## 变更理由

项目启动路径经历过 Echo、hybrid、up-k8s、全容器等多轮演进，当前有效语义已经收敛为：
`make dev` 用于本地 dev 调试，`make prod` 用于全 Docker 正式模式。同时 sandbox 后端已经确定只内置
OpenSandbox，继续保留 DockerProvider 与 M1/M2 历史脚本会增加维护成本和误用风险。

## 变更内容

- Makefile / scripts：`make dev` 改为 dev 调试入口，吸收 OpenSandbox Kubernetes runtime
  准备流程；正式 Docker 栈保留为 `make prod` / `scripts/start.sh`。
- apps/sandbox-manager：删除 DockerProvider 实现，默认 provider 改为 OpenSandbox，并保留
  `SandboxProvider` factory registry 扩展点；Go 聚合命令对 sandbox-manager 显式使用
  `GOWORK=off`。
- deploy / README / .env.example：更新活跃启动文档与 compose 默认值，去除 DockerProvider /
  hybrid / up-k8s 等旧路径描述。
- deploy/k8s / deploy/helm/cocola-sandbox：删除已退役的内置 K8s provider / gVisor
  部署物，保留当前使用的 `deploy/opensandbox-k8s/`。
- .gitignore：忽略本地 `.codegraph/` 索引，避免误提交。

## 关键取舍 / 注意事项

- 历史 `docs/archive/` 与已接受 ADR 不做大规模重写，保留为项目演进记录。
- 本机当前缺少 Go 工具链，后续验证需要在有 `go` / `gofmt` 的环境中补跑 Go 格式化与测试。
