# feat(gateway): P1a Step2 — MinIO objstore client

附件 P1a(ADR-0017)第 2 步:为 gateway 新增对象存储客户端,作为附件"真源"
写入侧的落桶能力与 agent-runtime 代 pull 的对端。本步只落客户端与单测,
尚未接入 `/v1/chat`(Step3)。

## 背景

P1a 的送达模型:gateway 始终把每个附件 PutObject 到 MinIO(真源),再按可配置
阈值分流——小文件内联推送,大文件仅带 oss_key 由 agent-runtime 代 pull。本步
提供 gateway 侧的 MinIO 封装。

## 改动

- 新增 `apps/gateway/internal/objstore`:
  - `Store` 接口:`Put`/`Get`/`Health`,窄接口便于 chat handler 用 fake 单测。
  - `Client`:`github.com/minio/minio-go/v7` 封装,`New` 仅构造(懒连接),
    `Health` 通过 `BucketExists` 探活。
  - `Config` + `ConfigFromEnv`:读取 `COCOLA_MINIO_ENDPOINT` /
    `COCOLA_MINIO_ACCESS_KEY` / `COCOLA_MINIO_SECRET_KEY` /
    `COCOLA_MINIO_BUCKET` / `COCOLA_MINIO_USE_SSL`。SecretKey 走 `config.SecretFromEnv`
    的 `_FILE` 间接(ADR-0008,Vault-ready)。`UseSSL` 仅当值精确为 `"1"` 才开。
  - `Enabled()`:endpoint+bucket 均配置时为真;否则 gateway 回落 P0 纯内联路径
    (灰度开关,ADR-0017 P1a 风险/回退)。
- `apps/gateway/go.mod` / `go.sum`:新增 `minio-go/v7 v7.0.80`(direct)及其传递依赖。

## 单测

- `ConfigFromEnv` 读全字段 / `UseSSL` 默认 false 且仅 `"1"` 开 / `_FILE` 间接优先。
- `Enabled()` 真值表。
- `New` 未配置报错、已配置成功且 bucket 正确。
- fakeStore 满足 `Store` 接口并可 roundtrip(锁定接口契约,供 Step3 复用)。

## 校验

因公司 TLS 拦截导致宿主机 `go get`/`go build` 拉取缺失模块失败,构建与测试均在
`golang:1.24` 容器内经 byted 代理执行:

- `go build ./internal/objstore/...` → BUILD_OK
- `go vet ./internal/objstore/...` → VET_OK
- `go test ./internal/objstore/...` → ok
- `go mod tidy` + `go build ./...`(整模块)→ 全绿

## 下一步

Step3:`/v1/chat` 解码后 PutObject,按 `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`
(默认 16MiB)分流 inline vs key-only,映射到 gRPC Attachment。
