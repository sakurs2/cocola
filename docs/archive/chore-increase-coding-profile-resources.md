# chore: 提高 coding Sandbox Profile 默认资源

- 变更时间：2026-07-20 13:13 (+08:00)

## 变更理由

预装 Code Server 标准插件和语言工具后，空 Workspace 打开编辑器的稳定内存约为 780 MiB，启动阶段峰值约为 1.3 GiB。原 `coding` Profile 的 `1 CPU / 2 GiB` 默认配额虽然可以运行，但同时启动 Agent、BasedPyright、Java、gopls 或 clangd 时可用余量偏小，启动阶段也出现过 CPU 限流。

## 变更内容

- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：将 `coding` Profile 的默认资源从 `1000m/2048Mi` 调整为 `2000m/4096Mi`。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`：用明确的 `2000m/4096Mi` 行为断言覆盖新的默认值。
- `deploy/sandbox-runtime/runtime-manifest.json`：同步 runtime 契约中的 coding 默认资源。
- `.env.example`、`deploy/sandbox-runtime/README.md`、`docs/configuration.md`、`docs/adr/0024-versioned-sandbox-runtime-contract.md`：同步配置示例和文档。
- 该调整仍允许运维通过 `COCOLA_OPENSANDBOX_DEFAULT_CPU/MEMORY` 覆盖，只影响 Sandbox Manager 重启后新创建的 Sandbox；现有 Pod 不会原地扩容。
