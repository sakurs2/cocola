# M6 验收 Runbook：K8s + gVisor 沙箱端到端验收(Layer C)

> 适用对象:一台 **Linux 云服务器**(最贴近生产,本仓首选路径),在其上用
> k3s + gVisor 搭一套带 `runsc` RuntimeClass 的单机 Kubernetes。
> 目标:跑通 ADR-0008/ADR-0009 的验收门——`runsc` 下大脑可运行、egress 被锁定、
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

1. 集群 v1.29+,`kubectl` 已指向目标 context(`kubectl config current-context`)。
2. **节点装了 gVisor**:`containerd-shim-runsc-v1` 在节点上且已在 containerd
   注册,handler 名与 `01-runtimeclass.yaml` 的 `runsc` 一致。
3. **CNI 强制 NetworkPolicy**。**k3s 默认即满足**:它在 flannel 之外自带一个
   kube-router 的 NetworkPolicy 控制器,NetworkPolicy 开箱强制(域名级 allowlist
   仍需 DNS-aware CNI 如 Cilium;纯 CIDR/IP 在 k3s 默认下即可)。
4. 控制面依赖(redis / llm-gateway)已按 `04-sandbox-manager.yaml` 里的
   in-cluster DNS 名就绪。

### 在 Linux 云服务器上搭 k3s + gVisor(单机即可,推荐)

> gVisor 默认 platform 是 **systrap**:纯用户态(seccomp + 信号),**不需要嵌套
> 虚拟化**,普通云主机直接能跑。仅当云主机暴露了 KVM 设备(`/dev/kvm`)时,
> 才可选 KVM platform 提速——对验收非必需。

1. **装 k3s**(自带 containerd + flannel + kube-router NetworkPolicy 控制器):
   下载官方安装脚本(get.k3s.io),先通读再执行(不要直接管道进 shell)。
   装完确认:`kubectl get nodes` 为 Ready;NetworkPolicy 默认强制,无需换 CNI。

2. **装 gVisor 二进制**:从 gVisor 官方 release 路径
   `storage.googleapis.com/gvisor/releases/release/latest/$(uname -m)` 下载
   `runsc` 与 `containerd-shim-runsc-v1`,`chmod +x` 后放到节点 PATH 上的
   系统 bin 目录(`/usr/local/bin`)。systrap 为默认 platform,无需额外配置。

3. **把 runsc 注册进 k3s 的 containerd**:k3s 用模板文件生成 containerd 配置,
   不要直接改生成出来的 `config.toml`,而是写模板(改完 `systemctl restart k3s`
   生效):
   - 新版 k3s(containerd 2.x / cri v1):模板路径
     `/var/lib/rancher/k3s/agent/etc/containerd/config-v3.toml.tmpl`,内容追加:
     ```toml
     {{ template "base" . }}
     [plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runsc]
       runtime_type = "io.containerd.runsc.v1"
     ```
   - 旧版 k3s(containerd 1.x):模板路径
     `/var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl`,内容追加:
     ```toml
     {{ template "base" . }}
     [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
       runtime_type = "io.containerd.runsc.v1"
     ```
   > runtime 名 `runsc` 必须与 `deploy/k8s/01-runtimeclass.yaml` 的 handler 一致。

4. **KUBECONFIG**:指向 k3s 默认 kubeconfig `/etc/rancher/k3s/k3s.yaml`(远程操作时把
   其中 server 地址改成云服务器公网/内网 IP)。

> 备选(非云服务器场景):minikube `--container-runtime=containerd` +
> `minikube addons enable gvisor`;或 GKE Sandbox 节点池(RuntimeClass 名通常是
> `gvisor`,部署时设 `runtimeClass.install=false`、`sandbox.runtimeClass=gvisor`)。

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
> (镜像也可放进 `/var/lib/rancher/k3s/agent/images/` 让 k3s 启动时自动导入。)

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
- **要验真实模型回复**:创建 `cocola-llm` Secret 后重启网关(见 §6)。

---

## 1. 部署沙箱平面

```sh
# 原始清单
kubectl apply -f deploy/k8s/00-namespaces.yaml
kubectl apply -f deploy/k8s/01-runtimeclass.yaml      # 集群自带 gvisor 时可跳过
kubectl apply -f deploy/k8s/02-rbac.yaml
kubectl apply -f deploy/k8s/03-sandbox-base.yaml      # 之后把插件灌进 cocola-plugins PVC
kubectl apply -f deploy/k8s/04-sandbox-manager.yaml

# 或 Helm
helm install cocola deploy/helm/cocola-sandbox \
  --set sandbox.storageClass=<your-sc> \
  --set sandbox.llmBaseURL=http://llm-gateway.cocola.svc.cluster.local:8080
```

