# fix: OpenSandbox WriteFile 上传被 execd 拒("metadata file is missing")

日期：2026-06-30 · 关联:feat-opensandbox-writefile-readfile

## 背景
对真 OpenSandbox server(`opensandbox/server:latest`,:8090)跑 `make
verify-opensandbox` 时,新增的 Stage 4n 原生 WriteFile 直接被 execd 以
`400 INVALID_FILE_METADATA: metadata file is missing` 拒绝。单测(假 server)
未暴露,因为 stub 用 `part.FormName()` 取值、不模拟 execd 的 FormFile 语义。

## 根因
execd 通过 multipart `FormFile("metadata")` 读取元数据段。Go 的 multipart
规约里,**只有带 `filename` 的 part 才算"文件"**,无 filename 的 part 被解析为
普通表单值——服务端 FormFile 取不到,即报 "metadata file is missing"。
原实现的 metadata 段只设了 `name="metadata"`,缺 `filename`。

## 改动(apps/sandbox-manager/internal/provider/opensandbox)
- WriteFile 的 metadata 段 Content-Disposition 补 `filename="metadata.json"`,
  其余(`application/json`、file 段)不变。
- 单测 `TestWriteFile_PostsMultipart` 增加回归断言:metadata 段 `part.FileName()`
  必须非空,锁死该约束防回归。

## 校验
- `GOWORK=off go build/vet/test ./...`(sandbox-manager)全绿,gofmt 干净。
- 真 server 回归 `COCOLA_OPENSANDBOX_EXEC_USER= make verify-opensandbox`:
  **VERIFY OK — all stages passed**;4n.write / 4n.read / 4n.missing 三项全绿,
  ReadFile 取回字节一致、不存在路径返回 fs.ErrNotExist。

## 备注
- 回归用 `COCOLA_OPENSANDBOX_EXEC_USER=`(空,以 root 跑)规避验收镜像
  `python:3.12-slim` 无 cocola 用户(uid 10001)导致的 `runuser: user cocola
  does not exist` 噪声——那是验收镜像问题,非 provider bug;全栈用的 brain 镜像
  (派生自 opensandbox/code-interpreter)内置该用户。
