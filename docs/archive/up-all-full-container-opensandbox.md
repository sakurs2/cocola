# make up-all 收敛为唯一的全容器 OpenSandbox 栈

日期: 2026-07-02
Plan: docs/plan/up-all-full-container-opensandbox.md

## 动机

`make up-all` 原先走 `scripts/run-stack.sh --all`(原生前台进程,`COCOLA_SANDBOX_ADDR`
默认空 → EchoProvider),名字暗示"全栈"实则不碰沙箱;而真正的全容器栈藏在
`scripts/start.sh` + `docker-compose.full.yml` 里。按"只有一条 route"的原则,把
`up-all` 收敛到这条已成型的全容器 Route A 路径,并让它在 provider=opensandbox 时
自动带起独立的 OpenSandbox server(不新增 up-route-a 之类分叉目标)。

## 改动

### scripts/start.sh
- 新增 `opensandbox_up` / `opensandbox_down` / `needs_opensandbox` / `sandbox_provider`
  辅助函数:仅当沙箱后端为 `opensandbox`(读环境或 .env)时,拉起/拆除独立 compose
  `docker-compose.opensandbox.yml`(宿主 :8090),并轮询 `/health` 就绪;provider=docker
  (DooD)时跳过。
- `up` / `--build` 在 full.yml `up -d` 前先 `opensandbox_up`;`--down` 在 full.yml
  `down` 后 `opensandbox_down`;`--stop` 同时停两个 compose。
- 头部注释补充 OpenSandbox server 联动说明;`--help` 输出范围 `2,16p` → `2,18p`
  以覆盖新增行后的完整用法块。

### Makefile
- `up-all` 目标:`bash scripts/run-stack.sh --all` → `bash scripts/start.sh`。
- 重写 up 系列头部注释:删除过时的 "real Claude Agent SDK path"(Route B 遗留话术),
  改为准确描述 —— `up`/`up-web` 原生 Echo 快调,`up-all` 全容器 Route A(含 OpenSandbox)。
- `up` / `up-web`(原生 Echo)保持不变。

## 影响

- `make up-all` 语义变更:从"原生 Echo、无沙箱"变为"全容器 Route A、真实模型、
  OpenSandbox 后端"。停止用 `bash scripts/start.sh --stop/--down`。
- 轻量调试路径 `make up` / `make up-web` 不受影响。

## 验证

- `bash -n scripts/start.sh` 通过;`make -n up-all` 解析为 `bash scripts/start.sh`。
- 端到端拉起 + chat 验收需用户本地执行(本环境禁止运行 docker/监听进程)。
