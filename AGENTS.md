# AGENTS.md · 仓库协作约束

> 本文件是 **winter 仓库的协作契约**，面向所有参与本仓库的 AI 助手与人类贡献者。
> **优先级**：用户在当前对话中的明确指令 > 本文件 > 助手/工具的默认习惯。
> 生效日期：2026-11-08 · 维护者：仓库 owner · 变更本文件本身也必须单独提交。

---

## 0. 三十秒速读（铁律，不可协商）

1. **每一次逻辑上独立的改动完成后立即 `git commit`**，不允许把未提交的工作区留到下一次会话。
2. **一个逻辑变更 = 一个提交**；无关改动必须拆分，禁止把文档、配置、代码混进一个大提交。
3. **提交前跑自检**（§7.3）并**先过 `.gitignore` 检查**（§2.5），提交后 `git status` 必须干净（仅允许 `.gitignore` 明确忽略的项）。
4. **禁止** `--amend`、`--no-verify`、`push --force`、`reset --hard`、无检查的 `git add -A`。回滚一律用 `git revert`。
5. **控制面与数据面分离**：任何代码、配置、文档都不得让业务 IO 经过本平台。
6. **Agent 只执行白名单内的参数化命令**，不拼 shell、不接自由文本；凭据明文零落盘。
7. **未拍板的选型（D1~D11）不得写成既定事实**，一律标注 `待决策` / `TODO`。
8. **不删除、不改写 `.workbuddy/`**（工作区元数据），它已被 `.gitignore` 排除。
9. **不自动 `git push`、不自动创建 PR、不自动安装 git hook**，除非用户明确要求。
10. **改动文档时同步更新 `docs/README.md` 索引**；改动架构时留 ADR。
11. **每次 commit 之前都要检查并按需修改 `.gitignore`**（§2.5）：`git status` 里出现任何不该入库的文件类型，先补忽略规则再 `git add`，绝不让日志、缓存、备份、密钥、生成物混进仓库。
12. **Ansible 内容全英文**（§4.4）：`deploy/` 下所有人类可读文本一律英文；变量名、Jinja 表达式、命令、路径、tag、ADR 引用不变。

---

## 1. 项目概览

| 项 | 内容 |
| --- | --- |
| 仓库 | `winter` |
| 当前阶段 | **实现阶段**：文档基线已完成，`deploy/`（ansible）与 `server/`（Go + Gin）已落地；代码规范已生效 |
| 主题 | Ceph 集群管理平台（控制面）：多集群纳管/新建、可视化运维、RBAC、审计、监控告警 |
| 主分支 | `main` |
| 关键入口 | [文档总览](docs/README.md) · [PRD](docs/01-产品线/01-产品经理/PRD-Ceph管理平台.md) · [架构设计](docs/02-研发线/04-架构师/架构设计-Ceph管理平台.md) · [权衡分析](docs/02-研发线/04-架构师/架构决策与权衡分析.md) |
| 技术栈 | **已确认**（`ADR-11` / `16` / `18` / `36`，2026-11-15）：React + TypeScript / Golang / PostgreSQL（**含任务队列，无独立 MQ**）/ **Harbor** / **VictoriaMetrics + VictoriaLogs + vmalert + Alertmanager + Grafana** / cephadm + podman / ansible / CentOS Stream 9。**待定**：认证源（D7） |
| 待决策 | D1~D11，见 [权衡分析 §8](docs/02-研发线/04-架构师/架构决策与权衡分析.md) 与 [PRD §11](docs/01-产品线/01-产品经理/PRD-Ceph管理平台.md) |

### 目录约定

```
AGENTS.md            本文件（协作约束）
README.md            仓库门面
.gitignore           忽略规则（已建立）
docs/                按《软件行业岗位全景图》七条线 / 24 个岗位归档的全部文档
  ├── README.md      文档总览与索引（唯一索引入口）
  └── 岗位映射与变更记录.md   岗位体系变更时的旧→新对照（体系变了必须更新）
deploy/              平台部署框架：inventory / group_vars / roles / playbooks / scripts / artifacts（ADR-17；PG HA、接入层、离线制品源、可观测性栈与 Server 部署）
server/              管理面 Server（Go + Gin）：internal/{config,db,auth,audit,task,api}，迁移 SQL 随二进制 embed
.workbuddy/          工作区元数据 / 会话记忆（不纳入版本控制，不得删除）
```

> 新增目录前先确认应归属哪条岗位线、哪个岗位编号；**不要在仓库根目录随手创建散落文件**。

---

## 2. Git：提交规范（本项目最高频操作）

### 2.1 什么时候提交

**每次改动都要提交。** 具体判定：

| 情况 | 做法 |
| --- | --- |
| 完成一个逻辑改动（哪怕只改一行） | 立即提交 |
| 一次任务产生多类改动（如 PRD + 架构 + 部署手册） | 按逻辑拆成多个提交，逐个提交 |
| 工作尚未完成但要结束会话 | 在 feature 分支上做 `WIP:` 提交，**不要留未提交的工作区** |
| 改动会让仓库处于不可构建/链接断裂状态 | 先修到可自检状态再提交；确实做不到时在提交信息里写明 `BROKEN-INTERMEDIATE` 与修复计划 |
| 纯调研、只读操作 | 不产生改动，无需提交 |

> 原则：**宁可多一个可定位的小提交，也不要一个混在一起的大提交，更不要未提交的工作区。** 这正是「方便回滚」的前提。

### 2.2 提交粒度

- 一次提交只回答一个问题：`改了什么 + 为什么`。
- 允许同一提交包含「文档正文 + 对应的索引更新 + 对应岗位 README 清单更新」，因为它们是同一逻辑变更。
- 不允许：格式调整与功能改动混提、多个无关主题混提、把生成物/二进制/密钥塞进提交。
- 禁止为了「让历史好看」压缩中间提交；可定位的中间状态优先于整洁的假象。

### 2.3 提交信息格式（Conventional Commits，中文 subject）

```
<type>(<scope>): <中文动词开头，≤50 字，不加句号>

<body：为什么改、影响范围、如何回滚；多行用 "- " 开头>

<footer：Refs: 文档路径 / 决策 ID / 问题编号>
```

| 字段 | 规则 |
| --- | --- |
| `type` | `feat` `fix` `docs` `refactor` `test` `chore` `perf` `build` `ci` `revert` `style` |
| `scope` | `docs/pm` `docs/arch` `docs/sre` `web` `server` `agent` `infra` `deploy` `repo` 等，用小写 |
| `subject` | 中文，动词开头（新增/修复/调整/删除/补充），说明**结果**而非过程 |
| `body` | 必写于非琐碎改动：动机、影响范围、**回滚方式**（`git revert <this>` 或需额外步骤） |
| `footer` | `Refs: docs/...`、`Refs: ADR-04`、`Refs: D1` |

