# chore: 统一存量代码格式

- 变更时间：2026-07-26 14:47 (+08:00)

## 变更理由

全仓 `make format-check` 被 33 个 Web 存量文件的 Prettier 差异阻塞；修复这些文件后，检查继续暴露出 1 个 Python 存量文件的 Ruff 格式差异。上述问题与功能逻辑无关，但会导致本地和 CI 格式检查无法通过。

## 变更内容

- `apps/web/`：使用仓库当前 Prettier 配置机械格式化检查报出的 33 个 TS、TSX、JS 文件。
- `apps/agent-runtime/cocola_agent_runtime/sandbox_client.py`：使用 Ruff format 移除存量格式差异。
- 未修改业务逻辑；格式化后全仓 `make format-check` 通过。
