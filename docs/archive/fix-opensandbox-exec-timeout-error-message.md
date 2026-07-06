# fix: OpenSandbox Exec 超时可配置并隐藏底层错误

- 变更时间：2026-07-06 17:43 (+08:00)

## 变更理由

用户在让 agent 用浏览器截图网页时遇到
`sandbox exec failed: opensandbox: sse read: context deadline exceeded`。根因是单次
OpenSandbox Exec 默认 5 分钟超时，浏览器/Playwright 脚本卡住后底层超时错误被直接透出到
前端，既不易理解，也暴露了 provider 实现细节。

## 变更内容

- apps/sandbox-manager/internal/provider/opensandbox：新增
  `COCOLA_OPENSANDBOX_EXEC_TIMEOUT`，用于配置 provider 默认单次 Exec 超时；请求级
  `timeout_secs` 仍优先。
- apps/agent-runtime/cocola_agent_runtime/shim_provider.py：将 sandbox exec timeout 类错误映射
  成用户可理解的“工具执行超时”提示，原始底层错误只写日志。
- .env.example / deploy/docker-compose/docker-compose.full.yml /
  deploy/opensandbox-k8s/README.md：补充 Exec timeout 配置说明和容器环境变量透传。
- apps/agent-runtime/tests / apps/sandbox-manager/internal/provider/opensandbox：补充 timeout 映射与
  env 解析测试。

