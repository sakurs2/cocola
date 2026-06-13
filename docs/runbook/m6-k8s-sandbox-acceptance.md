# M6 验收 Runbook：K8s 沙箱端到端验收(Layer C)

> 适用对象:一台 **Linux 云服务器**(最贴近生产,本仓首选路径),在其上用
> **k3s**(版本 1.35.5 实测)搭一套单机 Kubernetes。
>
> **默认隔离 = 普通 runc + Kubernetes 用户命名空间(`hostUsers: false`)**:
> 容器 root 被映射成宿主机上的非特权 uid,**节点零安装**——不需要装 gVisor、
> 不需要改 containerd、不需要嵌套虚拟化。这是自托管最省事的路径。
> gVisor(`runsc` RuntimeClass)是**可选增强**(更强的用户态内核隔离),
> 单独放在文末「附录 A」,默认验收**不依赖**它。
>
> 目标:跑通 ADR-0008/ADR-0009 的验收门——大脑可运行、egress 被锁定、
> 原生 bash/file IO 正常、删 Pod 重挂 PVC 后 `claude --resume` 能续接上下文。
>
> Layer A/B(代码 + 单测 + 部署物静态校验)已在本地完成并合入;本文件覆盖
> **必须在真集群上人工跑一遍**的 Layer C。每一步给出可直接复制的命令与期望输出。

---

## 0. 名词与前置

- **控制面命名空间**:`cocola`(`sandbox-manager` 等)。
- **沙箱命名空间**:`cocola-sandboxes`(用户 Pod / PVC / NetworkPolicy)。
- 沙箱 Pod 名:`cocola-<sandbox-id>`;binding ConfigMap:`cocola-bind-<sid>`;
  egress 策略:`cocola-egress-<sid>`(详见 `deploy/k8s/README.md`)。

前置清单(任一不满足都会卡在 `Pending`/`ContainerCreating`):

1. 集群 **v1.33+**(用户命名空间自 1.33 起默认开启)。k3s 1.35.5 满足。
   `kubectl` 已指向目标 context(`kubectl config current-context`)。
2. **节点无需任何隔离相关安装**——默认路径只用 runc + 用户命名空间,这两者
   k3s 1.35.5 开箱即有。
3. **CNI 强制 NetworkPolicy**。**k3s 默认即满足**:它在 flannel 之外自带一个
   kube-router 的 NetworkPolicy 控制器,NetworkPolicy 开箱强制(域名级 allowlist
   仍需 DNS-aware CNI 如 Cilium;纯 CIDR/IP 在 k3s 默认下即可)。
4. 控制面依赖(redis / llm-gateway)已按 `04-sandbox-manager.yaml` 里的
   in-cluster DNS 名就绪。

### 在 Linux 云服务器上搭 k3s(单机即可,推荐)

1. **装 k3s**(自带 containerd + flannel + kube-router NetworkPolicy 控制器):
   下载官方安装脚本(get.k3s.io),先通读再执行(不要直接管道进 shell)。
   装完确认:`kubectl get nodes` 为 Ready;`kubectl version` 服务端 ≥ 1.33;
   NetworkPolicy 默认强制,无需换 CNI、无需改 containerd。

2. **确认用户命名空间可用**(k3s 1.35.5 默认开启,通常无需任何操作):
   ```sh
   # 服务端版本 >= 1.33 即默认带 UserNamespacesSupport;k3s 用 containerd,
   # 现代内核(>= 5.19,建议 6.x)即满足 idmap 挂载等依赖。
   kubectl version -o json | grep gitVersion
   uname -r        # 期望 >= 5.19,生产建议 6.x
   ```
   > 若内核过旧导致 `hostUsers: false` 的 Pod 起不来,把
   > `COCOLA_K8S_HOST_USERS` 设为 `default`(交给集群默认)或升级内核。

3. **KUBECONFIG**:指向 k3s 默认 kubeconfig(k3s 写在 rancher 配置目录下的
   `k3s.yaml`;远程操作时把其中 server 地址改成云服务器公网/内网 IP)。

