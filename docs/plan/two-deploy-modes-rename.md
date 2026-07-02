# Plan: 收敛为两种部署模式 + 重命名 make 目标

## 背景 / 决策
用户拍板:后续只保留**两种**部署方式,命名要对应其语义:

1. **模式1 `make up`** —— 除 OpenSandbox server 与沙箱容器外,其余 cocola 服务全部**原生**
   运行(= 现在的 `--hybrid` / `make up-hybrid`)。这是**日常调试的默认模式**。
2. **模式2 `make up-container`** —— 全部在容器内(= 现在的 `make up-all` → `scripts/start.sh`
   + `docker-compose.full.yml`)。

删除:旧的 `make up`(EchoProvider、无模型的纯原生死路径)与 `make up-web`。

## 现状(为什么乱)
- `scripts/run-stack.sh` 一个脚本扛 4 种模式(echo / `--with-web` / `--all` / `--hybrid`),
  550 行,`--all` 与 `--hybrid` 语义重叠;`up`/`up-web` 自 `--hybrid` 出现后已无人用。
- Makefile 目标名 `up-hybrid` / `up-all` 不直观,和"两种部署模式"对不上。

## 变更点
### 1. Makefile(`up up-web up-hybrid up-all` → `up up-container`)
- `.PHONY: up up-container`(删除 `up-web`、`up-hybrid`、`up-all`)。
- `up:` → `bash scripts/run-stack.sh`(脚本内部即模式1,无需 flag)。
- `up-container:` → `bash scripts/start.sh`。
- 重写 dev-stack 头部注释块,只描述这两种模式。

### 2. scripts/run-stack.sh(hybrid 成为唯一模式)
- 删除 `--with-llm` / `--with-web` / `--all` 分支与死路径说明;保留 `-h/--help`。
- 不再解析模式 flag;内部 `HYBRID=1 / WITH_LLM=1 / WITH_WEB=1` 恒为真(hybrid 本就
  同时置起这三者),因此所有 `[[ "$X" == "1" ]]` 条件块行为不变——**低风险**。
- 未知 flag 仍报错退出;更新文件头 Usage 与设计注释为"唯一模式=模式1"。

### 3. scripts/start.sh
- 仅更新对外提示文案里的 `make up-all` → `make up-container`(命令行为不变)。

### 4. 活文档 docs/adr/0007
- "Local orchestration (`make up`)" 段:把 `up / up-web / up-all` 与
  "progressive enablement / EchoProvider" 的旧叙述,改写为"两种模式"的准确描述。

## 不动
- `dev-up/down`、`opensandbox-up/down`、`verify-opensandbox*`、里程碑/e2e 脚本:本次不改
  (归档/分目录留作后续,可另起)。`docs/archive/*` 历史 changelog 不回改。

## 校验
- `bash -n scripts/run-stack.sh` / `bash -n scripts/start.sh` 语法通过。
- `make -n up` → `bash scripts/run-stack.sh`;`make -n up-container` → `bash scripts/start.sh`。
- `make help` 只列出 `up` / `up-container` 两个启动目标。
- 端到端(用户本机):`make up` 起栈 + Web/curl 真实对话;`make up-container` 全容器栈。

## 回滚
`git revert` 本次提交即可;目标名与脚本模式一并回到 up-hybrid/up-all。
