# feat: 聊天附件上传 P0 后端(proto + agent-runtime 预置 + gateway 透传)

日期：2026-07-01 · 关联 Plan：docs/plan/web-file-upload.md(Step 1-3/6)· 将新增 ADR-0017

## 背景
P0 附件上传采用「后端预置(push)」模型:文件在 agent 运行前被写进会话沙箱,
模型可直接按路径读取(对齐 OpenAI Code Interpreter 挂载 / E2B files.write())。
本次覆盖契约层与后端两侧;前端在 Step 4 单独提交。

## 改动
### proto(packages/proto)
- `QueryRequest` 增 `repeated Attachment attachments = 6;`。
- 新增 `message Attachment { string filename = 1; bytes content = 2; string mime = 3; }`
  (filename=sanitized basename;content=裸字节;mime=浏览器尽力而为)。
- `buf generate` 重生成 Go/Python 两侧 gen。

### agent-runtime(apps/agent-runtime)
- `server.py`:executor 注入(可选,未接线时降级为丢弃+告警,与无 binder 同姿态)。
  Query 在沙箱 acquire 之后、provider.query 之前调用 `_provision_attachments`:
  - `_sanitize_name`:取 basename、去分隔符/父级引用/NUL,兜底固定名,永不逃出 ./uploads/。
  - 经 `pwd` 解析沙箱绝对 cwd(provider 无关),`mkdir -p uploads` 后逐个 `binder.write_file`
    落到 `/workspace/<session_id>/uploads/<name>`(ADR-0008 T1b 会话隔离卷,/workspace 根未挂载)。
  - 拼一段前言 prepend 到 prompt,告知模型文件清单与相对路径。
  - 预置失败发终止 `error` 事件(与 acquire 失败同约定),不裸崩流。
  - `__main__.py`/`sandbox_binder.py`:接线 executor + write_file 支撑。
- 新增 `tests/test_attachment_provisioning.py`;`test_server.py`/`test_sandbox_binding.py` 适配。

### gateway(apps/gateway)
- `httpapi/api.go`:`chatRequest` 增 `Attachments []attachmentDTO`
  (filename/content_b64/mime;`DisallowUnknownFields` 已开故须显式加)。
  base64 解码为裸字节后映射 `agent.Attachment`;解码失败丢弃该附件+告警,不整体失败。
- `agent/client.go`:`Query` 增 `Attachments` 字段并映射到 gRPC `pb.Attachment`。
- 新增 `api_test.go` / `e2e_test.go` 用例覆盖附件透传。

## 校验
- gateway:`go vet ./...` 0;`go test -count=1 ./internal/httpapi ./internal/integration` 全绿。
- agent-runtime:`pytest -q` → 55 passed, 2 skipped。

## 非目标
- 前端(adapter/回形针 UI)见 Step 4 提交;端到端联调见 Step 5;ADR-0017 见 Step 6。
- B. pull、OSS 持久化、大文件/分片均为后续阶段(见 Plan 非目标)。