> 备选(非云服务器场景):minikube `--container-runtime=containerd`;
> 任意 v1.33+ 托管集群(EKS/GKE/AKS)均可,用户命名空间为 K8s 原生能力。

---

## 0.5 在云服务器上 build 镜像并导入 k3s

k3s 用的是**自带 containerd**(不是 docker),`docker build` 出来的镜像 k3s 默认
看不到——必须显式导入。两条必备镜像:沙箱运行时 `cocola/sandbox-runtime:dev`
(大脑 + claude CLI)与控制面 `cocola/sandbox-manager:dev`;真实 query 还要
`cocola/llm-gateway:dev`。

> 前提:云服务器已装 git + docker(或 nerdctl)。仓库 clone 到服务器上,
> **下面命令都在仓库根目录执行**(sandbox-manager 的 Dockerfile 要求 context=根)。

```sh
# 1) 沙箱运行时镜像(context = deploy/sandbox-runtime)
#    可选:先 vendor 离线 CLI tgz 放到 deploy/sandbox-runtime/offline/ 以免装包走网
docker build -t cocola/sandbox-runtime:dev deploy/sandbox-runtime

# 2) 控制面 sandbox-manager(context 必须是仓库根,因 go.mod 用相对 replace)
docker build -f apps/sandbox-manager/Dockerfile -t cocola/sandbox-manager:dev .

# 3) llm-gateway(真实 query 上游)
docker build -f apps/llm-gateway/Dockerfile -t cocola/llm-gateway:dev .
```

把镜像导入 k3s 的 containerd(二选一):

```sh
# 方式 A:docker save -> k3s ctr import(最通用)
for img in cocola/sandbox-runtime:dev cocola/sandbox-manager:dev cocola/llm-gateway:dev; do
  docker save "$img" | sudo k3s ctr images import -
done
sudo k3s ctr images ls | grep cocola      # 期望看到三个 cocola/* 镜像

# 方式 B:若用 nerdctl 直接构建到 k3s 的 containerd(namespace k8s.io),可跳过导入
#   sudo nerdctl --namespace k8s.io build ...
```

> 架构对齐:云服务器是 x86_64 时,以上 build 天然产出 amd64 镜像,与节点匹配。
> 清单里 `imagePullPolicy: IfNotPresent`,镜像已在本地 containerd 即不会去远端拉。
> (镜像也可放进 k3s agent 的 `images/` 目录让 k3s 启动时自动导入。)

---

## 0.6 部署测试依赖(redis + llm-gateway)

`deploy/k8s/*` 按设计只含**沙箱平面**;真实 query(§3.2)需要上游网关。
`deploy/k8s/05-deps-redis-llm-gateway.yaml` 把 `redis` 与 `llm-gateway` 部署到
`cocola` 命名空间,Service DNS 正好对上 `04-sandbox-manager.yaml` 里引用的
`redis.cocola.svc...:6379` 与 `llm-gateway.cocola.svc...:8080`。

```sh
kubectl apply -f deploy/k8s/05-deps-redis-llm-gateway.yaml
kubectl -n cocola rollout status deploy/redis
kubectl -n cocola rollout status deploy/llm-gateway
```

- **不配真实模型**:llm-gateway 默认 `COCOLA_LLM_PROVIDER=fake`,返回固定应答
  ——足以验证 Route-A **网络通路**(沙箱 -> service DNS -> 网关 -> 回包)。
- **要验真实模型回复**:创建 `cocola-llm` Secret 后重启网关(见 §5.5)。

---

## 1. 部署沙箱平面

`04-sandbox-manager.yaml` 默认即 runc + 用户命名空间:
`COCOLA_K8S_RUNTIME_CLASS=""`(空 -> 不写 RuntimeClassName,回落 runc)、
`COCOLA_K8S_HOST_USERS="false"`(启用用户命名空间)。**无需** apply
`01-runtimeclass.yaml`(那是 gVisor 专用,见附录 A)。

