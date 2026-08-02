# fix: 同步 Storage 分页文案测试

- 变更时间：2026-08-02 22:25 (+08:00)

## 变更理由

Compose 单节点存储接入后，Admin Storage 页面把 Kubernetes 专属的 `PVCs` 用户文案改为
同时适用于 host volume 与 PVC 的 `volumes`，但源码契约测试仍匹配旧文案。Release workflow
因此在 Web 单元测试阶段失败，后续镜像和 CLI 构建任务被跳过。

## 变更内容

- `apps/web/lib/admin-pagination.test.mjs`：把 Session Storage 容量详情断言从 `PVCs`
  更新为页面当前使用的 `volumes`，保留对服务端分页总数展示的覆盖。
- 本次只同步过期测试，不修改 Storage 页面的产品行为或文案。
