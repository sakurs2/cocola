# fix(web): 抬高并配置化前端上传上限(P1a 收尾)

## 问题

P1a 已让 gateway 落 MinIO + 按 16MiB 阈值分流、agent-runtime 代 pull 大文件,但前端
`base64-attachment-adapter.ts` 仍是 P0 写死的 `MAX_BYTES = 1MiB`,导致传 19.1MB 文件在
`add()` 阶段就被前端拦下报 "the 1 MB inline limit is exceeded",大文件路径根本触达不到
gateway。这正是 ADR-0017 记的 TODO「前端上限对齐同一配置源」。

## 改动

- `apps/web/lib/base64-attachment-adapter.ts`:
  - 移除写死的 1MiB;改为 `NEXT_PUBLIC_ATTACHMENT_MAX_BYTES` 读取,非法/缺省回落
    **32MiB**(高于 16MiB 分流阈值,保证内联与后端代 pull 两条路都能从 UI 触达)。
  - 更新注释澄清:此值是「客户端内联 base64 走 client→gateway JSON 单跳」的硬上限,
    **不是**小/大分流阈值(分流在 gateway,按 `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`);
    真正解除该上限的客户端 presign 直传 OSS 属 P1b/TODO。
- `deploy/docker-compose/docker-compose.full.yml`:web 服务注入
  `NEXT_PUBLIC_ATTACHMENT_MAX_BYTES`(默认 33554432);注明 NEXT_PUBLIC_* 构建期内联,
  改动需重建 web 镜像。
- `.env.example`:文档化该前端上限变量与其「非分流阈值」语义。

## 校验

`apps/web` `tsc --noEmit` 全绿。

## 备注

Next.js App Router 的 route handler 以流式转发 body(无老式 1MB body-parser 上限),
gateway 用 `json.NewDecoder(r.Body)` 也未设 `MaxBytesReader`,故链路上唯一的卡点就是
前端这道 `add()` 校验;抬高后 19MB 文件可正常走「大文件→仅 oss_key→后端代 pull」。
