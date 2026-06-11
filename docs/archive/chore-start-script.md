# chore(scripts): 新增 start.sh 全栈一键启动脚本

封装 docker-compose.full.yml 的标准启停流程，固化两个易踩的坑，避免每次手敲
长命令出错。

## 改动

- `scripts/start.sh`（新增，可执行）：
  - 默认 `up`：镜像缺失时自动 build，再 `up -d`，轮询 gateway /healthz 就绪后
    打印各端口与 token 提示。
  - 子命令：`--build`（强制重建）/ `--stop`（保留数据）/ `--down`（删容器）/
    `--logs` / `--status` / `--help`。
  - 强制 `--env-file .env`（缺失即报错退出），杜绝回落 fake provider 的 echo。
  - 固定 `DOCKER_BUILDKIT=0`，绕开公司网络对 docker.io 的 TLS 拦截。
  - 启动前校验 Docker 守护进程可用。

## 说明

- dev 模式下 Web 端无需 token：gateway 已设 `COCOLA_AUTH_ALLOW_ANON=1`，
  空 token 视为 dev-user（已实测空 token 直连 /v1/chat 正常返回）。

## 测试

- `bash -n` 语法检查通过；`--status` 与幂等 `up`（栈在运行时）实测正常，
  正确打印端点信息。
