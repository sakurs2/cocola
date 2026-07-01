# fix: 侧边栏收起后 toggle 按钮被挤出可视区,无法再次展开

日期：2026-07-01 · 关联：feat-web-ui-openwebui-look.md(Open WebUI 观感改造)

## 现象
侧边栏点击折叠后,展开按钮消失,无法再次展开。

## 根因
折叠态侧边栏宽度为 `w-[3.25rem]`(52px),但 header 行在折叠时仍渲染
logo 方块(`size-7` + `shrink-0`,28px)+ `gap-2`(8px)+ toggle 按钮(28px),
加上 `px-3` 两侧 padding(24px)合计约 88px,远超 52px。整行不换行且 logo 为
`shrink-0` 不收缩,导致 toggle 按钮被挤出侧边栏可视区,既不可见也不可点。

## 改动(apps/web)
- `components/assistant-ui/app-sidebar.tsx` —— header 折叠态隐藏 logo 与 "cocola"
  文字(用 `!collapsed &&` 包裹),行容器切为 `justify-center px-0` 使仅存的
  toggle 按钮居中;toggle 按钮补 `shrink-0` 防止再被压缩。展开态外观不变。

## 校验
- lint PASS(next lint,无告警)
- build PASS(146 kB / 233 kB,与改造版一致)

## 非目标
未改动后端、SSE 契约、runtime 适配器;侧边栏仍为静态装饰壳子。
