# docs: ADR-0008 持久化挂载改为"双卷 + 系统路径只读"(参考 Mira 模型)

- 变更时间: 2026-06-10 23:05 (Asia/Shanghai, UTC+8)
- 变更类型: docs(架构决策修订)

## 变更理由(用户诉求)

用户要求挂卷方案参考 Mira 的做法。经检索 Mira 内部资料确认其实际模型:
- NAS 挂载在 /opt/tiger/mira_nas/,分两类目录、生命周期不同:
  - userdata/{user_id}/  跨会话持久、按用户(含 secrets/ 子目录放 PAT/TOKEN/SSHKey)
  - workspace/{session_id}/  会话级
- 系统路径不可改写;~/files 默认不持久;持久需显式挂载。

对照发现原 ADR-0008 把 T1 定义为"纯容器 overlay 不持久"存在缺陷:在 Route A 的
scale-to-zero 休眠下,Pod 可能在会话仍存活时被销毁,纯 overlay 的会话工作区会在
每次休眠丢失中间文件。Mira 把会话工作区也挂盘(仅会话结束清理)规避了此问题。

## 变更内容(docs/adr/0008-persistence-layering-and-vault.md)

- §1 分层表:将 T1 拆为 T1a(进程级 overlay,随容器消亡)与 T1b(会话级,挂盘、
  跨容器休眠存活、会话结束清理);新增"为何拆分 T1"说明引用 Mira 模型。
- §2 重写为"双卷 + 系统路径只读(Mira 模型)":
  - 用户卷 cocola_user/{userID}/(T2,跨会话),~/.claude 绑定到此,含 secrets/
    子目录对接 Vault。
  - 会话卷 cocola_session/{sessionID}/(T1b),挂盘、休眠存活、会话结束清理。
  - 系统路径只读;默认不持久,持久必须显式挂载。
  - 卷标识 cocola:vol:user:{userID} / cocola:vol:session:{sessionID},绑定记入 Postgres。
- §3 生命周期:休眠/恢复改为"保留两卷 / 重挂两卷",明确开放会话的工作区可跨休眠存活。
- §4:把 runsc spike 从"build-out 前置 gate"调整为"生产 gVisor 切换前的验收 gate",
  明确先在普通 Docker(runc)打通链路、runsc 仅换隔离层。
- §6 存储后端 / §7 排期 / Consequences-Followups:同步为双卷(用户卷 + 会话卷)。

## 备注

ADR 仍为 Proposed。此修订对齐了"先本地 Docker 打通链路、gVisor 验收顺延"的落地
顺序,以及 Mira 的双目录持久化模型。