示例：

```
docs(arch): 补充状态对账设计与漂移告警规则

- 原因：平台状态与 Ceph 实际状态必然漂移，PRD FR-04 需要明确展示口径
- 影响：架构设计 §5.3、SRE 告警规则表、测试用例 TC-12
- 回滚：git revert <this>，无数据迁移

Refs: 权衡分析 B3, ADR-08
```

### 2.4 提交前必做（顺序固定）

```bash
git status --short                      # 1. 确认改动范围，无意外文件
# 1b.【必做】检查并按需修改 .gitignore（§2.5）：
#     status 中若出现缓存 / 日志 / 备份 / 密钥 / 生成物等不该入库的文件，
#     先编辑 .gitignore 补上规则 → 再跑一次 git status 确认它已消失
git diff --stat && git diff             # 2. 逐段读一遍 diff
# 3. 跑 §7.3 自检（至少：链接校验 + 无密钥明文 + 无待决策写成定论）
git add <具体文件或目录>                 # 4. 明确列出，不用 git add -A
git diff --cached --stat                # 5. 确认暂存区正是预期内容
git commit -m "<type>(<scope>): ..."    # 6. 提交，不加 --no-verify
git status --short                      # 7. 确认工作区干净
git log -1 --stat                       # 8. 回看本次提交
```

> 第 **1b** 步不可跳过：`.gitignore` 是提交前的第一道闸门，先拦住无用文件，再谈 add / commit。

### 2.5 每次 commit 前都要检查并按需修改 `.gitignore`

> 目的：防止日志、缓存、备份、密钥、生成物、离线制品等无用文件混进仓库。这类文件一旦入库，清理成本高、`git revert` 会带噪声，还可能泄露敏感信息。

**这是每次提交的强制步骤，不是可选优化。** 执行顺序：

1. 提交前先跑 `git status --short`，逐行审视 `??` 与 `M` 条目，不只看「我改了什么」，还要看「多出来了什么」。
2. 对每个不该入库的文件问一句：**它会不会再次出现？**
   - 会（缓存、日志、构建产物、编辑器文件、备份…）→ **必须先写进 `.gitignore`**；
   - 不会（一次性临时文件）→ 移出仓库目录或删除，但绝不用 `git add -A` 顺手带上。
3. 改完 `.gitignore` 后**再跑一次 `git status --short`**，确认该文件已消失；必要时用 `git check-ignore -v <文件>` 查是哪条规则生效。
4. 然后才允许 `git add <具体文件>`。`.gitignore` 的修改与本次改动属于同一逻辑变更时一起提交；若顺带补充了多条通用规则，可单独提交 `chore(repo): 补充忽略规则`。
5. 已被跟踪的无用文件：用 `git rm --cached <file>` 移出索引并补忽略规则，在提交信息里写明；**不要只删工作区文件**（那会留下历史垃圾）。
6. 拿不准该不该忽略：先不提交，问用户。宁可多问一次，不要污染历史。

**每次必查的高频类型**：

| 类型 | 示例 |
| --- | --- |
| 依赖与缓存 | `node_modules/` `__pycache__/` `.pytest_cache/` `.mypy_cache/` `.venv/` |
| 构建与测试产物 | `dist/` `build/` `coverage/` `*.out` `*.log` |
| 编辑器 / 系统 | `.vscode/` `.idea/` `.DS_Store` `Thumbs.db` `*.swp` |
| 备份与冲突残留 | `*.bak` `*.orig` `*.rej` `*~` |
| 密钥与本地配置 | `.env` `*.key` `*.pem` `*_keyring` `credentials.local.*` |
| 大体积制品 | `*.tar` `*.tar.gz` `*.zip` `offline-images-*` |
| 工作区元数据 | `.workbuddy/` |

> 边界说明：**「每次都检查」是硬要求，「每次都产生 `.gitignore` 的 diff」不是**。没有新类型时不为了改而改（避免无意义改动）；但只要出现新的无用文件类型，就必须先补规则再提交。**不允许以「这次先不管」为由跳过。**

### 2.6 明令禁止

| 禁止 | 原因 |
| --- | --- |
| `git commit --amend` | 改写已存在的提交，破坏回滚锚点 |
| `git push --force` / `--force-with-lease` | 覆盖远端历史 |
| `git reset --hard` / `git clean -fd` | 不可逆丢失未提交改动；要丢弃先 `git stash` |
| `git commit --no-verify` | 跳过钩子等于跳过质量门槛 |
| `git add -A` / `git add .` 后直接提交 | 容易误提交密钥、大文件、临时文件 |
| **提交前未检查 `.gitignore` 就把 `??` 文件一起 `git add`** | 无用文件入库后难清理，污染回滚历史与 `git blame` |
| 在 `main` 上做实验性大改 | 见 §3 分支策略 |
| 提交 `WIP` / `tmp` / `test` 之类的无信息提交到 `main` | 污染历史，无法定位回滚点 |
| 提交二进制、离线镜像包、`*.key`、`*.pem`、`.env`、`*_keyring` | 仓库膨胀 / 密钥泄露 |

---

## 3. 分支、标签与回滚

### 3.1 分支

| 分支 | 用途 | 命名 |
| --- | --- | --- |
| `main` | 始终可交付、自检通过 | — |
| 功能分支 | 跨多次会话、影响面大的改动 | `feat/<主题>` `fix/<主题>` `docs/<主题>` |
| 实验分支 | 选型验证、可能被整体放弃 | `spike/<主题>` |

小改动（文档修订、约束调整）允许直接提交到 `main`；**任何可能推翻的设计实验必须开分支**。

### 3.2 标签（里程碑锚点）

- 每个阶段出口打轻量 tag：`v0.1-docs-baseline`、`v0.2-mvp`、`v1.0`。
- tag 打在该阶段最后一个提交上，提交信息中注明 tag 含义。
- 打 tag 前确认 `git status` 干净、自检通过。

### 3.3 回滚操作（唯一推荐路径）

```bash
git log --oneline --decorate -20     # 定位
git show <sha>                        # 确认就是它
git revert <sha>                      # 生成反向提交，历史完整可追溯
git revert --no-commit <sha1> <sha2>  # 多个连续提交的合并回滚，然后单独提交
git tag -l                            # 回到里程碑锚点时可 git checkout <tag> 查看
git reflog                            # 兜底：找回误操作前的 HEAD
```

