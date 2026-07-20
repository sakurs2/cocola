# feat: 为 Code Server 预装标准语言扩展与工具链

- 变更时间：2026-07-20 12:27 (+08:00)

## 变更理由

Sandbox runtime 原先只提供基础 Code Server，缺少平台统一管理的语言扩展和扩展依赖的 LSP/格式化工具。用户打开代码后需要自行安装插件，版本不可控，Sandbox 回收后也无法保证能力一致；Python、Go、Java、C/C++、Shell、YAML 和 Markdown 等常见项目无法获得稳定的补全、诊断与格式化体验。

## 变更内容

- `deploy/sandbox-runtime/code-server-extensions.lock.json`：锁定 Code Server/Code、10 个 Open VSX 扩展，以及 gopls、clangd、ShellCheck、shfmt 和 JDK 的版本。
- `deploy/sandbox-runtime/install-code-server-extensions.sh`：在镜像构建期下载 VSIX、校验 SHA-256 与扩展 manifest，限制下载超时，并验证最终扩展全集与 lock 完全一致。
- `deploy/sandbox-runtime/Dockerfile`：安装锁定的语言工具，将工具目录加入 PATH，设置稳定的 `JAVA_HOME`，并执行扩展安装器。
- `deploy/sandbox-runtime/code-server-launch.sh`：只加载 root 管理的扩展目录；将可变的 Workbench 配置和 user data 放到 session 状态目录，避免 guest 更新平台扩展或争用 HOME 默认配置。
- `deploy/sandbox-runtime/runtime-manifest.json`、`deploy/sandbox-runtime/cocola_sandbox.py`：通过 guest CLI 暴露编辑器扩展目录、lock、语言工具目录和仅随 runtime 镜像更新的策略。
- `deploy/sandbox-runtime/shim/agent_shim.py`、`.github/workflows/sandbox-runtime-image.yml`、`scripts/sandbox-runtime-verify.sh`：把语言工具、扩展全集、目录所有权和 Code Server readiness 纳入本地与 CI 验证。
- `apps/agent-runtime/tests/test_cocola_sandbox_cli.py`：补充编辑器契约、精确扩展/工具集合和固定权限边界的测试。
- `deploy/sandbox-runtime/README.md`：记录平台标准插件、内置语言能力、工具目录和更新方式。
- 关键取舍：扩展与工具只在发布 runtime 镜像时更新，不新增运行时网络请求或环境变量；Markdown 同时提供 markdownlint 与 Markdown All in One，Java 复用并校验基础镜像内的 JDK 21。
