# feat(P1a Step1): proto Attachment 加 oss_key/size + 重生成 Go/Py stub

日期：2026-07-01

## 背景
P1a(附件 MinIO 真源 + 后端代 pull,详见 docs/plan/attachment-p1a-oss-source-of-truth.md)
的契约先行步:让 `Attachment` 既能内联小文件字节,也能只携带对象存储 key 由后端代 pull。

## 改动
- `packages/proto/cocola/agent/v1/agent.proto`:`message Attachment` 加两字段(旧序号不动):
  - `string oss_key = 4;`——对象存储 key(source of truth),每个上传都置。
  - `int64 size = 5;`——原始字节数,驱动阈值判定与日志。
  - 补充注释说明 push 送达 + 大小分流语义(小文件走 content、大文件仅 oss_key)。
- 重生成 gen:
  - Go(`agent.pb.go`,protoc-gen-go v1.34.2):新增 `OssKey`/`Size` 字段与 getter。
  - Python(`agent_pb2.py`/`.pyi`,grpc_tools):serialized descriptor 加 field 4/5,
    `.pyi` 的 `__slots__`/`__init__` 同步。

## 生成方式(环境备注)
- 宿主机 `go install`/`buf`、`pip` 均撞公司 TLS 拦截(OSStatus -26276),故:
  - Go stub:`golang:1.24` 容器内装 protoc-gen-go v1.34.2 + protoc-gen-go-grpc v1.5.1 +
    buf v1.47.2,`GOPROXY=goproxy.byted.org` 后 `buf generate`。
  - Python stub:沿用 `scripts/proto-gen-py.sh`(python:3.11-slim 容器 + byted PyPI 镜像)。
- 为保持 diff 聚焦,还原了工具版本 bump 带来的无关 churn:grpc stub(service 未变,仅
  protoc-gen-go-grpc v1.3.0→v1.5.1 版本头)、sandbox/common gen 文件全部 revert。
  最终改动仅 `agent.proto` + `agent.pb.go` + `agent_pb2.py` + `agent_pb2.pyi`。

## 校验
- gateway `go build ./...` 全绿;proto gen 包 `go build ./...` 全绿。
- agent-runtime `pytest -q`(.venv)→ 61 passed, 2 skipped,无回归。

## 非目标
- 本步只扩契约与 gen;gateway 落桶、阈值分流、agent-runtime 代 pull 是后续 Step2–4。
