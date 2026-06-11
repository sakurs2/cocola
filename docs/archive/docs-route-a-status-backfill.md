# docs: 回填 Route A 落地状态（ADR-0009 Accepted + README 路线图）

让文档真相与代码真相对齐：Route A（大脑进沙箱）+ 真实模型 + Web 全链路已落地，
此前文档仍停在 Proposed / 未反映。

## 改动

- `docs/adr/0009-agent-runtime-in-sandbox.md`：
  - Status: Proposed -> Accepted（注明 Route A 已落地、Route B 保留 fallback）。
  - 新增「实现进展（2026-06-11）」小节：记录已落地的镜像 / 路由 / 凭证注入 /
    已验证项，并明确仍未做的硬化项（egress allowlist 未强制、Route-B 路径未删、
    gVisor/K8s 未开始）。

- `README.md`：
  - 当前里程碑 blurb 更新为「Route A 真实模型全链路打通」。
  - 路线图表新增 R-A 行（标记 ✅）。

## 测试

- 纯文档改动，无代码与行为变化。
