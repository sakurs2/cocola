# fix: 修复 Python StrEnum 格式门禁

- 变更时间：2026-07-26 18:24 (+08:00)

## 变更理由

全仓 `make format-check` 被 Ruff `UP042` 阻塞：`Env` 和 `ErrorCode` 使用了 Python 3.11 之前常见的 `str, Enum` 多重继承写法。项目最低 Python 版本已经是 3.11，可以直接使用标准库 `StrEnum`。

## 变更内容

- `packages/py-common/cocola_common/config.py`：将 `Env(str, Enum)` 替换为 `Env(StrEnum)`。
- `packages/py-common/cocola_common/errors.py`：将 `ErrorCode(str, Enum)` 替换为 `ErrorCode(StrEnum)`。
- 保持所有枚举成员值不变；现有 `.value`、身份比较、字符串比较和 Pydantic 序列化行为不变。
