# 修复 `go run ./cmd/opensandbox-verify` 的 go.work 报错

日期: 2026-06-28

## 现象

在仓库根目录执行：

```
go run ./cmd/opensandbox-verify
```

报错：

```
directory cmd/opensandbox-verify is contained in a module that is not one of
the workspace modules listed in go.work. You can add the module to the
workspace using:
	go work use .
```

## 根因

`cmd/opensandbox-verify` 属于 `apps/sandbox-manager` 模块，而该模块被**刻意**排除在
根 `go.work` 之外（`go.work` 只 `use` 了 admin-api / gateway / db / go-common /
proto）。

排除的原因（见根 `Makefile` 注释）：sandbox-manager 依赖 grpc v1.81.1，强制
`go 1.25.0`，其依赖图一旦进入 workspace，会通过 workspace 级 MVS（最小版本选择）
把其它模块的依赖一起上抬，破坏它们的离线构建；而本机（受管 macOS）的 TLS 校验器会
拦截模块下载（x509 OSStatus -26276），导致一旦触发下载即失败。因此 sandbox-manager
走独立容器构建（`scripts/sandbox-build.sh`）。

## 验证过的错误修法（不要再走)

报错提示的 `go work use .`（把 sandbox-manager 加入 go.work）是**错误**修法：

- 加入后需把 `go.work` 的 go 指令升到 1.25.0，并触发 grpc/toolchain 下载，本机 TLS
  直接拦截；
- 即便绕过下载，实测会**回归**：admin-api / gateway / go-common 的离线构建全部因
  `grpc@v1.81.1 -> detectors/gcp` 的 TLS 失败而挂掉。通过 stash A/B 对比确认：原始
  `go.work` 能干净构建这三个模块，改后的 `go.work` 会把它们一起带崩。

结论：方法 B（入 workspace）会通过 workspace MVS 污染其它模块，已回退
`go.work` / `go.work.sum`。

## 正确修法

sandbox-manager 的任何 go 命令都必须**进入模块内、带 `GOWORK=off`** 运行。把这套
"咒语"固化成 Makefile 目标，避免每次手敲：

```
make verify-opensandbox
```

等价于：

```
cd apps/sandbox-manager && GOWORK=off go run ./cmd/opensandbox-verify
```

- 需要环境变量 `COCOLA_OPENSANDBOX_URL`（启用鉴权时再加
  `COCOLA_OPENSANDBOX_API_KEY`）；
- 额外参数通过 `ARGS=` 透传，例如：
  `make verify-opensandbox ARGS="-keep -timeout 10m"`。

## 验证

- 未设 `COCOLA_OPENSANDBOX_URL` 时 `make verify-opensandbox` 能正常编译（无 workspace
  冲突）并 fail-fast，打印 base URL 缺失的提示，证明命令路由正确。
- 真 server 端到端验证留待合规环境（见 #22）。

## 改动

- `Makefile`: 新增 `verify-opensandbox` 目标，并加入 `.PHONY`。
