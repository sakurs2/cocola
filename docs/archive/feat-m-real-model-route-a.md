# feat(deploy): Route A 接入真实 Anthropic 模型,打通端到端真链路

把 Route A(brain in sandbox)从 echo/fake 验证态切到**真实模型**:llm-gateway
启用 anthropic provider,composition root 把沙箱镜像 + 注入凭证(ANTHROPIC_*)
下发到用户沙箱,沙箱内 Claude Code CLI 经宿主发布端口回连 gateway 出网。

方案详见 `docs/plan/m-real-model-route-a.md`。

## 改动

- `sandbox_binder.py`:`SandboxManagerBinder` 新增 `default_image` /
  `default_env` 构造参数;`acquire(...)` 以 `image or default_image`、
  `{**default_env, **env}` 合并 provisioning 默认值,允许每次调用覆盖。

- `__main__.py`:
  - 新增 `_sandbox_provisioning()`——从 `COCOLA_SANDBOX_IMAGE` /
    `COCOLA_SANDBOX_LLM_BASE_URL` / `COCOLA_SANDBOX_LLM_TOKEN` /
    `COCOLA_SANDBOX_MODEL_ALIAS` 读出镜像与待注入的 ANTHROPIC_* env
    (BASE_URL / AUTH_TOKEN / MODEL / SMALL_FAST_MODEL)。
  - `_build_binder()` 把上述默认值喂给 `SandboxManagerBinder`,启动日志打印
    sandbox_image 与 creds_injected。
  - docstring 补充 4 个 COCOLA_SANDBOX_* env 说明。

- `deploy/docker-compose/docker-compose.full.yml`:
  - llm-gateway:`COCOLA_LLM_PROVIDER` / `COCOLA_ANTHROPIC_BASE_URL` /
    `COCOLA_ANTHROPIC_API_KEY` / `COCOLA_ANTHROPIC_MODEL` / `COCOLA_LLM_DEFAULT_ALIAS`
    走 .env 注入(刻意不注 COCOLA_AUTH_SECRET,本地保持鉴权关闭)。
  - agent-runtime:`COCOLA_AGENT_ROUTE=A` + 4 个 COCOLA_SANDBOX_* 默认值。
  - **网关发布端口 8081 → 18091**:规避宿主上 IPv4 `*:8081` 的遗留监听冲突
    (容器内 host.docker.internal 解析到宿主 IPv4,会误连那个鉴权开启的幽灵
    进程,导致 `401 token must have three segments`);`COCOLA_SANDBOX_LLM_BASE_URL`
    同步改为 `http://host.docker.internal:18091`。

## 测试

- `tests/test_sandbox_binding.py` 新增 `test_manager_binder_applies_provisioning_defaults`:
  验证未指定时套用默认 image/env、且每次调用可覆盖(env 为合并语义)。
  binding 全量 7 passed。
- 端到端真链路(`.env` + 真实 anthropic provider)三连验收:
  - 纯对话:`The capital of France is Paris.`(claude-opus-4-6,$0.00989)。
  - 工具调用:触发 Bash tool_use → `uname -s` → 回复 `Linux`($0.00579)。
  - gateway 日志确认真实出网:`POST https://aiberm.com/v1/messages "200 OK"`。

## 范围与回滚

- 本次不做:Postgres 持久化、生产鉴权开启、K8s provider、egress allowlist、
  hibernate/resume 调优(后续)。
- 回滚:还原 compose 端口与 provider 即回到 fake/echo 态;Route A 开关
  `COCOLA_AGENT_ROUTE` 不变。