- **回滚也必须有提交信息**，写清回滚原因与 `Reverts: <sha>`，type 用 `revert`。
- 涉及数据库 migration、配置格式、Agent 协议的提交**不能只 revert 代码**：必须在 body 里写明向前兼容/补偿步骤（见 §8）。
- 需要「临时看一眼旧版本」用 `git checkout <tag>`，看完切回，不要在 detached HEAD 上改东西。
- `git reset --hard` 仅在**新建的、未推送的、无他人依赖的本地分支**且用户明确同意时才可考虑。

---

## 4. 代码风格

> 当前仓库无代码；以下规则在对应语言的文件首次出现时即生效。**宁可先写清规则，也不要等代码堆起来再统一格式化**（那会产生一个巨大的、无法回滚的 style 提交）。

### 4.1 通用（所有语言）

- 缩进一律 **空格**（Go 用 tab 由 `gofmt` 决定，其余 2 或 4 空格按语言约定），禁止混用 tab/空格；换行 `LF`。
- 文件 UTF-8 无 BOM，末尾保留一个换行，行尾无多余空格。
- 单行 ≤ 120 字符（Markdown 表格与长 URL 除外）。
- 命名用「能读懂业务含义」的名字，禁止 `tmp1` `data2` `foo` `handler_new`；缩写只保留行业通用词（`fsid` `pg` `osd` `az` `rbac`）。
- 注释写 **为什么**，不写「这行做了什么」；公开 API / 任务模板 / 表结构必须有注释或文档。
- 错误必须处理或显式向上传递，禁止静默 `catch/ignore`；日志结构化并带 `trace_id` / `task_id` / `cluster_id`。
- 配置与代码分离：地址、超时、并发、周期、白名单全部可配置，不硬编码。
- 任何新增「魔法值」先定义常量并说明来源。
- 函数/组件单一职责；超过 ~80 行先考虑拆分。
- 依赖注入优于全局状态；时间、随机数、外部调用要可替换，便于测试。

### 4.2 Golang（Server / Agent）

- 必须 `gofmt` + `goimports`，`go vet` 无告警；CI 中作为硬性检查。
- 包名单数、小写、无下划线；目录按领域分层（`api` / `domain` / `task` / `agent` / `store`），不按技术类型堆 `utils`。
- `error` 用 `fmt.Errorf("...: %w", err)` 包装并保留链路；库代码禁止 `panic`，只在 `main` 做 fatal 处理。
- `context.Context` 作为第一个参数一路传递；所有外部调用（DB、队列、Ceph CLI、HTTP）必须支持超时与取消。
- 并发：共享状态用锁或 channel，禁止裸 `go func` 泄漏；goroutine 有退出条件。
- **命令执行**：只用 `exec.CommandContext` + argv 数组，禁止 `sh -c` / `bash -c` / 字符串拼接（见 §6）。
- 测试：表驱动，文件名 `_test.go`，关键路径（状态机、幂等键、参数校验、白名单）必须有单测；`go test -race` 通过。
- 日志用结构化日志库，不打印凭据、keyring、token、完整 Ceph 输出中的敏感字段。

### 4.3 TypeScript / React（Web）

- `strict: true`，禁止 `any`（确需时 `unknown` + 类型守卫）、禁止 `@ts-ignore`（改用带说明的 `@ts-expect-error`）。
- ESLint + Prettier 配置纳入仓库，提交前自动格式化；**格式化改动与逻辑改动分开提交**。
- 函数组件 + Hooks；组件按页面/领域分目录，通用组件放 `components/`，状态按服务端状态与 UI 状态分离。
- API 类型由 OpenAPI 生成或集中定义，禁止在组件里手写接口形状。
- 所有异步操作处理 `loading` / `error` / `empty` / 降级四态；变更操作以 `task_id` 为准，不假设立即生效。
- 前端**不直连**数据库、消息队列、Ceph、Grafana 原生地址；一律经 Server / 反代。
- 不在前端存储凭据、token 明文放 `sessionStorage` 之外的持久位置；不在 `console.log` 打印敏感数据。

### 4.4 Python / ansible（初始化与脚本）

- Python 3.11+，类型注解，`ruff` / `black` 风格；脚本必须有 `if __name__ == "__main__":`。
- ansible playbook 使用 **FQCN**（`ansible.builtin.*`），任务写 `name`，全部幂等，禁止在 playbook 里写业务逻辑分支。
- 部署框架（`deploy/`）遵循 ADR-17：`roles/<组件>/{tasks,defaults,README.md}`，配置只在 `group_vars/all.yml`，任务按类别拆文件并打 `prechecks.<类别>` 之类 tag。
- **`prechecks` 类任务必须只读**：所有 `command` / `shell` 带 `changed_when: false`；不得装包、写 sysctl、改防火墙。
- **`fail_msg` 必须可操作**：写清「期望值 / 实际值 / 怎么修」，只写「检查失败」视为未完成；文本同样用英文。
- **Ansible 内容一律英文**：`deploy/` 下所有人类可读文本——注释、play/task/handler `name`、`debug`/`fail`/`assert` 消息、shell `echo`、模板说明、各 role 的 `README.md`——全部用英文（此条覆盖 §4.7 的「中文撰写」）。仅当引用真实存在的中文文档路径时可保留汉字，以免链接失效。变量名、`{{ ... }}`、命令、路径、包名、正则、tag、配置键/值、ADR 引用保持原样；提交前扫描 `[\p{Han}]`，除上述路径例外外应为零命中。
- 敏感值只出现在 `passwords.yml`（已被 `.gitignore` 排除），role 内用变量引用，禁止写默认口令。
- playbook 版本受控，与平台版本对应；变更需回归 3 节点 e2e。
- Shell 脚本首行 `set -euo pipefail`，变量加引号，路径用绝对路径或基于脚本位置推导；不使用 `rm -rf`、不写含非 ASCII 路径的 `.bat`/`.ps1`。

### 4.5 SQL / 数据库

- 迁移文件单向、编号递增、附带回滚说明（`db/migrations/NNNN_*.sql`）；**已应用的迁移不修改**，新变更写新文件。
- 迁移必须向后兼容（先加可空列 → 双写 → 再切读），保证代码 revert 后旧版本仍能跑。
- 表名复数、小写下划线；主键 UUIDv7；时间 `timestamptz`；JSON 用 `jsonb`。
- 审计表只追加：应用账号无 `UPDATE` / `DELETE` / `TRUNCATE` 权限，按月分区。
- 指标与日志正文**不入库**，只存引用（`log_ref`）。
- 所有变更任务加集群级互斥（PG advisory lock），不依赖应用层单点内存锁。

### 4.6 YAML / JSON / TOML / 配置

