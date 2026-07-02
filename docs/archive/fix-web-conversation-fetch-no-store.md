# Fix: 对话历史侧边栏永远为空 —— Next.js fetch Data Cache 缓存了早期空响应

## 现象
后端已确认正确:gateway `/v1/conversations` 直连(dev 匿名身份)返回 10 条对话,
`/v1/conversations/{id}/messages` 返回对应消息。但 web 界面侧边栏始终为空,
重启服务、清缓存均无效。

## 根因
web 的同源代理路由 `apps/web/app/api/conversations/route.ts`(及 `[id]/messages`)
里的 `fetch()` **未设 `cache` 选项**。Next.js 14 默认把 `GET` fetch 的响应写入
持久化 **Data Cache**。列表还为空(首次对话落库之前)时那个 `[]` 响应被缓存,
之后即便 gateway 返回新数据,代理仍吐旧的空数组 → 侧边栏永远空。

关键陷阱:路由已声明 `export const dynamic = "force-dynamic"`,但它只影响**路由
渲染缓存**,**不覆盖内部 `fetch` 的 Data Cache**,二者相互独立。

佐证:修复前 web.log 里代理响应 ~4ms(命中缓存);加 `no-store` 后真实往返 13–177ms,
且立即返回 10 条。

## 变更
- `apps/web/app/api/conversations/route.ts` —— 代理 `fetch` 增加 `cache: "no-store"`。
- `apps/web/app/api/conversations/[id]/messages/route.ts` —— 同样增加 `cache: "no-store"`
  (消息历史同理会被冻结在首次抓取状态)。
- 两处补充注释说明为何 `dynamic:"force-dynamic"` 不足以关闭 fetch 缓存。

## 校验
- 经 web 代理(3000):`GET /api/conversations` → 200,10 条;
  `GET /api/conversations/{first}/messages` → 200,2 条消息。
- gateway 直连结果与代理一致,断层消除。

## 回滚
`git revert` 本次提交即可。
