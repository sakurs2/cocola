# feat(gateway): P1a Step3 — /v1/chat upload + threshold split

附件 P1a(ADR-0017)第 3 步:把 Step2 的对象存储客户端接入 `/v1/chat`,让 gateway
成为附件"真源"的写入方,并按可配置阈值分流小/大文件。

## 改动

- `internal/agent/client.go`:`Attachment` 增 `OssKey string` / `Size int64`,
  `Stream` 映射到 gRPC `Attachment.OssKey/Size`(Step1 已加的 proto 字段)。
- `internal/httpapi/api.go`:
  - `API` 增可选 `store objstore.Store` + `inlineMaxBytes int64`;新增
    `WithObjStore(store, threshold)` 建造方法(threshold<=0 回落
    `DefaultInlineMaxBytes`=16MiB)。
  - chat handler:每个附件解码后 `store.Put`(key=`attachments/<session>/<uuid>-<name>`),
    再按阈值分流——`Size<=阈值`保留 inline `Content` 且带 `OssKey`;
    `Size>阈值`清空 `Content` 仅带 `OssKey`(交 agent-runtime 代 pull)。
  - 优雅降级:`store==nil` 走 P0 纯内联;`Put` 失败则该文件回落内联(字节仍在手)。
  - 新增 `objectKey` / `sanitizeKeySegment`:uuid 前缀防碰撞,basename 化 + 去
    `..`/分隔符/NUL,对齐 agent-runtime `_sanitize_filename` 的防穿越姿势。
- `cmd/gateway/main.go`:从 `COCOLA_MINIO_*` 构造 store(仅 endpoint+bucket 均配置
  时启用),读取 `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`(默认 16MiB,非法值告警忽略),
  `api.WithObjStore(...)` 接线;未配置则保持 P0 纯内联(feature 默认 dark)。
- `go.mod`:`github.com/google/uuid` 提升为 direct(被 httpapi 直接导入)。

## 单测(internal/httpapi)

- 小于阈值:1 次 Put、转发 1 个附件、带 OssKey、保留 inline content、Size 正确、
  key 形状 `attachments/s1/...-a.txt`。
- 大于阈值:带 OssKey、inline content 清空、Size 仍为原长、store 保有全字节(真源)。
- 无 store:不落桶、无 OssKey、保留 inline(P0 兼容)。
- `sanitizeKeySegment` 防穿越表。

## 校验

`golang:1.24` 容器经 byted 代理:`go build ./...` / `go vet ./...` 全绿;
`go test ./internal/httpapi/... ./internal/agent/... ./internal/objstore/...` 全 ok。

## 下一步

Step4:agent-runtime 侧对"仅 oss_key"的附件 GetObject 取字节写进 uploads/。
