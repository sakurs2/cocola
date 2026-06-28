# Changelog: Plan — OpenSandbox 双卷文件系统映射

日期: 2026-06-28
关联: task #26、ADR-0008、ADR-0014、ADR-0002

## 背景
opensandbox provider 当前 Create 不传任何 volume,ADR-0008 的「用户持久 /
会话工作区 / 系统只读 / 默认不持久」四条语义在 OpenSandbox 后端完全缺失。
本次只提交设计 Plan(不含编码),把映射方案与关键决策固化,供评审后实现。

## 改动
- 新增 `docs/plan/opensandbox-volume-mapping.md`:
  - 映射表:用户长期文件(T2)->pvc `cocola-user-<userID>` @ `/data/userdata/<userID>`;
    Claude 配置/会话(T2)-> 同一用户卷 `subPath=.claude` @ `/home/cocola/.claude`;
    会话工作区(T1b)->pvc `cocola-session-<sessionID>` @ `/workspace/<sessionID>`;
    平台 skill->pvc `cocola-plugins`(共享, readOnly) @ `/data/plugins`。
  - 关键决策:全用 `pvc` 后端(本机 docker 即 named volume,避开 host allowlist);
    `.claude` 合入用户卷而非另开卷;T1b 不用 deleteOnSandboxTermination(cocola GC);
    不接 ossfs、不用 snapshot resume、不动 docker provider。
  - 决策记录:为什么会话工作区不合进用户卷(生命周期/GC、并发挂载 RWO、配额备份三点)。
  - 两个真 server 实测项:Docker named volume 多 subpath 挂载、`~/.claude` uid=10001 写权限。

## 不含
本次仅文档。provider 编码(volumes wire 类型、mapVolumes、单测、e2e)在 task #26 实现阶段提交。
