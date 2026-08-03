# ci: Reuse unchanged sandbox runtime images

- 变更时间：2026-08-03 21:00 (+08:00)

## 变更理由

`cocola-sandbox-runtime` 包含浏览器、Agent CLI、编辑器和多种语言工具链，镜像体积明显大于其他
Cocola 服务。此前每个 Cocola tag 都会无条件重新构建该镜像，并让 CLI 拉取同版本的新标签；即使
Runtime 内容没有变化，也会增加发布时间、Registry 存储和用户对大镜像重复下载的担忧。

## 变更内容

- `.github/workflows/release.yml`：比较当前 release 与前一个 release 的
  `deploy/sandbox-runtime/` 内容。内容未变化且旧镜像存在时，通过旧镜像 digest 创建当前版本、
  minor 和 `latest` 同步标签；内容变化或旧镜像不可用时保留完整多架构构建流程。
- `.github/workflows/release.yml`：无论 Runtime 是新构建还是复用，均通过当前 release 标签执行
  匿名访问检查和 Runtime self-check。
- `scripts/tests/test_release_workflow.py`：增加 Release workflow 契约测试，覆盖复用判断、构建跳过
  条件和复用镜像自检。
- 关键取舍：保留 CLI 现有的同版本镜像引用与开发版 `latest` 行为，不增加用户配置，也不改变首次
  部署时提前拉取 Runtime 的行为。
