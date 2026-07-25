# feat: 增加用户 Wiki 与 Agent 文件引用

- 变更时间：2026-07-25 22:12 (+08:00)

## 变更理由

用户需要在主 Web 页面维护一套可复用的个人知识文件：按文件夹组织、在线创建和编辑 Markdown，并在 Agent 对话中通过 `@` 选择已有文件。上传不设置累计容量或文件数配额，只限制单文件大小；同时需要支持 DOCX、XLSX、PPTX 原文件进入 Agent 沙箱后被可靠读取。

## 变更内容

- `db/migrations/00047_user_wiki.sql`：新增用户隔离的 Wiki 节点、不可变文件版本、父子关系、同级名称唯一和软删除约束。
- `apps/gateway/internal/wiki/`、`apps/gateway/internal/httpapi/wiki.go`：实现目录树、搜索、创建 Markdown、受限上传、重命名、移动、删除、下载和带 revision 的 Markdown 保存；支持 `.md/.txt/.csv/.json/.yaml/.yml/.pdf/.docx/.xlsx/.pptx`，默认单文件上限 20 MiB。
- `apps/gateway/internal/httpapi/simple_chat.go`、`apps/gateway/internal/agent/client.go`、`packages/proto/cocola/agent/v1/agent.proto`：按当前用户解析 `@` 引用，将不可变版本快照写入会话并传给 Agent Runtime。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：校验对象大小和 SHA-256 后，将引用文件按逻辑目录写入 `/workspace/wiki`，并把不可信内容边界和可用读取命令加入 Agent 上下文。
- `deploy/sandbox-runtime/wiki_reader.py`、`deploy/sandbox-runtime/Dockerfile`：增加 DOCX、XLSX、PPTX 只读提取工具及固定版本依赖；Office 文件保留原件，不提供浏览器在线编辑。
- `apps/web/app/wiki/`、`apps/web/components/wiki/`、`apps/web/app/api/wiki/`：增加 Wiki Tab、嵌套目录树、搜索、多文件上传、拖拽移动、Markdown Monaco 源码编辑/预览、显式保存和编辑冲突提示。
- `apps/web/components/assistant-ui/thread.tsx`、`apps/web/app/runtime-provider.tsx`：增加对话框 `@` Wiki 文件选择、引用卡片和 `wiki_refs` 请求映射。
- `.env.example`、`scripts/run-stack.sh`、`docs/configuration.md`：增加 `COCOLA_WIKI_MAX_FILE_BYTES` 配置说明，本地默认 20 MiB，不增加累计配额。
- `apps/gateway/internal/**/*_test.go`、`apps/agent-runtime/tests/test_wiki_provisioning.py`：覆盖格式校验、输入边界、对象清理、版本快照、gRPC 映射、目录安全和完整性校验。

## 关键取舍

- Markdown 采用 Monaco 源码编辑器配合现有 Markdown 渲染器，优先保证 Markdown 文本的可逆性和与 Agent 的一致语义。
- DOCX、XLSX、PPTX 采用“原文件存储 + 沙箱只读解析”，避免在首版引入格式有损的浏览器编辑与复杂预览链路。
- 每次 Markdown 保存生成新对象和新版本；对话发送时固定版本 ID，后续编辑不会改变历史 Agent 上下文。
