# feat(agent-runtime): P1a Step4 — 代 pull 仅 oss_key 的附件

附件 P1a(ADR-0017)第 4 步:agent-runtime 侧对"仅 oss_key"(大文件)的附件
从对象存储 GetObject 取回字节,再走既有 provision 路径写进 ./uploads/。
送达仍然全程 push——模型侧零改动,无新工具;后端替模型把大文件"代 pull"进沙箱。

## 改动

- `cocola_agent_runtime/objstore.py`(新增):
  - `Fetcher` Protocol(`get(key) -> bytes`):servicer 依赖抽象而非 minio SDK,
    对齐 runtime 其余组合根姿势(真 MinIO 上线、fake 上测)。
  - `MinioFetcher(client, bucket)`:`get_object` 全量读入内存(附件在上游已限流,
    全读可接受),`finally` 里 `close()`+`release_conn()` 回收连接。
  - `fetcher_from_env()`:从 `COCOLA_MINIO_*` 构造(与 gateway 同源命名);
    endpoint/bucket 缺一即返回 None(不启用)。`minio` 延迟导入,零配置本机启动
    不需要该依赖。
  - `_secret_from_env`:`_FILE` 间接(ADR-0008),支持 Vault Agent 渲染到盘。
- `cocola_agent_runtime/server.py`:
  - 新增 `_ResolvedAttachment(NamedTuple){filename, content}`:两条送达路径统一成
    "文件名 + 字节在手"的形状。
  - Servicer `__init__` 增可选 `objstore: Fetcher | None`;未接线时,"仅 oss_key"
    附件会浮为一条干净的 provision 报错而非静默空文件。
  - `_provision_attachments` 先 `await self._materialize_attachments(...)` 再把结果
    传给 `_provision_into_sandbox` / `_provision_onto_host`(两路都收 resolved)。
  - 新增 `_materialize_attachments`:逐个附件,若 `not content and oss_key` 则
    `await asyncio.to_thread(self._objstore.get, oss_key)`(阻塞 SDK 调用丢进
    worker 线程,不堵事件循环);无 fetcher 则 raise。用 `getattr(att,"oss_key","")`
    兼容 P0 无该字段的 FakeAttachment。
- `cocola_agent_runtime/__main__.py`:组合根用 `objstore=fetcher_from_env()` 接线。
- `pyproject.toml` / `uv.lock`:新增 `minio>=7.2`(锁定 7.2.20,连带 argon2-cffi
  / argon2-cffi-bindings / pycryptodome 三个新传递依赖;`uv lock` 走 pypi.org
  仅新增 4 包、零改动其余锁项)。

## 单测

- `tests/test_attachment_oss_pull.py`:仅 key 被代 pull 并写盘;inline 不 pull;
  仅 key 但无 fetcher → 终态 `error` 事件且 provider 被跳过;host 路径小+大混合。
- `tests/test_objstore.py`:`fetcher_from_env` None/构造两态;`_secret_from_env`
  `_FILE` + 明文回落;`MinioFetcher` 经 fake client 读取并回收连接。

## 校验

`.venv/bin/python -m pytest`(uv 托管 venv)Step4 用例 `10 passed`;
全量 `71 passed, 2 skipped`;`ruff check` 全绿。

## 下一步

Step5:compose/env 接线——把 `COCOLA_MINIO_*` + `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`
注入 gateway 与 agent-runtime,补 full compose 的 minio(dev compose 已具备)。
