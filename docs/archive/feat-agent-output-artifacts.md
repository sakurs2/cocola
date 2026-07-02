# feat: agent output artifacts

- 变更时间：2026-07-03 01:20 (+08:00)
- 关联提交：待提交

## 变更理由

用户希望 agent 能生成文件，用户可下载到本地；点击文件时在右侧侧边栏预览，无法预览的类型显示不支持预览。为避免暴露 sandbox 内部临时文件，v1 采用明确约定：只有 agent 写入 `./outputs/` 的文件会被发布为用户可见产物。

## 变更内容

- apps/agent-runtime：为 `./outputs/` 产物增加 turn 前后快照、二进制读取、对象存储上传和 `file` 事件发布；通过 system prompt 告知 agent 输出目录约定。
- apps/gateway：新增 artifact 元数据持久化、`file` part 聚合、鉴权下载接口，并在 SSE 转发前隐藏内部 `object_key`。
- apps/web：新增文件 part 渲染、同源 artifact 代理和右侧预览面板，支持图片、PDF 与文本类预览，不支持类型保留下载入口。
- db/migrations：新增 `artifacts` 表，记录对象存储 key 与会话/用户归属，支持历史会话下载。
