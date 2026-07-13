# feat: 增加统一 Cocola CLI 与无源码安装入口

- 变更时间：2026-07-12 21:31 (+08:00)

## 变更理由

正式部署此前依赖用户 clone 仓库并直接使用开发脚本。开发脚本同时承担 Native 调试、
容器编排、k3d 和兼容逻辑，无法提供稳定、简洁且可自动化的安装运维接口。用户期望
通过一条 `curl | sh` 安装，并获得彩色、美观、可自定义的统一 CLI。

## 变更内容

- `apps/cli`：新增独立 Go CLI，提供彩色品牌 UI、交互和无交互安装、JSON/无颜色
  输出、`up/down/restart/status/logs/doctor/version` 运维命令。
- `apps/cli/internal/assets/compose.yaml`：内嵌不依赖源码的正式 Compose，统一生成
  Secret、启动全套服务、内置 OpenSandbox，并支持完整校验的外部 OpenSandbox。
- `scripts/install.sh`：新增跨 linux/darwin、amd64/arm64 的 Release 下载、SHA-256
  校验与原子安装引导脚本。
- `.goreleaser.yml`、`.github/workflows/release.yml`：发布多平台 CLI 和同版本全套
  容器镜像，镜像成功后才创建 CLI Release；sandbox runtime 保留主干镜像流水线，
  tag 发布收口到统一 Release。
- `Makefile`、`go.work`：接入 CLI build/tidy/test。
- `cocola down`：按 checkpoint 安全顺序关闭服务，并清理 CLI 管理的动态 sandbox；
  单步失败仍继续执行剩余收尾，最终统一返回错误。
- 删除旧 `make prod`、`scripts/start.sh` 和源码正式 Compose；benchmark 使用小型
  override 暴露内部 gRPC 端口，observability 直接连接 CLI 创建的网络。
- `make dev`：终端输出收敛为 sandbox 准备、应用启动、Ready 和关闭四类信息；
  Helm/kubectl/k3d 明细追加到 `.run-logs/dev-setup.log`，Ready 只展示 Web、开发
  账号、日志目录和 Ctrl-C，错误仍直接显示并指向对应日志。
- 删除 `make dev` 启动期 `admin-mint` 和静态 `COCOLA_SANDBOX_LLM_TOKEN` 接线；
  Web 与 Scheduler 统一依赖 Gateway 的逐 Run 用户 token，Warm Pool 在 claim 前不携带
  共享身份，冷/热/reused sandbox 都在每次 shim exec 时注入真实用户 token。
- `docs/cli.md`、ADR-0021、README、配置文档：记录安装、运维、配置边界和技术取舍。

## 关键取舍

- CLI 只管理简单固定的正式 Docker Compose，不包装现有开发脚本，也不引入 Docker
  SDK、daemon 或新服务。
- `cocola install` 只生成并校验部署配置，`cocola up` 是唯一启动入口，避免安装过程
  直接启动服务而不给用户检查完整配置的机会。
- 首版不加入远程 SSH、Kubernetes 运维、升级迁移和备份恢复；后续按真实需求增加
  独立命令，不预留永久灰度开关。