```sh
# 原始清单(默认路径:不含 01-runtimeclass.yaml)
kubectl apply -f deploy/k8s/00-namespaces.yaml
kubectl apply -f deploy/k8s/02-rbac.yaml
kubectl apply -f deploy/k8s/03-sandbox-base.yaml      # 之后把插件灌进 cocola-plugins PVC
kubectl apply -f deploy/k8s/04-sandbox-manager.yaml

# 或 Helm(默认 runtimeClass.install=false、sandbox.runtimeClass=""、hostUsers="false")
helm install cocola deploy/helm/cocola-sandbox \
  --set sandbox.storageClass=<your-sc> \
  --set sandbox.llmBaseURL=http://llm-gateway.cocola.svc.cluster.local:8080
```

就绪检查:

```sh
kubectl -n cocola rollout status deploy/sandbox-manager   # 期望 2/2 available
kubectl -n cocola-sandboxes get pvc cocola-plugins        # 期望 Bound
```

---

## 2. 隔离自检 —— 大脑能在默认 runc + userns 下起来

先验证用户命名空间真的把容器 root 映射成了宿主机非特权 uid,再验大脑二进制。

```sh
# 2a. 用户命名空间指纹:容器内自认为 root(uid 0),宿主机视角却是高位非特权 uid。
#     起一个带 hostUsers:false 的探针 Pod,看 /proc/self/uid_map 的映射。
kubectl -n cocola-sandboxes run userns-probe --rm -it --restart=Never \
  --overrides='{"spec":{"hostUsers":false}}' \
  --image=alpine:3.20 -- sh -c 'id; cat /proc/self/uid_map'
# 期望:容器内 id 为 uid=0(root);uid_map 形如 "0  <大基址>  65536"
#       —— 容器 0 被映射到宿主机一个非 0 的高位 uid(用户命名空间生效)。
```

```sh
# 2b. 真实沙箱镜像里大脑可运行(把 image 换成你的 cocola sandbox 镜像)
kubectl -n cocola-sandboxes run brain-probe --rm -it --restart=Never \
  --overrides='{"spec":{"hostUsers":false}}' \
  --image=<your-cocola-sandbox-image> -- claude --version
# 期望:正常打印 claude code 版本号。runc 共享宿主机内核,无 syscall 兼容问题。
```

> runc 路径下大脑二进制无需任何兼容性 spike(与本机/Docker 一致)。
> 若你启用了 gVisor(附录 A),此处可能暴露 syscall 拦截,详见附录 A。

---

## 3. 端到端:经 sandbox-manager 拉起一个沙箱并验四件事

> 以下用 `sandbox-manager` 的 gRPC/HTTP 接口拉起沙箱。把 `<sm-addr>` 换成你
> 暴露 `sandbox-manager` 的地址(集群内可
> `kubectl -n cocola port-forward svc/sandbox-manager 8080:8080` 后用
> `localhost:8080`)。具体调用方式以本仓现有 e2e/集成脚本为准;下面给出
> "用 kubectl 直接观测"的等价校验。

### 3.1 创建沙箱

通过控制面创建一个沙箱后,确认 Pod 用 runc(无 RuntimeClassName)+ 用户命名空间:

```sh
SID=<返回的 sandbox-id>
kubectl -n cocola-sandboxes get pod cocola-$SID \
  -o jsonpath='rc=[{.spec.runtimeClassName}] hostUsers=[{.spec.hostUsers}]{"\n"}'
# 期望:rc=[](空,即 runc) hostUsers=[false](用户命名空间已开)
kubectl -n cocola-sandboxes get pod cocola-$SID -o wide   # 期望 Running / Ready
```

### 3.2 一次真实 query 经 service DNS 打到网关并返回