- 2 空格缩进，字符串按需加引号（尤其 `yes/no/on/off`、以 `*` 或 `{{` 开头的值）。
- 配置文件模板化，密钥用占位符 + 外部注入，**模板里不得出现真实值**。
- 环境变量命名 `WINTER_<模块>_<语义>`，全部在 `.env.example` 中登记并说明默认值。
- compose / podman pod 定义固定镜像 tag 与 digest，禁止 `latest`。

### 4.7 Markdown / Mermaid（当前仓库主要语言）

- 中文撰写，术语首次出现给中英文/缩写；表格优先于长段落；每节有明确标题层级（不跳级）。
- 相对链接引用仓库内文件，禁止硬编码本机绝对路径；链接变更必须跑 §7.3 校验。
- 每篇文档头部有元信息表（归属岗位、状态、版本/日期、依据、关联文档）。
- 状态只用四个值：`草案` / `评审中` / `已确认` / `已废弃`；已确认文档的改动需技术评审。
- Mermaid 图节点文字简短，不塞长句；图后必须有文字说明，图不是解释的替代品。
- 命令示例必须**可直接执行**；未确定的命令写 `【TODO-Dx】` 占位并注明依赖，禁止编造看似能跑的假命令。
- 一行一个句子，避免超过 200 字的段落。

### 4.8 命名总表

| 对象 | 规则 | 示例 |
| --- | --- | --- |
| 文档目录 | `两位序号-岗位线/两位序号-岗位名`，岗位序号**沿用文章原文编号**，不重排 | `02-研发线/04-架构师/` |
| 文档文件 | 中文主题 + 限定后缀，无空格 | `架构决策与权衡分析.md` |
| 提交 subject | `type(scope): 中文动词开头` | `docs(arch): 补充对账设计` |
| 分支 | `type/kebab-case` | `feat/multi-region-federation` |
| tag | `v<semver>-<阶段>` | `v0.1-docs-baseline` |
| 任务模板 type | `小写.动作` | `pool.create`、`host.precheck` |
| ADR | `ADR-NN` + 一句话决策 | `ADR-04 双能力合并为单二进制双模式` |

---

## 5. 文档规范

1. **归档规则**：文档放在「产出它的岗位」目录下；跨岗位引用用相对路径。
2. **单一索引**：`docs/README.md` 是唯一索引入口；新增/重命名/废弃文档必须同步更新它**以及对应岗位 `README.md` 的「本目录文档」清单**。
3. **状态与承诺**：`草案` 文档不得被当作已确认需求对外引用；PRD 必须写明「哪些情况不做」。
4. **待决策显式化**：D1~D11 未拍板前，所有涉及选型的表述标注 `待决策` / `暂定` / `建议`，并指向决策清单；**禁止把建议写成结论**。
5. **ADR**：架构/选型变更写入 `docs/02-研发线/04-架构师/` 的 ADR 条目（决策、理由、放弃什么、推翻条件），不要只留在聊天记录里。
6. **可追溯**：文档中的风险、弊端、待办要有稳定编号（`B1` `R3` `D1` `TC-05`），供提交信息与测试用例引用。
7. **禁止删历史**：废弃文档标注 `已废弃` 并说明原因与替代文档，不直接删除（保留回滚与追溯线索）。
8. **岗位体系变更留痕**：岗位线/岗位发生增删改时，必须同步 `docs/README.md` 目录树、各岗位 `README.md`、本文件 §1 与 §4.8，并在 `docs/岗位映射与变更记录.md` 写清旧→新路径；旧目录用 `git mv` 迁移，不做「删了重建」。

---

## 6. 架构不变量（改动代码时逐条自检）

以下来自 [架构设计](docs/02-研发线/04-架构师/架构设计-Ceph管理平台.md) 与 [权衡分析](docs/02-研发线/04-架构师/架构决策与权衡分析.md)，**违反任一条的改动应被拒绝**：

| # | 不变量 |
| --- | --- |
| I1 | 业务 IO 直连 Ceph，**绝不经过控制面**；平台全停不影响数据面 |
| I2 | 所有变更走异步任务（返回 `task_id`），禁止同步长调用；任务有状态机、幂等键、超时、死信 |
| I3 | Agent **主动拉取**任务 + 双向 mTLS；**运行期零 SSH**：平台不持 SSH 凭据、不建 SSH 通道；SSH 只存在于交付期 ansible（实施人员执行，ADR-35）。**Server 不直连任何 Ceph 端点**（mgr/cephadm/CLI 都不碰），唯一执行面是 Agent |
| I4 | 命令白名单 + 强类型参数校验 + `exec argv`；禁止自由 shell、禁止 `bash -c` 拼接。**Agent 一律非 root**：专用账号常驻，特权动作经 sudo 白名单（绝对路径 + 参数形态，禁 `ALL` 与解释器）（ADR-19） |
| I5 | 凭据静态加密，明文不出现在 API 响应、日志、审计、前端、错误堆栈。**SSH 登录凭据仅存内存、不落库不入队列**，任务终态即擦除（ADR-24） |
| I6 | 审计只追加 + fail-closed（审计写失败则任务拒绝执行）；存在全局「停止任务下发」熔断开关 |
| I7 | Ceph 实际状态为真相；DB 只是期望/流程状态；必须有 Reconciler 对账并展示采集时间 |
| I8 | Grafana 经反代统一鉴权，禁止直连，按集群过滤；不绕过平台 RBAC |
| I9 | PG 只存元数据/任务/审计；指标入 VM、日志入日志系统 |
| I10 | cephadm 是**集群级执行器**，不是中心服务；每集群独立 FSID / spec 上下文 |
| I11 | 监控/日志/镜像仓库是**可降级组件**：全挂时控制台仍能展示最近同步状态并允许提交预定义任务 |
| I12 | 保留 Ceph Dashboard 与 SSH 作为兜底；审计口径写明「平台内操作 100%」，不宣称覆盖旁路 |
| I13 | 新建集群等不可逆步骤标记 `compensable=false`，失败不自动重试，重试不重复创建资源 |
| I14 | **交付形态是硬指标**：三段式离线交付（有网制包 → 搬运校验 → 现场安装，ADR-14），安装通道为「Nginx 只读内网源 + ansible 触发」（ADR-15），目标环境零外网依赖、不编译；组件数量已不设上限（G6 已放宽，ADR-13），但新增有状态组件必须同步给出离线安装、升级、备份与自监控（对应弊端 B1） |
| I15 | **镜像唯一来源是自建 Harbor**：节点只 pull，push 仅从制包机 / CI 执行；必须 TLS + `certs.d` 信任 CA，**禁用 `insecure-registry` 与 `--tls-verify=false`**；一律使用只读 robot 凭据（ADR-16） |
| I16 | **部署编排统一走 `deploy/` 的分阶段 playbook**（ADR-17）：`prechecks` 必须只读、`deploy` 必须幂等（重复执行 `changed=0`）、配置只在 `group_vars` 单点、secrets 只在 `passwords.yml` 且不入库；playbook 内**禁止**写 hostname 判断 |

