# fix: 补齐 Gateway 镜像的数据库模块

- 变更时间：2026-07-26 18:30 (+08:00)

## 变更理由

Gateway 的 `go.mod` 通过 `replace` 引用仓库根目录下的 `db` 模块，Gateway 启动时也使用该模块内嵌的 Goose migration。原 Dockerfile 只复制 Gateway、go-common 和 Proto，容器内缺少 `/src/db`，导致 `GOWORK=off go build` 无法解析数据库模块。

## 变更内容

- `apps/gateway/Dockerfile`：在构建阶段增加 `COPY db ./db`。
- 更新构建上下文注释，明确 Gateway 同时依赖 db、go-common 和生成的 Proto 模块。
- 不改变最终运行镜像内容；migration 仍被编译进 Gateway 二进制，并在启动时按现有逻辑自动执行。
