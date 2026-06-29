# Plan: 补齐 opensandbox provider 的 WriteFile / ReadFile

日期: 2026-06-29
关联: ADR-0013(OpenSandbox 可插拔 provider)、ADR-0002(provider 接口)、ADR-0008(双卷文件系统)
状态: 进行中

## 目标

把 opensandbox provider 仅剩的两个未实现方法 `WriteFile` / `ReadFile`
(当前 `return errNotImplemented`)落地,使其与 docker provider 行为等价:
- `WriteFile(ctx, sid, path, data)`:把字节写入沙箱内 `path`。
- `ReadFile(ctx, sid, path) ([]byte, error)`:读出沙箱内 `path` 全部字节。

至此 opensandbox 后端的 8 个 SandboxProvider 方法全部实现,后端能力与 docker 持平。

## 事实依据(已核对)

### provider 接口契约(`internal/provider/provider.go`)
    WriteFile(ctx context.Context, sid, path string, data []byte) error
    ReadFile(ctx context.Context, sid, path string) ([]byte, error)
docker provider 用 `CopyToContainer` / `CopyFromContainer`(tar 流)实现,语义是
「按绝对路径整文件写/读」,不涉及 owner/mode 推断之外的语义。

### execd 文件 API(`specs/execd-api.yaml`,从 execd v1.0.19 镜像 + 上游 spec 核对)
execd 在沙箱内 44772 端口提供:

- **`POST /files/upload`** — multipart/form-data,两个有序部分:
  1. `metadata`(contentType application/json):`FileMetadata{path, owner, group, mode}`
     - `path`:目标绝对路径
     - `owner`/`group`:属主(可选)
     - `mode`:八进制权限整数(例 0644 传 644)
  2. `file`(contentType application/octet-stream):文件原始字节
  成功返回 200(无 body)。
- **`GET /files/download?path=<abs>`** — 返回 octet-stream 全文件;
  支持 Range 头与 offset/limit 行读(本实现只用全量读,不传这些参数)。
  200 = 全量;404 = 不存在。

execd 端点解析与鉴权头复用既有 `resolveExecd`(GET
`/sandboxes/{id}/endpoints/44772[?use_server_proxy=true]`,回填
`X-EXECD-ACCESS-TOKEN`)。

## 实现方案(最大化复用现有代码)

两个方法都复用 Exec 已验证的前置链:
`resolve(sid)` -> `thawIfPaused` -> `resolveExecd` -> (文件 API 调用)。
**不复用 `waitExecdReady`**:文件读写是控制面操作,非紧接冷启动的关键路径,
单次失败由调用方重试即可,先与 docker 行为对齐(docker 也无探针)。

HTTP 走 `p.stream` 客户端(无超时,受 ctx 约束),与 execd 其它调用一致;
回填 `resolveExecd` 返回的 headers。

### WriteFile
1. `osbID := resolve(sid)`;`thawIfPaused`;`url, headers := resolveExecd`。
2. 用 `mime/multipart.Writer` 构造 body:
   - 先写 `metadata` 部分(Content-Type application/json),
     写入 `json.Marshal(fileMetadata{Path: path, Owner: execUser, Group: execUser, Mode: 0o644})`。
     owner/group 用 `p.execUser`(默认 cocola),保证写出文件归属与 Exec 运行用户一致;
     `execUser==""` 时省略 owner/group。
   - 再写 `file` 部分(`CreateFormFile("file", base(path))`,Content-Type octet-stream),写入 data。
3. `POST {execd}/files/upload`,Content-Type 由 writer 给出(含 boundary),回填 headers。
   非 2xx -> 带状态码与 body 的错误。

### ReadFile
1. 同样前置链拿到 `url, headers`。
2. `GET {execd}/files/download?path=<url-escaped path>`,回填 headers。
3. 404 -> `fs.ErrNotExist` 包装错误(便于调用方区分);其余非 2xx -> 状态码错误。
4. `io.ReadAll(resp.Body)` 返回字节。

### 公共小工具
- 新增 `fileMetadata` struct(json: path/owner/group/mode,omitempty 处理空 owner/group)。
- metadata 部分用 `CreatePart(textproto.MIMEHeader{...})` 手动设 application/json
  (CreateFormFile 固定 octet-stream)。

### 删除/收敛 errNotImplemented
两个 stub 替换后,`errNotImplemented` 不再被引用 -> 删除该 var,更新包头注释
(去掉 WriteFile/ReadFile deferred 一句,改为 8 方法已全部实现)。

## 不做(non-goals)
- 不实现 Range / offset-limit 行读(接口只要求整文件)。
- 不实现目录递归 / mkdir(接口未要求)。
- 不引入 OpenSandbox Go SDK(保持 stdlib-only 一致性)。
- 不加 waitExecdReady 探针(与 docker 行为对齐)。

## 验证
1. 单测(roundTripFunc stub,GOWORK=off go test):
   - `TestWriteFile_PostsMultipart`:命中 `POST /files/upload`,解析 multipart 得 metadata.path
     正确、file 字节 == 输入。
   - `TestReadFile_GetsDownload`:命中 `GET /files/download?path=...`,返回字节正确。
   - `TestReadFile_NotFound`:404 -> 错误且 `errors.Is(err, fs.ErrNotExist)`。
2. 在 apps/sandbox-manager 下 `GOWORK=off go build ./... && go vet ./... && go test ./...` 全绿。
3. changelog 入 docs/archive/;review-before-commit;提交不带 .claude/,不 --no-verify。
