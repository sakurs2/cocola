# feat: Route-A 沙箱运行时镜像（Dockerfile + stdio shim + 本地验证脚本）

- 变更时间：2026-06-10 23:25 (+08:00)
- 关联提交：<待补>

## 变更理由

ADR-0009 决定转向 Route A：把整个 Claude Code 运行时（Node + claude CLI +
claude-agent-sdk + shim）打进每用户沙箱镜像，大脑和双手都在用户自己的容器里跑。
ADR-0008 进一步定型了持久化（双卷）/生命周期（懒启动+会话绑定+休眠）/后端
（先 K8s+gVisor，先在本地 Docker bind-mount 上验证）。

本次按用户要求「先开始 Dockerfile + 验证脚本」，落地 Route A 的第一块可运行物料，
并参考了《Claude code 云端部署 & 魔改 sdk 实现 http 流式调用》教程里的可复用模式
（npm pack 离线 tgz、Node 22、Python 3.11/uv、claude-agent-sdk>=0.1.60、
bypassPermissions 默认、sync 会话函数注意点）。

关键取舍：教程用 FastAPI+SSE 在容器内 :8765 起 HTTP 服务；cocola 有「沙箱绝不
监听端口」的硬约束，因此 shim 改为 **stdio 驱动**（一次 JSON 请求 → NDJSON 事件流
→ 退出），控制面通过 `docker exec -i` / `kubectl exec -i` 喂 stdin、收 stdout。

## 变更内容

