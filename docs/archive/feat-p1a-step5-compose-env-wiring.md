# feat(deploy): P1a Step5 — compose/env 接线 MinIO 真源

附件 P1a(ADR-0017)第 5 步:把前四步的对象存储真源接进全栈编排,让
`make up-all`(full compose)零配置即拥有 MinIO,并把 `COCOLA_MINIO_*` +
分流阈值注入 gateway(上传方)与 agent-runtime(代 pull 方)。

## 改动

- `deploy/docker-compose/docker-compose.full.yml`:
  - 新增 `minio` 节点(minio/minio:latest,`server /data`,root 凭据可经
    `COCOLA_MINIO_ROOT_USER/PASSWORD` 覆盖,宿主端口 9000/9001 可覆盖,healthcheck
    命中 `/minio/health/live`,数据卷 `miniodata`)。
  - 新增 `minio-init` 一次性节点(minio/mc):`mc mb --ignore-existing
    local/<bucket>` 建附件桶,`depends_on minio: service_healthy`。对齐 dev
    compose 既有姿势。
  - gateway 环境注入 `COCOLA_MINIO_ENDPOINT`(默认 `minio:9000`,网络内 host:port)
    /`ACCESS_KEY`/`SECRET_KEY`/`BUCKET`/`USE_SSL` + `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`
    (留空回落 16MiB);`depends_on` 追加 `minio-init: service_completed_successfully`。
  - agent-runtime 环境注入同一组 `COCOLA_MINIO_*`(同源命名,读侧只需连桶取字节);
    `depends_on` 追加 `minio-init`。
  - volumes 增 `miniodata`。
- `.env.example`:把原先未被任何代码消费的 `S3_*` 占位块替换为文档化的
  `COCOLA_MINIO_*` + `COCOLA_ATTACHMENT_INLINE_MAX_BYTES` + MinIO 根凭据/宿主端口段;
  说明真源模型、同源命名、`_FILE` 间接、阈值可配置(默认 16MiB,不写死)。

## 设计要点

- **默认开箱即用**:full 栈默认变量即指向内置 minio,`make up-all` 后附件走真源 +
  阈值分流;不设 `COCOLA_MINIO_*` 则 gateway 回落 P0 纯内联(feature dark),
  agent-runtime 无 fetcher 时"仅 oss_key"附件浮为干净报错。
- **同源命名**:gateway 与 agent-runtime 共用一套 `COCOLA_MINIO_*`,上传/代 pull
  连同一桶;secret 支持 `_FILE` 间接(ADR-0008),Vault-ready。
- dev compose 早已具备 minio/minio-init,本步只补 full compose 缺口 + env 文档。

## 校验

`python -c "yaml.safe_load(full.yml)"` 通过;services 增 minio/minio-init,
gateway+agent-runtime depends_on 含 minio-init,两者 `COCOLA_MINIO_ENDPOINT` 就位;
volumes 含 miniodata。

## 下一步

Step6:`make up-all` 起全栈,小/大文件各传一次端到端验收(由使用者跑,沙箱内不起
监听进程),回填 ADR-0017 "P1a landed" + 收尾 changelog。