```sh
# 沙箱内应能解析并连到网关/集群内服务
kubectl -n cocola-sandboxes exec cocola-$SID -c sandbox -- \
  sh -c 'nslookup llm-gateway.cocola.svc.cluster.local && \
         wget -qO- --timeout=5 http://llm-gateway.cocola.svc.cluster.local:8080/healthz'
# 期望:DNS 解析成功 + healthz 200
```

通过控制面发起一次真实对话 query(走 Route A:大脑在沙箱内,经
`COCOLA_SANDBOX_LLM_BASE_URL` 指向的 service DNS 打到 llm-gateway),期望
**有正常模型回复**。

### 3.3 egress 锁定确认(ADR-0009)

```sh
# 出公网应被 NetworkPolicy 拒绝(超时/不可达,而非秒回)
kubectl -n cocola-sandboxes exec cocola-$SID -c sandbox -- \
  sh -c 'wget -qO- --timeout=5 https://example.com || echo "BLOCKED-as-expected"'
# 期望:BLOCKED-as-expected
kubectl -n cocola-sandboxes get networkpolicy cocola-egress-$SID -o yaml | head -40
```

### 3.4 原生 bash / file IO

```sh
kubectl -n cocola-sandboxes exec cocola-$SID -c sandbox -- \
  sh -c 'echo hello-cocola > /workspace/'$SID'/probe.txt && cat /workspace/'$SID'/probe.txt && id'
# 期望:打印 hello-cocola;id 显示 uid=10001(容器内非 root,且经 userns 再降权)
```

经控制面的 Exec / WriteFile / ReadFile 接口各跑一次,确认 tar-over-exec 链路
正常(写一个文件再读回,内容一致)。

---

## 4. 持久化:删 Pod -> 重挂 PVC -> `claude --resume` 续接

这是 M6 最关键的验收点——hibernate=删 Pod 留 PVC,resume=重建 Pod 重挂同一对
PVC,且大脑 `--resume` 能接上之前的会话上下文。

```sh
# 4a. 在沙箱里制造可验证的状态(会话 + 用户态文件)
#     先经控制面发一轮 query 让 ~/.claude 下留下会话记录;再写一个 workspace 文件
kubectl -n cocola-sandboxes exec cocola-$SID -c sandbox -- \
  sh -c 'echo persisted-marker > /workspace/'$SID'/keep.txt'

# 4b. Pause(经控制面 Pause 接口) -> Pod 应被删除,PVC/binding 保留
kubectl -n cocola-sandboxes get pod cocola-$SID            # 期望 NotFound
kubectl -n cocola-sandboxes get pvc | grep $SID            # 期望 user/session PVC 仍 Bound
kubectl -n cocola-sandboxes get configmap cocola-bind-$SID # 期望仍在(resolve 源)

# 4c. Resume(经控制面 Resume 接口) -> 新 Pod 重建并重挂同一对 PVC
kubectl -n cocola-sandboxes get pod cocola-$SID -o wide    # 期望重新 Running / Ready
kubectl -n cocola-sandboxes exec cocola-$SID -c sandbox -- cat /workspace/$SID/keep.txt
# 期望:persisted-marker —— workspace PVC 续上了

# 4d. 大脑续接:再发一轮 query,确认走 claude --resume 接上了之前会话
#     (~/.claude 经 user PVC subPath 持久化,跨 Pod 仍在)
```

> 注意:启用用户命名空间后,PVC 上的文件属主在宿主机视角是被映射后的高位 uid。
> 只要 Pause/Resume 用**同一个** `hostUsers` 设置(本仓由 binding 驱动,Create 与
> Resume 产出 byte-identical 的 Pod),映射一致,文件可正常读回。

跨副本验证(可选但建议):

```sh
# 由另一副本执行 Resume/Exec 也应成功(binding ConfigMap 是跨副本真相源,非内存态)
kubectl -n cocola get pods -l app=sandbox-manager
```

---

## 5. 验收判定(逐条勾)

- [ ] **2a** `hostUsers:false` Pod 的 uid_map 显示容器 0 映射到宿主机
      非 0 高位 uid(用户命名空间生效)。
