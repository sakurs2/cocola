# fix: 固定 Artifact Markdown MIME 类型

- 变更时间：2026-08-09 00:03 (+08:00)

## 变更理由

提交 `ecbf703` 的远端 Agent Runtime CI 在 Python 3.11 上失败。Artifact 列表直接依赖 `mimetypes.guess_type` 和宿主机 MIME 数据库；Python 3.12 开发环境把 `.md` 识别为 `text/markdown`，CI 的 Python 3.11 则返回空值并回退为 `application/octet-stream`，造成跨平台行为和测试结果不一致。

## 变更内容

- `deploy/sandbox-runtime/cocola_sandbox.py`：为 `.md` 和 `.markdown` 增加局部、确定性的 `text/markdown` 映射，其余类型继续使用标准库判断和二进制兜底。
- `apps/agent-runtime/tests/test_cocola_sandbox_cli.py`：模拟平台 MIME 数据库缺少 Markdown 的情况，验证 Artifact 元数据仍保持稳定。
- 使用独立 Python 3.11 + 锁定依赖环境复现 CI，并确认完整 Agent Runtime 测试为 323 passed、2 skipped。
