# feat: Auth.js whitelist user management

- 变更时间：2026-07-04 14:52 (+08:00)
- 关联提交：待提交

## 变更理由

Web 端需要从无鉴权模式升级为白名单账号体系：用户通过 Auth.js 登录，admin-api 作为账号、角色、密码哈希、运行时 token 的事实源。开发模式还需要稳定的 bootstrap admin，并且管理员后台需要支持创建、禁用、删除用户。后续排查发现 username/email 双登录需要统一 identifier 模型，Postgres join 查询需要避免歧义列，已删除/禁用账号也必须在后续请求中被即时拦截并跳回登录页。

## 变更内容

- apps/admin-api：新增 Auth 用户 service/http/store 能力，支持 username/email identifier 登录、bcrypt 密码、bootstrap admin、runtime token、账号启停、密码重置、软删除和审计。
- apps/admin-api：新增 `auth_users`、`auth_user_identifiers`、软删除迁移；Postgres 和 Memory store 保持同一 contract，删除用户不释放 username/email/identifier。
- apps/admin-api：bootstrap admin 标记为受保护账号，禁止降权、禁用、删除。
- apps/web：新增 Auth.js Credentials 登录、登录页、session provider、middleware、BFF 鉴权与 admin/user API 代理。
- apps/web：新增 `/admin/users` 管理页面，支持创建用户、切换角色、启停、重置密码、软删除确认弹窗；登录页支持密码显示/隐藏。
- apps/web：所有 Web BFF 请求就近校验账号状态，禁用或删除账号会返回登录页并显示账号不可用提示。
- apps/sandbox-manager：OpenSandbox metadata label 使用安全化用户标识，避免邮箱等字符导致 sandbox 创建失败。
- apps/web/components/assistant-ui/thread.tsx：模型下拉菜单宽度跟随按钮宽度，避免菜单条过长。
- README、scripts/run-stack.sh、docker compose：补充 Auth.js/admin-api/bootstrap admin 的本地开发配置和默认 dev 凭据打印。
- 测试：补充 admin-api service/http/store 覆盖用户登录、重复账号冲突、软删除、protected bootstrap admin、Postgres parity 和 runtime token 行为。
