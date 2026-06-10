# chore: 存量代码基线格式化 + 修复遗留 lint

- 变更时间：2026-06-10 15:12 (+08:00)

## 变更理由
引入统一格式化工具后，存量代码此前未经统一，需一次性基线规整，使新机制生效后
历史代码即处于干净状态；同时修复 ruff 暴露的若干真实风格问题。

## 变更内容
- 全仓库基线格式化：Python `ruff format`（约 54 文件）+ `ruff check --fix`；
  Go `gofmt -w -s`（约 8 文件）；前端 `prettier --write`。
- 手工修复 ruff 无法自动修复的问题：
  - test_claude_sdk_provider.py：删除未使用 walrus 变量 prov（F841）
  - llm-gateway/config.py：超长行换行（E501）、raise ... from e（B904）
  - llm-gateway/server.py：合并嵌套 if（SIM102）
  - llm-gateway/service.py：超长布尔条件改写，已穷举验证逻辑等价（E501）
  - scripts/*-e2e.py：% 格式化改 f-string（UP031）
- 验证：`ruff check .` 全过；test_claude_sdk_provider.py 5 用例全过。

## 备注
- llm-gateway 全量 pytest 存在预先存在的环境问题（conda protobuf runtime 5.29.6
  与 gencode 6.33.5 版本错配，import 阶段失败），与本次格式化无关，未处理。
