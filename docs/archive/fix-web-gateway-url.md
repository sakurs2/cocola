# fix(web): 修复 Web 端 "gateway unreachable: fetch failed"

Web 的服务端 SSE 代理（apps/web/app/api/chat/route.ts）读 `COCOLA_GATEWAY_URL`
转发到 gateway，但 compose 从未设该变量，回落默认 `http://127.0.0.1:8080`。
在 web 容器内 127.0.0.1 指向容器自身，到不了 gateway 容器，故 fetch failed。
（compose 里原本只设了 NEXT_PUBLIC_GATEWAY_URL，那是给浏览器侧用的，route.ts
不读它。）

## 改动

- `deploy/docker-compose/docker-compose.full.yml`：web 服务新增
  `COCOLA_GATEWAY_URL: "http://gateway:8080"`，让服务端代理用 compose 网络内
  的服务名访问 gateway。

## 测试

- 容器内 `fetch http://gateway:8080/healthz` 返回 OK。
- 经 web 同源代理 `POST http://localhost:3000/api/chat` 实测真实模型回复
  （"法国的首都是巴黎"），event 流 text/result/done 正常。