就绪检查:

```sh
kubectl -n cocola rollout status deploy/sandbox-manager   # 期望 2/2 available
kubectl get runtimeclass runsc                            # 期望存在(自建场景)
kubectl -n cocola-sandboxes get pvc cocola-plugins        # 期望 Bound
```

---

## 2. gVisor compat spike —— 大脑能在 runsc 下起来

先验证 RuntimeClass 真的把 Pod 关进了 gVisor 用户态内核,再验大脑二进制。

```sh
# 2a. runsc 内核指纹:gVisor 的 dmesg 首行带 gVisor 字样,普通 runc 不会
kubectl -n cocola-sandboxes run gvisor-probe --rm -it --restart=Never \
  --overrides='{"spec":{"runtimeClassName":"runsc"}}' \
  --image=alpine:3.20 -- sh -c 'uname -a; dmesg | head -1'
# 期望:dmesg 首行包含 "gVisor"
```

```sh
# 2b. 真实沙箱镜像里大脑可运行(把 image 换成你的 cocola sandbox 镜像)
kubectl -n cocola-sandboxes run brain-probe --rm -it --restart=Never \
  --overrides='{"spec":{"runtimeClassName":"runsc"}}' \
  --image=<your-cocola-sandbox-image> -- claude --version
# 期望:正常打印 claude code 版本号,无 runsc 拦截的 syscall 报错
```

> 若 2b 出现某 syscall 被 gVisor 拦截(`function not implemented` 之类),
> 记录失败的调用,这正是 compat spike 要暴露的信息——回报后再决定补丁或
> 调整基础镜像。

---

## 3. 端到端:经 sandbox-manager 拉起一个沙箱并验四件事

> 以下用 `sandbox-manager` 的 gRPC/HTTP 接口拉起沙箱。把 `<sm-addr>` 换成你
> 暴露 `sandbox-manager` 的地址(集群内可
> `kubectl -n cocola port-forward svc/sandbox-manager 8080:8080` 后用
> `localhost:8080`)。具体调用方式以本仓现有 e2e/集成脚本为准;下面给出
> "用 kubectl 直接观测"的等价校验。

### 3.1 创建沙箱

通过控制面创建一个沙箱后,确认 Pod 跑在 runsc 上:

```sh
SID=<返回的 sandbox-id>
kubectl -n cocola-sandboxes get pod cocola-$SID -o jsonpath='{.spec.runtimeClassName}{"\n"}'
# 期望:runsc
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
# 期望:打印 hello-cocola;id 显示 uid=10001(非 root)
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

跨副本验证(可选但建议):

```sh
# 由另一副本执行 Resume/Exec 也应成功(binding ConfigMap 是跨副本真相源,非内存态)
kubectl -n cocola get pods -l app=sandbox-manager
```

---

## 5. 验收判定(逐条勾)

- [ ] **2a** runsc Pod 的 dmesg 含 "gVisor"(确实在用户态内核内)。
- [ ] **2b** `claude --version` 在 runsc 下正常打印,无 syscall 拦截。
- [ ] **3.1** 沙箱 Pod `runtimeClassName=runsc` 且 Running/Ready,uid=10001。
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
| 沙箱 Pod 卡 `Pending`/`ContainerCreating` | 节点无 gVisor / RuntimeClass handler 不匹配 | 装 `containerd-shim-runsc-v1` 并核对 handler 名(模板 runtime 名 = `runsc`) |
| Pod 起得来但 `claude` 报 syscall 错 | gVisor 拦了某调用 | 记录失败 syscall,评估基础镜像 / runsc 版本;或试不同 platform |
| egress 没被拦(公网秒回) | CNI 不强制 NetworkPolicy(纯上游 flannel、部分托管 CNI) | k3s 自带 kube-router 控制器默认强制;托管集群确认 CNI 支持 NetworkPolicy |
| 域名级 allowlist 不生效 | 纯 NetworkPolicy 不支持域名 | 用 DNS-aware CNI(Cilium),否则只用 CIDR/IP |
| Resume 后文件丢 | storageClass 非持久 / PVC 未重挂 | 确认 PVC `Bound` 且 `ReadWriteOnce` 节点亲和满足 |
| 跨副本 Resume 失败 | 误以为靠内存态 | 确认 binding ConfigMap 存在,resolve 走的是它 |
| `runsc` 报需要 KVM/嵌套虚拟化 | 误用 KVM platform | 用默认 systrap platform(无需嵌套虚拟化) |
