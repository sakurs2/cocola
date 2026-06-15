# 变更记录：S1 —— `*_FILE` 机密间接缝口

- 关联 Plan：`docs/plan/hardening-vault-secret-management.md`（S1 阶段）
- 关联 ADR：ADR-0008 §5「Secrets via Vault, using mature integrations (no
  custom crypto)」
- 日期：2026-06-15

## 背景

机密（`COCOLA_AUTH_SECRET` 全链路 HS256 签名、`COCOLA_ADMIN_KEY`、上游 API
key）此前全部以明文 env 读取。要让 Vault Agent Sidecar / CSI / Docker secret
注入机密，需要一个「从文件读机密」的标准缝口，且**不给应用塞 Vault SDK
依赖**（用户约束「复用成熟集成、不造轮子」+ ADR §5「no custom crypto」）。

## 设计

采用业界标准的 `*_FILE` 间接约定（Postgres 官方镜像、Docker secrets、Vault
Agent template 渲染均用此约定）：读机密时优先看 `<NAME>_FILE`，若设则从该路径
读取文件内容（strip 尾换行），否则回落到 `<NAME>` env。Vault Agent 把机密渲染
到 `/vault/secrets/*`，operator 只需令 `<NAME>_FILE` 指向它，**应用零感知
Vault**。不设 `_FILE` 时行为与 `os.Getenv` 完全一致，dev `.env` 流不变。文件
读失败降级到 env 回落，避免挂载瞬时缺口导致崩溃。

## 改动

- `packages/go-common/config/secret.go`（新增）：`SecretFromEnv(name)` 共享
  helper，gateway 与 admin-api 复用。
- `packages/go-common/config/secret_test.go`（新增）：5 项单测（文件优先、
  env 回落、文件不可读回落、都缺为空、仅 strip 尾换行）。
- `apps/gateway/cmd/gateway/main.go`：`COCOLA_AUTH_SECRET` 改走 `SecretFromEnv`。
- `apps/admin-api/cmd/admin-api/main.go`：`COCOLA_ADMIN_KEY` /
  `COCOLA_AUTH_SECRET` 改走 `SecretFromEnv`。
- `apps/admin-api/cmd/admin-mint/main.go`：`COCOLA_AUTH_SECRET` 改走
  `SecretFromEnv`。
- `apps/llm-gateway/cocola_llm_gateway/config.py`：新增 `read_secret_env(name)`
  helper；`_resolve_secret` env 分支与 `auth_config_from_env` 的
  `COCOLA_AUTH_SECRET` 读取改走它（`issue_token.py` 经 `auth_config_from_env`
  传递性覆盖）。
- `apps/llm-gateway/tests/test_secret_indirection.py`（新增）：7 项单测，含
  `_resolve_secret` / `auth_config_from_env` 经文件读取的端到端校验。
- `docs/plan/hardening-vault-secret-management.md`（新增）：S1/S2/S3 计划。
- `go.work.sum`：构建拉入的传递性校验和条目。

## 验收

- `packages/go-common`：`go test ./config/...` 全绿。
- gateway / admin-api：`go build ./...` + `go vet ./...` 干净。
- llm-gateway：`uv run ruff check` 通过；`uv run pytest
  tests/test_secret_indirection.py` 7 passed。

## 范围说明

S1 仅代码侧缝口（本机可完整测试）。S2（Vault 部署清单）、S3（runbook +
ADR Status）后续独立提交；K8s 真链路验收待目标集群。
