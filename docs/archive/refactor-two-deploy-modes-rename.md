# Refactor: 收敛为两种部署模式 + 重命名 make 目标

Plan: docs/plan/two-deploy-modes-rename.md

## 背景
启动/调试入口太乱:`scripts/run-stack.sh` 一个脚本扛 4 种模式
(echo / `--with-web` / `--all` / `--hybrid`),Makefile 暴露
`up`/`up-web`/`up-hybrid`/`up-all` 四个目标,`up-hybrid`/`up-all` 命名不直观。
用户拍板:后续只保留**两种**部署方式,命名对应语义。

## 变更
1. **Makefile** —— 目标从 `up up-web up-hybrid up-all` 收敛为 **`up` / `up-container`**:
   - `make up`(模式1,默认调试):除 OpenSandbox server + 沙箱/基建容器外,所有
     cocola 服务原生运行(= 原 `--hybrid`)→ `bash scripts/run-stack.sh`。
   - `make up-container`(模式2):全容器 Route A 栈 → `bash scripts/start.sh`。
   - 删除旧 `make up`(EchoProvider 死路径)与 `make up-web`。dev-stack 头部注释重写为两模式。
2. **scripts/run-stack.sh** —— hybrid 成为**唯一模式**。删除 `--with-llm`/`--with-web`/
   `--all` 分支;`WITH_LLM`/`WITH_WEB`/`HYBRID` 三个开关改为恒为 `1`(hybrid 本就同时
   置起三者),下游所有 `[[ "$X" == "1" ]]` 守卫行为不变——低风险。文件头 Usage/设计
   注释改写为"模式1"。未知 flag 仍报错退出。
3. **docs/adr/0007**(活文档) —— "Local orchestration" 段从
   `up`/`up-web`/`up-all` + EchoProvider/progressive-enablement 旧叙述,改写为
   "两种部署模式(`make up` / `make up-container`)"。

## 不动
- `scripts/start.sh` 无 make 目标名引用,行为不变。
- `docs/plan/*`、`docs/archive/*` 历史文档保留原样(时间点记录,不回改)。
- `dev-*`/`opensandbox-*`/`verify-*`/里程碑脚本本次不动。

## 校验
- `bash -n scripts/run-stack.sh` / `bash -n scripts/start.sh` 通过。
- `make -n up` → `bash scripts/run-stack.sh`;`make -n up-container` → `bash scripts/start.sh`。
- `make -n up-web` / `make -n up-hybrid` → No rule(确认已删)。
- `make help` 启动目标只剩 `up` / `up-container`。
- 端到端(用户本机):`make up` 真实对话;`make up-container` 全容器栈。

## 回滚
`git revert` 本次提交即可。