- deploy/sandbox-runtime/Dockerfile：基于 ubuntu:22.04，装 Node 22（Claude Code
  运行时）+ Python 3.11 + uv + claude CLI（优先用 offline/*.tgz 离线安装，否则走
  registry）+ claude-agent-sdk；非 root 用户 cocola；预置两卷挂载点
  /home/cocola/.claude（T2 用户卷，CLAUDE_CONFIG_DIR 指向此处）与 /workspace
  （T1b 会话卷）；ENTRYPOINT 用 tini 收割 claude 子进程，默认 sleep 保活
  （会话级存活）。构建参数化（NODE_MAJOR / PYTHON_VERSION / NPM_REGISTRY /
  CLAUDE_AGENT_SDK_SPEC）以便 CI 切镜像源。
- deploy/sandbox-runtime/shim/agent_shim.py：沙箱内 stdio shim。读取 stdin 一个
  JSON 请求 → 调 claude_agent_sdk.query() → 把 SDK 消息映射成 NDJSON 事件
  （start/text/thinking/tool_use/tool_result/result/done），事件分类与
  agent-runtime 的 claude_sdk_provider.py 对齐，便于 router 直接透传。鉴权/路由
  全部从容器 ENV 读，不从请求读（凭据不走 prompt 通道）。跑满血原生工具集，无
  MCP 转发、无 disallowed_tools；permission_mode 默认 bypassPermissions（沙箱内
  没有人来点确认，边界靠容器+egress allowlist）。带 `--selfcheck` 离线自检，输出
  一行 JSON（node/claude_cli/sdk/config_dir/workspace/auth 是否就绪）。
- deploy/sandbox-runtime/shim/entrypoint.sh：稳定入口路径的瘦启动器，透传
  stdin/stdout/stderr 与退出码、转发参数。
- deploy/sandbox-runtime/offline/.gitkeep：放离线 npm pack tgz 的占位目录。
- deploy/sandbox-runtime/README.md：用法、stdio 协议、双卷持久化、构建与验证说明。
- scripts/sandbox-runtime-verify.sh：本地 Docker(runc) 端到端验证；同一脚本体即
  未来 runsc(gVisor) 验收 spike（改 DOCKER_RUNTIME=runsc 即可）。覆盖 4 步：
  build → selfcheck(无网) → 真实 query（打 gateway 出网 + 容器内原生 bash 写
  /workspace/proof.txt）→ 双卷持久化（销毁+重建容器后 ~/.claude 仍在、
  `--resume <session_id>` 恢复上一轮上下文）。shim 一律走 `docker exec -i` stdio，
  不监听端口。支持 SKIP_BUILD / SKIP_QUERY 开关。

## 验证

- bash -n、python -m py_compile 全通过。
- 宿主机跑 `agent_shim.py --selfcheck`：正确输出 JSON，并因宿主无 SDK 返回 exit=1
  （符合预期，镜像内会装上 SDK）。
- 完整镜像构建 + runc 端到端验证需在装有 Docker 的机器上执行；runsc(gVisor)
  spike 需 Linux+gVisor 宿主，按 ADR-0008 作为生产切换前的验收门，本地链路先行。

## 修复(测试回归)

测试期间 Tier-2/3 出现 "8 passed, 2 failed"：模型回 "I don't have a Bash
tool available"，且把 cwd 误报成宿主 NAS 路径 `/Volumes/users/han-o/...`。
根因不在我们的代码，而在 SDK 改名后的破坏性默认：

- claude-agent-sdk 已从 0.1.x 演进到 **0.2.95**（`>=0.1.60` 实际解析到
  0.2.x；旧记的"0.2 not on PyPI"作废）。Renamed Claude *Agent* SDK 不再隐式
  启用 Claude Code 的默认行为：`tools`/`system_prompt` 都不设时，SDK 会传
  `--system-prompt ""`（抹掉默认 prompt 连同注入真实 cwd 的 `<env>` 块）且
  **不带任何 tools preset**——于是模型既没有 Bash/Read/Write 原生工具，也丢了
  对 /workspace 的环境感知，退化成被动问答并臆造宿主路径。

修复(agent_shim.py `_build_options`)：

- `kwargs["tools"] = {"type":"preset","preset":"claude_code"}` —— 显式拿回
  完整 Claude Code 工具集（CLI 实际收到 `--tools default`，含 Bash）。
- system_prompt 默认走 `{"type":"preset","preset":"claude_code"}`；调用方
  自带 prompt 时改用 `append` 字段挂在 preset 之后，避免再次截断 `<env>`。
- 用 SDK 0.2.95 的 `SubprocessCLITransport._build_command()` 实测校验：新命令
  含 `--tools default` 且不再出现空 `--system-prompt ""`；旧命令对照确认是
  空 prompt + 无 tools。

- Dockerfile：`CLAUDE_AGENT_SDK_SPEC` 下限由 `>=0.1.60` 抬到 `>=0.2.0`
  （preset dict 形态需要 SDK ≥0.2）。

## 已知阻塞(转下一个独立 feature)

Tier-3 的两项(native bash 写 proof.txt、--resume)当前无法通过,根因不在
sandbox-runtime,而在 **llm-gateway 仍是 M3 的纯文本中继**,tool-use 协议被整段
截断:

- anthropic_codec.py:`AnthropicRequest` 无 `tools`/`tool_choice` 字段
  (extra:ignore 丢弃);`_flatten_content` 把 tool_use/tool_result block 压成
  text(代码注释明示 "known M3 limitation")。
- types.py:`ChatMessage.content` 是 str;`StreamEventType` 只有 text 事件,
  没有 tool_use / input_json_delta。
- upstream/anthropic.py:`_build_payload` 转发给上游的 payload 不含 tools;
  `_parse_stream` 不解析上游回传的 tool_use block。

证据:容器内 claude CLI 已正确发出工具定义(shim 修复后),但模型回
"I don't have a Bash tool",且真实计费 $0.003275 —— 请求打到了上游,只是
tools 在 gateway 这层被丢了。上游代理已确认支持 tool-use,故只需 gateway 补
全链路透传即可解锁。

下一步:新开 feature(建议配套新 ADR)实现 gateway 的 tool-use 双向透传:
入站 codec 增 tools/tool_choice + 保留 content-block;归一化模型(types.py)
升级 content 为 block 数组 + 增 tool_use 流事件;upstream 透传 tools 并解析
tool_use/input_json_delta;出站 SSE 能输出 tool_use content block(含 partial
JSON)。完成后 Tier-3 应自动转绿。
