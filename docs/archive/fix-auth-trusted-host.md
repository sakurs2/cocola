# fix: 允许自部署入口通过 Auth.js Host 校验

- 变更时间：2026-08-02 15:44 (+08:00)

## 变更理由

生产部署启动成功后，用户通过默认 localhost、服务器 IP 或自有域名访问登录页时，Auth.js 会返回 `UntrustedHost`，页面仅显示服务端配置错误。Cocola 已将 Web 端口监听在所有网卡，但认证配置没有显式信任浏览器请求的 Host，导致登录链路在调用凭据校验前就被阻断。

## 变更内容

- `apps/web/auth.ts`：显式启用 Auth.js `trustHost`，使自部署实例能够使用实际的浏览器访问 Host。
- `apps/web/lib/auth-session-policy.test.mjs`：增加回归约束，防止可信 Host 配置被意外移除。

不新增用户配置项，不改变认证密钥、会话策略或管理员账号；现有 localhost、服务器 IP 和反向代理入口均使用同一套自部署行为。
