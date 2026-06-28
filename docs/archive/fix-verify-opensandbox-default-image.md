# 修复 harness 创建沙箱 422:缺少 image

日期: 2026-06-28

## 现象

`make verify-opensandbox-full`(或直接跑 harness)在 Create 阶段报 422:

```
status 422: {"detail":[{"type":"value_error","loc":["body"],
"msg":"Value error, Exactly one of image or snapshotId must be provided.",
"input":{"metadata":{...}}}]}
```

server 收到的 `input` 只有 metadata,没有 image。

## 根因

harness 的 `-image` flag 默认值是空串 `""`,provider 的 Create 只有在
`spec.Image != ""` 时才会把 image 写进请求体。于是默认跑法发出的请求**不带 image**。

最初 harness 的注释假设"server 有默认 image",但 OpenSandbox 实际要求 create
请求必须**恰好提供 image 或 snapshotId 之一**,服务端没有默认 image。

## 修复

把 harness 的默认 image 改成真实镜像 `python:3.12-slim`:

- 新增包级常量 `defaultImage = "python:3.12-slim"`;
- `-image` flag 默认值由 `""` 改为 `defaultImage`;
- 同步更新 usage / flag 注释(去掉"server default image"的错误说法)。

仍可用 `-image <其它镜像>` 覆盖。

## 验证

- `gofmt` 干净、`go vet` 无输出、`GOWORK=off go build` 通过。
- `go run ./cmd/opensandbox-verify -h` 显示 `-image` 默认值为 `python:3.12-slim`。
- 真 server 端到端跑在合规环境执行(见 #22)。

## 改动

- `apps/sandbox-manager/cmd/opensandbox-verify/main.go`:新增 `defaultImage` 常量,
  `-image` 默认值改为该常量,更新注释。
