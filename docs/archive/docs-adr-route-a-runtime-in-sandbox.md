# docs: ADR-0009 转向运行时进沙箱(Route A) + 重写 ADR-0008(持久化/生命周期/后端选型)

- 变更时间: 2026-06-10 22:10 (Asia/Shanghai, UTC+8)
- 变更类型: docs(架构决策)

## 变更理由(用户诉求)

用户提供了两份关键资料(CubeSandbox 介绍、与 Mira 关于服务端应用 Claude Agent
SDK 的内部探讨),要求暂停后续开发,先校验 cocola 当前架构是否合理。经过多轮讨论
对齐了以下结论:

1. cocola 的目标是企业自托管、员工 web 端即用、本地零安装。现状(Claude Code 在
   服务端)已满足该目标;用户原先担心的"员工本地要装 Claude Code"是对资料中
   "服务器要装"的误读。
2. 但现状是 Route B(大脑在中心化多租户 agent-runtime、双手经 in-process MCP 转发
   进沙箱),在企业多租户场景下隔离是"打补丁"而非"结构性",且存在安全洞:只设了
   allowed_tools、未禁用 Claude Code 原生 Bash/Read/Write,模型可能绕过 MCP 在
   共享宿主上执行不可信代码。
3. 用户拍板:① 方向转向 Route A(把 Claude Code 运行时打进每用户沙箱);
   ② 沙箱后端先定 K8s + gVisor(CubeSandbox 部署太重,作为可插拔备选);
   ③ 用户指出容器会挂载外部磁盘,使"容器关闭"与"数据丢失"解耦。

讨论还澄清了:Route A 下"命令执行完即释放容器"不可行(大脑有状态,与会话同生命
周期),空闲省资源靠 Pause/Resume 休眠;在 K8s 上用 scale-to-zero(销毁 Pod + 保
留 PVC)+ claude --resume 从磁盘 session 重建,替代 MicroVM 的内存级冻结。

## 变更内容

- 新增 docs/adr/0009-agent-runtime-in-sandbox.md(Proposed):
  - 决策采用 Route A,agent-runtime 退化为控制面路由器,不再自己 spawn claude。
  - 删除 MCP 转发缝合层(sandbox_tools.py),启用纯血原生工具全集。
  - 安全边界从"工具白名单"转为"网络 egress 管控"(沙箱仅可出网到 llm-gateway)。
  - CLI 预装进基础镜像(npm pack 离线包)。
  - 声明修订 ADR-0007、ADR-0004。
- 重写 docs/adr/0008-persistence-layering-and-vault.md(Proposed):
  - 三层持久化(T1 会话级 / T2 用户级 / T3 平台 secrets)。
  - T2 用外部挂载卷(K8s PVC)挂到容器 $HOME,使容器销毁不丢数据;~/.claude
    session 落盘,Resume 从磁盘重建,无需 MicroVM 内存快照。
  - 生命周期:懒启动 + 会话级绑定 + scale-to-zero 休眠 + warm pool。
  - 后端选型:K8s + gVisor 起步、CubeSandbox 可插拔备选、上线前做 runsc 兼容 spike。
  - Vault 接入(Sidecar/CSI,不自造轮子);存储后端 Postgres + MinIO + PVC。
  - 排期:M7 先在 Docker provider 用 bind-mount 验证模型,生产 PVC 形态随 M6,
    M7 不被 M6 阻塞。
- 更新 docs/adr/README.md 索引(0008 改名、新增 0009;补回此前缺失的 0007 行)。

## 备注

ADR 状态均为 Proposed,按 cocola ADR 规范应经 PR 评审后翻 Accepted。后续落地需:
重做 agent-runtime 为路由器、删除 Route-B 的 MCP 路径、构建 Route-A 基础镜像、
端到端落实 egress 白名单。
