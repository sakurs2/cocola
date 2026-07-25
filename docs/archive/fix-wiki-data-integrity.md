# fix: 收紧 Wiki 数据完整性边界

- 变更时间：2026-07-25 23:13 (+08:00)

## 变更理由

Wiki 首版审查发现五类完整性风险：Markdown 自动保存可能覆盖新输入；元数据提交结果不确定时可能误删文件对象；合法点文件在 Agent 工作区可能发生路径碰撞；占位 XML 组成的伪 Office 包会通过上传校验；并发移动文件夹可能绕过目录环检查。

首版继续遵循简单实现原则，不引入自动保存状态机、异步补偿服务、对象 GC、完整 Office 转换服务或新的数据库表。

## 变更内容

- `apps/web/components/wiki/wiki-workspace.tsx`：移除 Markdown debounce 自动保存，改为显式保存；加载和保存期间只读，未保存切换与页面离开时提示。
- `apps/gateway/internal/wiki/postgres.go`：文件创建和 Markdown 新版本在提交后直接返回事务内已知结果；同一用户的目录移动通过 PostgreSQL 事务级 advisory lock 串行化，递归查询使用去重集合。
- `apps/gateway/internal/httpapi/wiki.go`：仅对明确未提交的业务错误清理对象，未知数据库错误保留对象；Office 上传读取 Content Types 和入口 XML，校验 CRC、根节点与 MIME 声明。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：Wiki 逻辑路径采用拒绝式校验，保留合法前导点，并在写入前拒绝目标路径碰撞。
- `apps/gateway/internal/httpapi/wiki_test.go`、`apps/agent-runtime/tests/test_wiki_provisioning.py`：增加未知提交结果、伪 OOXML、路径穿越、点文件和路径碰撞回归用例。

## 关键取舍

- Markdown 采用用户主动保存，避免在首版引入复杂自动保存状态机。
- 数据库结果不确定时宁可保留少量无引用对象，也不冒险删除可能已被版本元数据引用的内容。
- Office 校验定位为基础 OOXML 结构校验，不承诺覆盖 Microsoft Office 的全部格式语义。
