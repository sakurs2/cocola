# fix: 稳定 sandbox code-server 访问与生命周期

- 变更时间：2026-07-19 14:03 (+08:00)

## 变更理由

Sandbox runtime 新增 resident code-server 后，新 Agent 对话打开 Code 面板会出现页面重载提示，浏览器报告 WebSocket 1006。链路排查发现问题并非单点：OpenSandbox 自定义 entrypoint 覆盖了镜像 CMD，code-server 未必启动；浏览器 Origin 与 sandbox 内 Host 不同，code-server 会拒绝升级；Web/Gateway 的连接包装和复用可能让 Upgrade 丢失；Preview 子路径的尾斜杠重定向会破坏相对资源路径。

同时，空白对话在第一次消息前不会 Acquire sandbox，历史对话的 sandbox 也可能已回收，直接挂载 Code iframe 会先显示 502。另有 OpenSandbox bootstrap 在镜像 `WORKDIR /workspace` 中提前启动 execd，而 Cocola 随后会删除并重建 `/workspace` symlink，导致 execd 长期持有失效 cwd 并反复输出 `getcwd` 错误。

## 变更内容

- `.env.example`、`apps/cli/internal/config/config.go`、`apps/cli/internal/assets/compose.yaml`、`scripts/run-stack.sh`：新增并贯通 `COCOLA_PUBLIC_ORIGINS`，本地默认仅允许显式 localhost Origin，禁止通配符。
- `apps/web/server.mjs`、`apps/web/lib/public-origins.mjs`：Preview WebSocket 在读取登录 Cookie、签发 runtime token 前精确校验 Origin；每次 Upgrade 使用独立上游连接并补充静默关闭日志。
- `packages/go-common/metrics/http.go`、`apps/web/next.config.mjs`：metrics writer 透传 `http.Hijacker`，并保留 Preview 子路径尾斜杠，确保 WebSocket 与 code-server 相对资源正常代理。
- `apps/web/components/assistant-ui/workspace-panel.tsx`、`apps/web/lib/code-editor-readiness.mjs`：Workspace 侧边栏不再自动加载 Code；主动打开 Code 后先探测环境，准备中自动退避重试，历史 sandbox 回收时提示继续对话恢复。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：OpenSandbox entrypoint 显式启动 code-server；从公开 Origin 派生平台托管的 trusted origins，禁止 Agent 覆盖；普通 Exec 默认 cwd 保持 `/workspace`，readiness 探针固定使用稳定的 `/`。
- `deploy/sandbox-runtime/code-server-launch.sh`：安全展开多个 `--trusted-origins`，拒绝通配符和非法 host，不使用 `eval`。
- `deploy/sandbox-runtime/Dockerfile`：镜像 `WORKDIR` 改为不可删除的 `/`，避免 bootstrap 提前启动的 execd 持有被替换的 `/workspace`；Agent 与 code-server 仍显式使用 `/workspace`。
- `scripts/sandbox-runtime-verify.sh`：新增镜像 WorkingDir 回归检查；`scripts/run-stack.sh` 同时加强旧监听进程清理失败的诊断。
- `apps/web/lib/*test.mjs`、`apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`、`apps/cli/internal/command/root_test.go`：覆盖 Origin 规则、Code readiness、平台环境变量所有权、稳定 cwd 以及部署配置生成。

关键取舍：不使用 `--trusted-origins '*'`；Origin 缺失或配置非法时 fail-closed；查看 Code 不触发 sandbox Acquire；基础设施进程使用稳定 cwd，用户执行语义通过显式 `/workspace` 保持不变。已有 sandbox 必须在新 runtime 镜像发布后重建才能生效。
