# Changelog: 修复 OpenSandbox volumes[*].name 必填 + 真 server 卷持久化验收

日期: 2026-06-28
关联: task #26、docs/plan/opensandbox-volume-mapping.md

## 背景
上一提交(36cf84a)实现卷映射后,首次对本机真 OpenSandbox server 跑
`opensandbox-verify -persist`,server 返回 422:`body.volumes[*].name Field required`。
stub 单测覆盖不到服务端必填校验,真 server 一跑即暴露。

## 改动
- `opensandbox.go`:`volumeSpec` 增必填 `Name string \`json:"name"\``;`mapVolumes`
  为 4 个卷赋稳定 name(user/claude/session/plugins)。同一 PVC 被挂两次(user 卷根 +
  .claude subPath)时,ClaimName 相同但 Name 不同,满足 server 的 per-request 唯一性。
- `opensandbox_test.go`:`TestMapVolumes` 增「每卷 Name 非空且唯一」断言。

## 真 server 验收(本机 cocola-opensandbox-server,:8090,docker runtime)
`go run ./cmd/opensandbox-verify -persist -skip-pause` 全绿(VERIFY OK):
- 6d 用户卷 `/data/userdata/<uid>/marker` 跨 destroy-recreate 读回。
- 6e `.claude`(同用户卷 subPath)写入成功并读回 —— 同时验证:
  ① Docker named volume 多 subPath 挂载可用(此前担心会推翻 .claude 合卷方案,现确认成立);
  ② ~/.claude 的 uid 写权限不卡(无需额外 chown 钩子,沙箱用户可直接写)。
- 6f 会话工作区 `/workspace/<sid>/marker` 跨 destroy-recreate 读回。

至此 Plan 的两个真 server 实测子项全部通过,卷映射方案落地完成。