- [ ] **2b** `claude --version` 在默认 runc 下正常打印。
- [ ] **3.1** 沙箱 Pod 无 `runtimeClassName`(runc)、`hostUsers=false`,
      且 Running/Ready,容器内 uid=10001。
- [ ] **3.2** 经 service DNS 打到 llm-gateway,真实 query 有模型回复。
- [ ] **3.3** 出公网被拒(egress NetworkPolicy 生效)。
- [ ] **3.4** 原生 bash 与 Exec/WriteFile/ReadFile 正常。
- [ ] **4** 删 Pod 后 PVC + binding 保留;Resume 重建重挂;`keep.txt` 续上;
      `claude --resume` 接回会话上下文。
- [ ] 全绿 -> 把 README 路线图 M6 行从 🚧 改为 ✅。

---

## 5.5 配置真实模型(可选,验真实回复时)

llm-gateway 默认 `fake` provider 即可验证通路。要让 §3.2 返回**真实模型回复**,
建一个 `cocola-llm` Secret(密钥只走 Secret,绝不写进清单/镜像),然后重启网关:

```sh
kubectl -n cocola create secret generic cocola-llm \
  --from-literal=provider=anthropic \
  --from-literal=anthropic_base_url=https://<你购买服务的域名> \
  --from-literal=anthropic_api_key=sk-ant-xxxx \
  --from-literal=anthropic_model=claude-3-5-sonnet-20241022
kubectl -n cocola rollout restart deploy/llm-gateway
```

> `05-deps-redis-llm-gateway.yaml` 里这些 env 用 `secretKeyRef ... optional: true`
> 引用该 Secret:不存在则回落 `fake`,存在则走真实上游。base_url 后端 SDK 会自动
> 追加 `/v1/messages`。

---

## 6. 排障速查

| 现象 | 多半原因 | 处理 |
|---|---|---|
| 沙箱 Pod 卡 `Pending`/`ContainerCreating`,事件提示 userns 相关 | 内核过旧 / 集群 < 1.33 | 升级内核(≥ 5.19,建议 6.x)与集群(≥ 1.33);或临时设 `COCOLA_K8S_HOST_USERS=default` |
| `hostUsers:false` 的 Pod 报 idmap/feature gate 不可用 | 旧版集群未默认开 UserNamespacesSupport | 升级到 1.33+(k3s 1.35.5 已默认开);或显式开 feature gate |
| egress 没被拦(公网秒回) | CNI 不强制 NetworkPolicy(纯上游 flannel、部分托管 CNI) | k3s 自带 kube-router 控制器默认强制;托管集群确认 CNI 支持 NetworkPolicy |
| 域名级 allowlist 不生效 | 纯 NetworkPolicy 不支持域名 | 用 DNS-aware CNI(Cilium),否则只用 CIDR/IP |
| Resume 后文件丢/读不回 | storageClass 非持久 / PVC 未重挂 / userns 映射不一致 | 确认 PVC `Bound` 且 `ReadWriteOnce` 节点亲和满足;确认 Create/Resume 同一 `hostUsers` |
| 跨副本 Resume 失败 | 误以为靠内存态 | 确认 binding ConfigMap 存在,resolve 走的是它 |

---

## 附录 A:可选增强 —— gVisor(`runsc` RuntimeClass)

默认 runc + 用户命名空间已满足"自托管、简单部署、k3s 编排"的目标。若你需要
**更强的隔离**(gVisor 在用户态实现了一个应用内核,拦截并代理 syscall,显著缩小
宿主机内核攻击面),按下面启用——代价是**每个节点都要装 gVisor**。

### A.1 节点装 gVisor 并注册进 k3s 的 containerd

> gVisor 默认 platform 是 **systrap**:纯用户态(seccomp + 信号),**不需要嵌套
> 虚拟化**,普通云主机直接能跑。仅当云主机暴露了 KVM 设备时,才可选 KVM
> platform 提速——非必需。

