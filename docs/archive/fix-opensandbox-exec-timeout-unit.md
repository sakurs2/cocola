# fix: 修复 OpenSandbox Exec 超时单位

- 变更时间：2026-07-10 17:40 (+08:00)

## 变更理由

`SandboxService.Exec.timeout_secs` 使用秒，而 OpenSandbox execd `/command.timeout` 使用毫秒。provider 原样转发数值后，所有带超时的 sandbox 命令都会比预期提前一千倍终止，例如 45 秒实际变成 45 毫秒。

## 变更内容

- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：向 execd 发请求时将秒转换为毫秒；本地 context 仍按秒控制整体调用。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`：断言 45 秒被序列化为 `45000` 毫秒。
- 该修复属于通用 Exec 语义修正，与具体业务调用方无关。
