# feat: OpenSandbox 补齐 WriteFile/ReadFile(execd 文件上传/下载)

日期：2026-06-29 · 关联 ADR：0013(可插拔 provider)· 关联 PoC：P0/P1/P2

## 背景
opensandbox provider 此前 8 个 SandboxProvider 方法中,WriteFile/ReadFile 一直
以 `errNotImplemented` 占位(P2 仅打通流式 Exec / Pause / Resume,文件传输另案)。
本次补齐这两个方法,使 opensandbox 后端与 docker 后端在文件读写语义上对齐,
provider 接口全部实装。

## execd 文件 API(依据 specs/execd-api.yaml,并经 execd v1.0.19 二进制串校验)
- `POST /files/upload`:`multipart/form-data`,有序两段——先 `metadata`
  (Content-Type `application/json`,内容为 `FileMetadata{path, owner, group, mode}`),
  后 `file`(Content-Type `application/octet-stream`,原始字节)。2xx 即成功。
- `GET /files/download?path=<abs>`:返回 octet-stream 整文件;404 表示文件不存在。
- `FileMetadata.mode` 为八进制权限整数(0o644 → 644)。

## 改动(apps/sandbox-manager/internal/provider/opensandbox)
### WriteFile —— 复用 Exec 的解析链,经 execd multipart 上传
- 复用既有 `resolve(sid)` → `thawIfPaused` → `resolveExecd` 链拿到 execdURL 与
  鉴权头,避免重复造轮子;不额外调 `waitExecdReady`(对齐 docker provider:
  文件操作无就绪探针)。
- 手工构造 multipart:`metadata` 段用 `CreatePart` 显式置 `application/json`
  (`CreateFormFile` 会固定为 octet-stream,不符合 execd 解析),`file` 段为
  octet-stream + `filename=<base>`。
- owner/group 取 Exec 用户(默认 `cocola`),保证控制面写入的文件能被沙箱内同样
  以 cocola 身份运行的 claude 进程读取;execUser 为空时留空,沿用 execd 默认属主。
- 非 2xx 读取响应体(LimitReader 4KiB)拼进错误信息。

### ReadFile —— GET /files/download?path= 整文件下载
- 同样复用解析链;`neturl.Values` 编码 path 查询参数。
- 404 包装为 `fs.ErrNotExist`,使调用方可用 `errors.Is` 区分"文件缺失"与传输错误;
  其余非 2xx 返回带状态码与响应体的错误;成功则 `io.ReadAll` 整体返回。
- 语义对齐 docker provider 的 CopyFromContainer(整文件、绝对路径,无 Range /
  按行 offset-limit)。

### 清理
- 删除 `errNotImplemented` 哨兵与不再使用的 `errors` 导入。
- 更新包头注释:WriteFile/ReadFile 不再"deferred",已映射到 execd 上传/下载,
  8 个方法全部实装。

## 校验
- `GOWORK=off go build ./... && go vet ./... && go test ./...`(sandbox-manager)
  全绿;gofmt 干净。
- 新增单测:
  - `TestWriteFile_PostsMultipart`:解析 multipart,断言 POST /files/upload、
    metadata.path、metadata.owner==cocola、file 字节与输入一致。
  - `TestReadFile_GetsDownload`:断言 download 查询 path 与返回字节。
  - `TestReadFile_NotFound`:404 → `errors.Is(err, fs.ErrNotExist)`。
- 删除已失效的 `TestDeferredFileMethods_ReturnNotImplemented`。

## 非目标
- 不实现 Range / 按行 offset-limit 下载(execd 支持,本次不需要)。
- 不做远端目录创建(mkdir)、不引入 opensandbox SDK(维持 stdlib-only REST 客户端)。
- 不在文件操作前加 execd 就绪探针(对齐 docker provider)。