---

## 7. 变更流程

### 7.1 改动前

- 读 `docs/README.md` 与相关岗位文档，确认当前状态与待决策项。
- 确认改动属于哪个逻辑变更；若会触碰 §6 不变量，先开分支并准备 ADR。
- 待决策项未拍板时，**不替用户做决定**：按建议默认值实现并显式标注，或在文档中列为开放问题。

### 7.2 改动中

- 一次只推进一个逻辑变更。
- 文档与它描述的对象同步改（改接口 → 改 API 文档 + 前端调用 + 测试用例）。
- 不引入与当前阶段不符的重型依赖；新增依赖见 §9。

### 7.3 改动后自检（提交前必跑）

```bash
git status --short                    # 逐个审视 ?? / M 条目（§2.5 第 1 步）
git check-ignore -v <可疑文件>          # 确认无用文件确实被某条 .gitignore 规则忽略
git diff --check                      # 行尾空格 / 冲突标记
```

Markdown 链接校验（本仓库当前主要自检项）：

```bash
python - <<'PY'
import re, pathlib
root = pathlib.Path('docs'); bad = 0; n = 0
for f in root.rglob('*.md'):
    for link in re.findall(r'\[[^\]]*\]\(([^)]+)\)', f.read_text(encoding='utf-8')):
        if link.startswith(('http://','https://','mailto:','#')): continue
        t = link.split('#')[0]
        if not t: continue
        n += 1
        if not (f.parent / t).resolve().exists():
            bad += 1; print('BROKEN', f, '->', link)
print(f'checked {n} links, broken {bad}')
raise SystemExit(1 if bad else 0)
PY
```

另需人工确认：

- [ ] **已执行 §2.5**：`git status --short` 中无无用文件；新类型已补进 `.gitignore` 并复查 `status`
- [ ] 无密钥 / 凭据 / `.env` / 二进制 / 大文件进入暂存区
- [ ] 无 `TODO` 被写成既定结论；待决策项仍标注开放状态
- [ ] `docs/README.md` 索引与岗位 README 清单已同步
- [ ] 新增命令示例可直接执行，否则已用 `【TODO-Dx】` 标注
- [ ] §6 不变量未被破坏；若破坏已在 ADR 中说明并获确认
- [ ] 提交信息写清「为什么 + 影响范围 + 回滚方式」

### 7.4 提交后

`git status --short` 干净 → `git log -1 --stat` 回看 → 若达到阶段出口，按 §3.2 打 tag。

---

## 8. 测试与发布门槛

- **代码**：单测覆盖状态机、幂等键、参数校验、RBAC 判定、白名单；`go test -race` / `tsc --noEmit` 通过。
- **集成**：cephadm / ansible / Ceph 行为**不得用 mock 代替验收**，必须在 3 节点 CentOS Stream 9 + podman 环境跑。
- **每次发布必跑**：[测试用例](docs/03-测试线/06-测试工程师/测试策略与验收用例.md) 中的 TC-01/03/05/07/09/13/15/16/17/19。
- **发布门槛**（PRD §9）：P0 用例 100% 通过、命令注入 0 通过、凭据明文 0 处、离线部署由未参与开发者独立完成、管理面全停 10 分钟数据面不受影响。
- **涉及 migration / 协议 / 配置格式的改动**：必须验证「新代码 + 旧数据」与「回滚后旧代码 + 新数据」两种组合，否则不允许合并到 `main`。

---

## 9. 依赖与选型

1. D1~D11 未拍板前，代码中的具体实现以**接口**表达，不把某个中间件写死进领域层。
2. 新增依赖必须说明理由、替代方案、离线获取方式、许可证、维护活跃度，并写进 ADR。
3. 镜像固定 tag + digest；离线制品走制品库，**不进 git**（`*.tar` / `*.tar.gz` 已忽略）。
4. 优先复用客户现网已有的监控栈 / 镜像仓库 / PKI，降低交付体积与运维负担（弊端 B1）。
5. 锁定 OS、podman、Ceph、cephadm、Agent、Server 的兼容矩阵；升级前跑回归。
6. 不因为「看起来更先进」引入组件；团队不熟时优先选运维能力覆盖得到的方案。

---

## 10. 安全与密钥

- 密钥、凭据、证书、keyring 一律不进仓库；`.env` 只提交 `.env.example`。
- 提交前 `git diff --cached` 目视检查敏感字符串；发现误提交**不要只删文件**，需按安全事件处理（轮换凭据 + 清理历史需用户确认）。
- 高危动作（删 OSD、改副本数、停服务、集群重置）代码路径必须有二次确认 / 双人复核钩子。
- 安全设计以 [安全与审计设计](docs/07-新兴与稀缺线/19-安全工程师/安全与审计设计.md) 为准，MVP 上线前完成其检查清单。

---

## 11. 禁止事项（No-Go 清单）

| 类别 | 禁止 |
| --- | --- |
| 版本控制 | 见 §2.6；另禁止自动 push、自动建 PR、自动改写他人提交 |
| 数据 | 删除 / 清空 `.workbuddy/`；删除已废弃文档（应标注保留）；批量删除个人目录文件 |
| 安全 | 提交密钥；跳过鉴权校验；`bash -c` 拼接用户输入；打印凭据明文 |
| 架构 | 让业务 IO 经过控制面；把 cephadm 中心化；把 DB 状态当真相；绕过 Grafana 反代 |
| 流程 | **提交前未检查 / 未按需更新 `.gitignore`**；未自检就提交；自检失败仍标记「完成」；把待决策项写成结论；改动文档不更新索引 |
| Ansible | `deploy/` 中出现中文注释 / task name / 消息文案 / role 文档正文（真实中文文档路径引用除外）；翻译时改动变量、Jinja、命令、路径或 tag |
| 环境 | 擅自安装 git hook / CI / 全局工具；在 `main` 上做不可逆实验；修改用户未授权的外部系统 |

---

## 12. AI 助手专属约定