1. **装 gVisor 二进制**:从 gVisor 官方 release 路径
   `storage.googleapis.com/gvisor/releases/release/latest/$(uname -m)` 下载
   `runsc` 与 `containerd-shim-runsc-v1`,校验 sha512 后 `chmod +x`,放到节点
   PATH 上的系统 bin 目录。systrap 为默认 platform,无需额外配置。

2. **把 runsc 注册进 k3s 的 containerd**:k3s 用模板文件生成 containerd 配置,
   不要直接改生成出来的 `config.toml`,而是写模板(改完 `systemctl restart k3s`
   生效):
   - 新版 k3s(containerd 2.x / cri v1):在 k3s agent 的 containerd 配置目录下
     新建 `config-v3.toml.tmpl`,内容追加:
     ```toml
     {{ template "base" . }}
     [plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runsc]
       runtime_type = "io.containerd.runsc.v1"
     ```
   - 旧版 k3s(containerd 1.x):同目录新建 `config.toml.tmpl`,内容追加:
     ```toml
     {{ template "base" . }}
     [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
       runtime_type = "io.containerd.runsc.v1"
     ```
   > runtime 名 `runsc` 必须与 `deploy/k8s/01-runtimeclass.yaml` 的 handler 一致。

3. **托管集群**:GKE Sandbox 节点池开箱带 gVisor(RuntimeClass 名通常是
   `gvisor`);minikube 可 `addons enable gvisor`。

### A.2 启用 gVisor

```sh
# 1) apply gVisor RuntimeClass
kubectl apply -f deploy/k8s/01-runtimeclass.yaml

# 2) 让 sandbox-manager 给沙箱 Pod 打上 runtimeClassName=runsc
kubectl -n cocola set env deploy/sandbox-manager COCOLA_K8S_RUNTIME_CLASS=runsc
# Helm:--set runtimeClass.install=true --set sandbox.runtimeClass=runsc
#       (GKE Sandbox:runtimeClass.install=false、sandbox.runtimeClass=gvisor)
```

> gVisor 与用户命名空间可叠加:`COCOLA_K8S_HOST_USERS=false` 仍生效。也可只用
> gVisor 而把 `hostUsers` 设 `default`,视你的纵深防御策略而定。

### A.3 gVisor compat spike(启用后追加验)

```sh
# A.3a. runsc 内核指纹:gVisor 的 dmesg 首行带 gVisor 字样,普通 runc 不会
kubectl -n cocola-sandboxes run gvisor-probe --rm -it --restart=Never \
  --overrides='{"spec":{"runtimeClassName":"runsc"}}' \
  --image=alpine:3.20 -- sh -c 'uname -a; dmesg | head -1'
# 期望:dmesg 首行包含 "gVisor"

# A.3b. 大脑在 runsc 下可运行(syscall 兼容性 spike)
kubectl -n cocola-sandboxes run brain-probe --rm -it --restart=Never \
  --overrides='{"spec":{"runtimeClassName":"runsc"}}' \
  --image=<your-cocola-sandbox-image> -- claude --version
# 期望:正常打印版本号。若某 syscall 被 gVisor 拦截(function not implemented 之类),
#       记录失败调用,评估基础镜像 / runsc 版本 / 不同 platform。
```

| gVisor 专属现象 | 多半原因 | 处理 |
|---|---|---|
| 沙箱 Pod 卡 `Pending`/`ContainerCreating` | 节点无 gVisor / RuntimeClass handler 不匹配 | 装 `containerd-shim-runsc-v1` 并核对 handler 名(模板 runtime 名 = `runsc`) |
| Pod 起得来但 `claude` 报 syscall 错 | gVisor 拦了某调用 | 记录失败 syscall,评估基础镜像 / runsc 版本;或试不同 platform |
| `runsc` 报需要 KVM/嵌套虚拟化 | 误用 KVM platform | 用默认 systrap platform(无需嵌套虚拟化) |
