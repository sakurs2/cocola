# fix: 让 env example 可直接启动本地栈

- 变更时间：2026-07-12 16:01 (+08:00)

## 变更理由

配置归一化后，`.env.example` 仍保留未被启动脚本读取的 `POSTGRES_*` / `REDIS_*`
变量，同时缺少全容器 Web 登录需要的 Auth.js secret、Admin key 和初始管理员配置。
直接复制虽然能启动 dev 进程，但不能保证 dev/prod 都具备完整可登录的本地配置。

## 变更内容

- `.env.example`：删除无效的基础设施别名，补齐本地鉴权和初始管理员默认值，并
  明确 dev/prod 网络地址不能照搬 localhost。
- `README.md`：把 `cp .env.example .env` 纳入快速启动步骤，标注默认管理员和生产
  密钥要求。
- `docs/configuration.md`：记录 example 的复制、升级合并和模型加密密钥稳定性约束。

关键取舍：不新增配置加载器或兼容别名；启动脚本继续负责本地基础设施默认值，
example 只暴露真实生效且用户需要持久保存的配置。