1. **每次改动立即提交**，包括本文件自身的修改；任务结束时 `git status` 必须干净。
2. **不自动 push、不自动建 PR、不自动合并分支**，除非用户明确要求。
3. **提交信息用中文 subject + 结构化 body**，写清回滚方式，让未来的自己能靠 `git log` 独立还原上下文。
4. **不替用户拍板待决策项**；缺信息时按建议默认值实现并标注，或提出明确问题，不臆造。
5. **上下文丢失时**：先读本文件 → `git log --oneline -20` → `docs/README.md` → 相关岗位文档，再继续工作。
6. **不以「已完成」报告失败或部分完成的工作**；自检失败、链接断裂、测试未过，保持任务未完成状态并说明阻塞。
7. **不修改 `.workbuddy/` 中的记忆文件以外的内容，也不删除该目录**；记忆写入是追加，不覆盖。
8. **新增文档 / 重命名 / 废弃必须同步索引**，否则视为改动未完成。
9. **遇到 §6 不变量冲突**：先停下来说明冲突与备选方案，等确认，不擅自绕过。
10. **大改动先说计划**：涉及多个岗位文档或跨语言代码时，先列出将产生的提交拆分，再动手。
11. **每次 commit 前先过 `.gitignore` 检查**（§2.5）：先看 `git status --short`，发现新的无用文件类型就先补忽略规则、复查 `status`，再 `git add`；不把「临时文件这次先带上」当作可接受理由，也不为了凑改动写无意义的忽略规则。
12. **Ansible 一律英文**（§0 / §4.4 / §11）：新增或修改 `deploy/` 下任何文件时，所有人类可读文本用英文；提交前扫描 `[\p{Han}]`，除真实中文文档路径引用外应为零命中。

---

## 13. 配套文件（落地清单）

| 文件 | 状态 | 作用 |
| --- | --- | --- |
| `.gitignore` | **已建立 · 每次 commit 前按需更新（§2.5）** | 忽略 `.workbuddy/`、密钥、缓存、备份、临时与离线制品 |
| `.gitattributes` | **已建立** | 强制 LF、标记二进制；避免 CRLF 造成整文件 diff 与回滚噪音 |
| `.editorconfig` | 建议（代码出现前建立，单独提交） | 统一缩进 / 换行 / 编码 |
| `.env.example` | 建议（配置出现时建立） | 登记全部环境变量与默认值，不含真实值 |
| `pre-commit` hook | 建议（需用户同意后安装） | 跑 `git diff --check` + 链接校验 + lint |

`.editorconfig` 建议内容（采用时新建文件并单独提交）：

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true
indent_style = space
indent_size = 2

[*.go]
indent_style = tab

