# docs: P1a Step6 — ADR-0017 回填 P1a 已落地 + 端到端验收清单

附件 P1a(ADR-0017)第 6 步收尾:把 Step1–5 的落地状态回填进 ADR,并给出全栈
端到端验收清单(实际 `make up-all` 冒烟由使用者在本机跑——沙箱内不起监听进程)。

## 改动

- `docs/adr/0017-attachment-storage-and-sandbox-delivery.md`:
  - Followups 里 **P1a 标记 ✅ 已落地(2026-07)**;P1b(阈值分流)已并入 P1a 实现,
    不再单列;补前端上限接入同一配置源为 TODO。
  - 新增「P1a 实现纪要(2026-07)」段:逐步(proto → gateway objstore → 落桶分流 →
    agent-runtime 代 pull → compose/env)记录落地范围与优雅降级姿势,与本 ADR 决策对齐。

## 端到端验收清单(使用者在本机执行)

前置:仓库根 `.env` 已配真实 LLM 上游(`COCOLA_LLM_PROVIDER=anthropic` + BASE_URL/
API_KEY/MODEL);MinIO 走 full compose 内置默认(无需额外配置)。

1. `make up-all` 起全栈;确认 `minio` healthy、`minio-init` 成功退出(建好 `cocola` 桶)。
2. **小文件**(< 16MiB,如一个几 KB 的 .txt/.py):聊天里附上并提问其内容。
   - 期望:模型能读到文件内容作答。
   - 佐证真源:MinIO 控制台(http://localhost:9001,cocola/cocola_dev_pw)`cocola` 桶下
     `attachments/<session>/<uuid>-<name>` 存在;gateway 日志无 Put 失败告警。
3. **大文件**(> 16MiB;可 `head -c 20000000 /dev/urandom | base64 > big.txt` 造一个,
   或直接传一个 >16MiB 文件):聊天里附上并提问。
   - 期望:模型仍能读到文件(agent-runtime 代 pull 生效);请求体不因内联膨胀。
   - 佐证:gRPC 该附件 `content` 为空、仅带 `oss_key`;agent-runtime 日志显示
     object-store fetcher enabled + 该 key 被 GetObject;桶内对象存在。
4. **阈值可配置验证**(可选):`COCOLA_ATTACHMENT_INLINE_MAX_BYTES=1024` 重启 gateway,
   传一个 2KB 文件,确认它这次走大文件路径(仅 oss_key + 代 pull)。
5. **降级验证**(可选):不配 `COCOLA_MINIO_*`(或停 minio),确认小文件仍走 P0 内联可用,
   大文件浮为干净 `error` 事件而非崩溃。

## 单测/校验现状(已在沙箱内跑绿)

- agent-runtime:`.venv/bin/python -m pytest` → `71 passed, 2 skipped`;ruff 全绿。
- gateway:`golang:1.24` 容器经 byted 代理 build/vet/test 全绿(见 Step2/3 changelog)。
- compose:`yaml.safe_load(full.yml)` 通过,minio/minio-init 就位,两服务同源注入
  `COCOLA_MINIO_*` + 阈值并 gate 在 minio-init 完成后。

## 状态

P1a(真源=MinIO、送达=push、大文件后端代 pull、阈值可配置)代码 + 编排 + 文档全部落地,
待使用者跑上面的全栈冒烟做最终确认。后续:历史附件回看、presign 直传、前端上限对齐配置源、
P2 工具型 pull——均已在 ADR-0017 记为 TODO。
