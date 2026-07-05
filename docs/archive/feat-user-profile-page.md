# feat: user profile page

- 变更时间：2026-07-05 21:15 (+08:00)
- 关联提交：待提交

## 变更理由

用户希望主页面中的用户入口可以进入个人 Profile 页面，用于查看基础个人信息。当前阶段不引入用户级设置，保留后续扩展空间。

## 变更内容

- apps/web/components/assistant-ui/app-sidebar.tsx：将侧边栏底部用户身份区域改为 Profile 入口，保留退出登录按钮。
- apps/web/app/profile/page.tsx：新增 Profile 页面，复用聊天主页面侧边栏并在右侧内容区展示姓名、用户名、邮箱、角色、用户 ID 和账号状态。

## 关键取舍 / 注意事项

- Profile 信息直接读取现有 Auth.js session，不新增后端 API。
- 本次不添加用户级 setting，也不增加可编辑表单。
- Profile 页面不脱离聊天主壳，点击用户入口后只替换右侧对话区域。