[*.md]
trim_trailing_whitespace = false
```

`.gitattributes` 已落地内容。**为什么现在就建**：Windows 下 `core.autocrlf` 会在下次 checkout 把工作区 LF 转成 CRLF，产生整文件 diff，让 `git blame` / `git revert` 失去按行定位能力；这与「方便回滚」直接冲突。

```gitattributes
* text=auto eol=lf
*.png binary
*.jpg binary
*.tar binary
*.tar.gz binary
```

> 若某人希望把 `.workbuddy/` 纳入版本控制，需**先征得同意**再修改 `.gitignore`，并单独提交说明原因。

---

## 14. 变更记录

| 日期 | 变更 | 提交 |
| --- | --- | --- |
| 2026-11-08 | 初版：提交纪律、回滚、代码/文档风格、架构不变量、自检与禁止清单 | `4117ff1` |
| 2026-11-08 | 新增「每次 commit 前检查并按需修改 `.gitignore`」为强制步骤（§0 / §2.4 / §2.5 / §7.3 / §11 / §12 / §13），并把原 §2.5「明令禁止」顺延为 §2.6；同步补充 `.gitignore` 高频类型 | `d7c1045` |
| 2026-11-15 | 岗位体系随文章更新：7 族 22 岗 → 7 条线 24 岗。同步 §1 目录约定与关键入口、§4.8 目录命名（岗位序号沿用原文编号）、§5 新增「岗位体系变更留痕」、§6/§9/§10 文档路径；旧目录用 `git mv` 迁移，对照见 `docs/岗位映射与变更记录.md` | `3a38fbe` |
| 2026-11-15 | 技术栈 7 项确认并落成 `ADR-11`：§1 技术栈从「拟定」改为「已确认 + 待定清单」；架构设计 §4.1 增状态列、§14 ADR 索引增状态列并把 ADR-07 退回 `Proposed`；权衡分析 §8 与 PRD §11.1 标注 D2/D6/D10 部分收敛 | `5093ab2` |
| 2026-11-15 | D1 关闭：消息队列定为 **RabbitMQ**，落成 `ADR-12`（ADR-07 标 `Rejected`）。同步架构设计 §3/§4.1/§5.1/§6/§10/§13/§14、权衡分析 R7/R10/§8、PRD 里程碑与 D1、API 分片并发约定、部署资源估算（10C/15G → 11C/16G）、安装手册前提说明 | `269c711` |
| 2026-11-15 | G6 放宽：由「部署单元尽量少」改为「**交付可控**」，落成 `ADR-13`。B1 从「目标自相矛盾·最核心弊端」降级为「成本转移」；§5.1 风险矩阵、§2 R7/R10、§6 备选 D、§9 第二件事、架构设计 §1.1/§4.1/§4.2、PRD R4、项目计划 M0/RK-1、部署方案 §1 标题、I14 与 §10.4 同步；新增待定项「离线包体积/启动时间上限」 | `2c5c01a` |
| 2026-11-15 | G4 落地为「离线可搬运」：有网制包 → 搬运校验 → 现场远程安装，落成 `ADR-14`。安装部署手册重构为三段式（v0.2，含 BOM/digest/无跳板机降级/断网重装验收）；同步架构设计 §1.1/§6.1、PRD 部署目标与 FR-10/D6、TC-22、权衡 §1/B1、部署方案、I14 | `aa969cf` |
| 2026-11-15 | 安装通道定案：**Nginx 只读内网源 + ansible 批量安装**，落成 `ADR-15`。明确两条边界——镜像走不了 yum（registry 或静态 tar，成为 D3 新候选）、明文 HTTP 必须配包与 repodata 双重签名；手册阶段 C 重排为 C1~C10（起源→前置检查→装包→镜像→配置→启动→Agent→初始化→纳管→验收），验收新增源签名与限网段检查；同步架构设计 §6.1、PRD FR-10、TC-22、权衡 D3、I14 | `7a812b0` |
| 2026-11-15 | D3 关闭：**自建 Harbor，节点统一经仓库拉取**（ansible 下发 CA 与只读 robot 凭据），落成 `ADR-16`；静态 tar 降级为应急路径。新增不变量 I15（镜像唯一来源、禁 insecure-registry）；部署资源 11C/16G/220G → 14C/22G/300G；手册 BOM/C4/验收、TC-25、架构设计 §4.1/§6.1/§6.2/§14、权衡 §8、部署方案同步 | `dba8bc5` |
| 2026-11-15 | 部署框架定案：**Ansible，结构对齐 kolla-ansible，不 fork**，落成 `ADR-17`。阶段 C 由 `deploy/` 分阶段 playbook 驱动（bootstrap-servers / prechecks / pull / deploy / post-deploy / reconfigure / upgrade / destroy）；手册升 v0.3（C1~C8）；新增不变量 I16 与 TC-26（prechecks 只读、deploy 幂等）；目录约定登记 `deploy/` | `c07d334` |
| 2026-11-15 | 四项决策关闭：**D4/D5 可观测性**（VictoriaLogs + 自带全套，`ADR-18`）、**D9 Agent 权限**（专用账号 + sudo 白名单，`ADR-19`）、**D11 适用下限**（≥3 集群或 ≥20 节点，`ADR-20`）。I4 增加「Agent 一律非 root」；新增 TC-27（提权边界）；架构设计 §1.2/§3/§4.1/§6.1/§11/§14、权衡 §8、PRD D4/D5/FR-09/§2.1、安全设计、部署资源表、手册 C5 与前置表同步 | `7f94f30` |
| 2026-11-15 | D7 关闭：认证源定为**本地账号 + V1 预留 LDAP/OIDC**（`ADR-21`）。组件层选型全部收敛；架构设计 §1.2/§4.1/§11/§14、权衡 §8、PRD D7 同步 | `fe4a2b0` |
| 2026-11-15 | 落地 `deploy/` 骨架与 prechecks role（ADR-17 首批代码）：`deploy.sh` 阶段分发、inventory 双形态、单一 `group_vars`、只读预检（OS/资源/时间/内核/网络/端口/制品）；`.gitignore` 排除 `passwords.yml`；§1 目录约定与 §4.4 补充 ansible 约定 | `ea0dff0` |
| 2026-11-15 | **D7 改判**：LDAP 从「V1 预留」改为「MVP 可配置项」，落成 `ADR-22` 并取代 `ADR-21`（原文保留）。新增 TC-28（LDAP 五场景 + 明文拒绝）；安全设计 §3/§8、PRD D7、架构设计 §4.1/§11/§14、权衡 §8、手册 C6、`deploy/group_vars` 的 `platform.auth` 同步 | `4868c4c` |
| 2026-11-15 | 修正 §5.1 三处矛盾并定「预检执行者」：合并为单 Agent、回写一律经 Server、预检由**管理面 Runner 复用 deploy prechecks** 执行（`ADR-23`）。新增 `tasks.executor`（agent/runner）与互斥规则；架构设计 §3/§5.1/§8/§9/§14、安全设计 §1 信任边界图、TC-29、I3 例外条款同步 | `c6f07c5` |
| 2026-11-15 | SSH 凭据定案：**内存态一次性、不落库不入队列**（`ADR-24`），解决 ADR-23 遗留项。`credentials` 表明确只存 Ceph 凭据；`hosts` 增 `ssh_host_key_fp`；新增 TC-30；I5 增补；安全设计 §3、架构设计 §8/§11、手册 C7 同步 | `efe40f4` |
| 2026-11-15 | 任务状态机定案：**自动重试不动任务号、人工重跑开新单**（`ADR-25`）。去掉 `TIMEOUT` 状态（改用 `last_progress_at` 告警），删除 `FAILED→PENDING` 回流；`tasks` 增 `parent_task_id`/`cancel_requested_at`/`last_progress_at`，`task_steps` 改 `attempt_count`/`last_error_code`/`next_retry_at`/`compensable`；新增 TC-31；API 文档状态机同步修正 | `a9beb88` |
| 2026-11-15 | 会话存储定案：**PG sessions 表 + 不透明 Session ID**（`ADR-26`）。修正 §10「JWT/Redis 可选」与「服务端可撤销」的矛盾；新增 §5.5 认证与会话链路图；`users`/`sessions` 表补齐；API 增 `/auth/*` 并把 retry 语义改为「开新单」；新增 TC-32 | `89c0cc4` |
| 2026-11-15 | 纳管定案：**双通道**（managed 装 Agent / limited 只读 mgr API，`ADR-27`）。新增 §5.6 纳管链路；`clusters` 增 `collector`、`credentials.type` 增 `ceph_key`/`dashboard`；明确「拿不到 mgr API 就不支持纳管、不许偷偷退 SSH」；新增 TC-33 | `ee230d9` |
| 2026-11-15 | 证书体系定案：**内置两层 CA + 私钥不出节点 + DB 状态双校验吊销**（`ADR-28`）。新增 §5.7 证书链路；`agent_certs` / `ca_state` 表；`audit_logs` 补 `trace_id`；安全设计清单增节点身份项；新增 TC-34 | `e4a7bb3` |
| 2026-11-15 | 审计落点定案：**独立审计库 + 两段式写入**（`ADR-29`）。跨库无共同事务，故 fail-closed 落在 intent、result 幂等补写 + 每日对账；新增 §5.8 链路；业务库撤 `audit_logs`；部署资源 14C/22G/300G → 15C/24G/350G；新增 TC-35 | `cbd5399` |
| 2026-11-15 | 镜像拉取定案：**Ceph 镜像由 cephadm 直拉，平台不预拉**（`ADR-30`）；管理面组件仍走 `pull` 阶段。补三件兜底：digest 引用禁裸 tag、`image_pull_failed` 错误码、拉取专用超时；`image_inventory` 表与 `clusters.image_digest`；新增 §5.9 链路与 TC-36 | `d0ba3ac` |
| 2026-11-15 | 备份恢复定案：**备数据/密钥/CA/配置，不备镜像与日志**（`ADR-31`，RPO ≤15min / RTO ≤2h）。硬约束：密钥与备份包**物理分离**、备份包加密、每季演练、连续失败升级告警；新增 §5.10 链路、备份卷 ≥100G（不计入 350G）、TC-37 | `e139d5f` |
| 2026-11-15 | 保留期定案：**可配置 + 审计 180 天硬下限 + 容量校验**（`ADR-32`）。默认 30/30/90/180/7；`clusters` 补 `drift_state`/`last_drift_at`；`group_vars` 增 `platform.retention`；分区 DROP 与元审计；新增 TC-38 | `e346102` |
| 2026-11-15 | 日志与告警链路定案：**Agent 直推 VictoriaLogs（mTLS 鉴权）+ 告警 HMAC webhook 回流**（`ADR-33`）。Server 不代理日志字节；**日志不可用不阻塞任务**；告警可丢、审计不可丢；§5.4 重写为指标/日志/告警/面板四段；新增 TC-39 | `05f784c` |
| 2026-11-15 | 复核 6 处矛盾：①②③ 维持（单进程 / 经 Server 写库 / Runner 预检），④ 确认不设 TIMEOUT 状态并**补两个必配项**（显示最后进展时间、告警阈值按任务类型分级），⑤ 维持开新单；⑥ 改为 **JWT + 版本号/黑名单**，落成 `ADR-34` 取代 `ADR-26`（踢人 ≤30s 窗口，可配 0 换即时）。`sessions` 表改为 `refresh_tokens` + `token_blacklist` + `users.token_version`；`group_vars` 增 `auth.session`；TC-32 重写 | `07d0e2f` |
| 2026-11-15 | **架构简化**：定「Agent 交付期预装 + 运行期零 SSH + Server 不碰 Ceph + 纳管单位=cluster」，落成 `ADR-35`，取代 `ADR-23`/`ADR-27`、缩小 `ADR-24` 范围。取消 Runner 与 `executor=runner`；`limited` 改为「只读 Agent」（不再需要 mgr dashboard）；`hosts.cluster_id` 允许空（未分配池）；删除 `clusters.collector`；预检改由 Agent 本地实现并与 deploy prechecks 对齐；架构设计 §3/§5.1/§5.6/§5.7/§7/§8/§11、安全设计 §1、全景与图册、TC-29/TC-33、I3 同步 | 本次提交 |
| 2026-11-15 | 消息队列取消：落成 `ADR-36` 取代 `ADR-12`。ADR-35 删掉 Runner 后队列无消费者、且任务状态本就在 PG，两份状态会漂移；改为 **PG 任务表 + `SKIP LOCKED` + 租约**（`leased_until`/`next_retry_at`/`DEAD_LETTER` 状态值）。组件 -1、资源 15C/24G/350G → **13C/22G/330G**；同步架构设计 §1/§2/§3/§4.1/§5.1/§5.2/§5.8/§6.1/§9/§10/§14、部署方案、手册 C5、全景与图册、API 分片并发、ADR-13/20/29、prechecks 注释、TC-40、技术栈 | 本次提交 |
| 2026-11-15 | 容量模型定案（`ADR-37`）：设计上限 **10,000 节点 / 100 集群 / 单 Region**；单副本连接上限 5,000，10k 需 3 副本；**心跳只更新内存、30s 批量落库**（否则 1,000 写/秒打穿 PG）；多副本任务按「本地连接优先」领取 + 租约兜底；重连 0–300s 抖动 + accept 限速；证书签发按批打散；**每集群只由一个采集代表跑 `ceph -s`**。同步架构设计 §1/§5.2/§9/§10/§12/§14、部署方案 §7.1 规模阶梯、全景 ⑩、图册 §4.6、TC-41 | 本次提交 |
| 2026-11-15 | 通信分片定案（`ADR-38`）：目标 **100,000 节点**。按机房分 **20 片**（每片 ≤5k 连接）× **2 个对等副本**（40 个 Server 进程）；Agent 交付期**静态归属**（主 endpoint + 2 备用，备用不指向同片）；`tasks` 增 `shard_id`，副本只领「本片 + 本地有连接」的任务；一条 mTLS 连接承载控制/任务/结果三流，**日志不走这条连接**；`REDIRECT` 优雅迁移（宽限 300s、50/s/片）。ADR-37 的单控制面上限保持 10k，>10k 走本 ADR | 本次提交 |
| 2026-11-15 | 新增《Agent-Server 通信协议（v1）》：传输栈（TLS1.3 + HTTP/2 + gRPC + protobuf、1MiB 上限、**不提供长轮询降级**）、证书即身份（node_id 必须与 HELLO 一致）、3 条逻辑流（control/task/result）、12 类消息与字段、任务 ACK/窗口 4/幂等键、心跳 10s 与 SUSPECT 60s、REDIRECT 迁移、9 个错误码、版本 N/N-1 | 本次提交 |
| 2026-11-15 | **撤回分片设计**：项目决定规模问题在部署层解决（部署多套独立实例），架构内不做分片。`ADR-38` 标 **Withdrawn**（保留作参考）；删除图册 §1.1–§1.3（分片逻辑/路由/部署视角）与 TC-42；协议去掉 `shard_id`/`REDIRECT`/`SHARD_MISMATCH`，§8 改为「重连与故障转移」；架构设计 §1/§5.2/§8 与全景 §2.2、部署方案 §7.1 同步为「单套上限 10k，超出则多套实例」 | 本次提交 |
| 2026-11-15 | `deploy` 阶段落地：新增 `roles/postgresql`（收敛式：复用已有实例、只增库与账号、默认不改参数）与 `roles/nginx`（叠加式：只写 conf.d、不抢 default_server、靠 server_name 分流）+ `playbooks/deploy.yml`。在 CentOS Stream 9 实机跑通（ok=25/failed=0，二次运行 changed=0，既有站点与库未受影响）。修掉 5 个实机才暴露的问题：passwords.yml 不被自动加载、deploy.sh 执行位、pg_isready 不在 PATH、dnf 被 pgdg 源拖死、**group_vars 从未被加载**（+字典整块替换改 combine） | 本次提交 |
| 2026-11-15 | `server/`（Go + Gin）落地：M0 可运行骨架（配置/双连接池/embed 迁移/JWT/审计两段式/REST）、`ADR-36` 任务表队列（`SKIP LOCKED` + 租约 + 退避 + 死信 + 队列深度）、幂等键与任务模板白名单、表驱动单测与真实 PG 集成测试；修复骨架中 `audit.VerifyChain` 的 `prev` 类型错误与 `Queue.Stats` 的非法 `FILTER`；§1 目录约定登记 `server/`、阶段状态更新；`.gitignore` 排除 Go 构建产物与 vendor；`server/README.md` 记录配置、接口与实机验证结果 | 本次提交 |
| 2026-11-15 | 离线交付补齐可观测性栈与 Server 部署：新增 `scripts/fetch-artifacts.sh`（有网制包、版本固定 + `SHA256SUMS.txt`），5 个监控组件静态包与 Go 单二进制入库；新增 `roles/observability`（VM/vmalert/VictoriaLogs/Alertmanager/Grafana 数据驱动，端口全在 group_vars）与 `roles/winter-server`（systemd + `mv` 原子替换避 ETXTBSY）；`deploy.yml` 收敛顺序扩展、Nginx 增 `/grafana/` 反代（ADR-09）、`passwords.yml.example`；Grafana 锁 9.5.20、vmalert 二次打包，以规避 GitHub 单文件 100MB 上限；§1 目录约定与 `deploy/README.md` 同步 | 本次提交 |
| 2026-11-16 | Ansible 全英文约束入契约：§0 铁律新增第 12 条、§4.4 明确范围与例外（覆盖 §4.7 中文撰写）、§11 No-Go 新增 Ansible 行、§12 AI 约定新增第 12 条；`deploy/` 全量英文化已先行单独提交 | 本次提交 |
