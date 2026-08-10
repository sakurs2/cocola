# fix: 中国下载源直接使用 OpenSandbox 官方阿里云镜像

- Change time: 2026-08-10 20:40 (+08:00)

## Reason

中国大陆云服务器使用 `cn-mirror` 启动 Cocola 时，南京大学 Docker Hub 代理对
`docker.nju.edu.cn/opensandbox/server:v0.1.14` 的 Manifest HEAD 请求返回 `403 Forbidden`，
导致 `cocola start` 在启动容器前失败。OpenSandbox 已在官方阿里云容器仓库发布相同版本，且实机
匿名拉取 Server 与 Egress 均成功，因此无需为这些官方中国镜像再增加一层第三方代理。

## Changes

- `apps/cli/internal/config/images.go`：中国下载源下的 OpenSandbox Server、Execd 和 Egress 统一使用
  OpenSandbox 官方阿里云仓库；直连源行为保持不变。
- `apps/cli/internal/config/images_test.go`：精确校验两种下载源生成的 OpenSandbox 镜像引用，防止中国
  下载源重新依赖第三方 Docker Hub 代理。
- `docs/cli.md`：说明中国模式下 OpenSandbox 镜像的直接来源。
- 取舍：不新增镜像配置项或自动回退逻辑，用户仍只选择一次下载源，供应链来源保持确定且可诊断。
