# ADR-59: tag / role / inventory 组名三者强制同名

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-27 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-49 组名统一为软件名](./ADR-49-组名统一为软件名.md)、[ADR-55 允许超融合](./ADR-55-允许超融合部署.md)、[ADR-58 podman 独立 role](./ADR-58-podman独立role.md) |
| 回滚锚点 | `v0.12-pre-triple-name-align` @ `1302c40` |

## 1. 问题

用户指出："tags、role 和 inventory 中的 [] 名也要相同吧"。核查后发现三类不一致：

| 类型 | 例子 | 问题 |
| --- | --- | --- |
| A · 名字不同 | `system-checks` role + `[system-checks]` 组，tag 叫 `prechecks`；`lvs-director` role+tag，组叫 `[keepalived-director]` | 看 tag 不知对应哪个 role |
| B · 缺入口 | `haproxy`/`patroni`/`reconfigure`/`upgrade`/`verify`/`uninstall` 有 role 但无同名 tag/组（或不全）| 无法按软件名选择 |
| C · 组无 role | `[vmalert]`/`[victorialogs]`/`[alertmanager]`/`[grafana]` 有组无 role；`[pg-primary-candidate]`/`[pg-repo]` 死组 | 违反一一对应 |

## 2. 决策：26 个 role = 26 个 tag = 26 个 inventory 组

用户选择"全部强制一一对应"，包括把监控拆成 5 个完全独立 role（推翻 ADR-49 的"5 软件组同主机由一个 role 管"）。

### 2.1 A 类改名

- `prechecks` tag → `system-checks`（含子 tag `prechecks.*` → `system-checks.*`）；
  **action 名 `./deploy.sh prechecks` 不变**（那是操作不是 tag）。
- `[keepalived-director]` → `[lvs-director]`，`[lvs-backend]` → `[lvs-realserver]`。

### 2.2 B 类补入口

- `haproxy` / `patroni`：新增同名 play（`hosts`/`tag`/`role` 三者同名）+ inventory 组，
  位置在 postgresql play 之后；仍可由 `postgresql-ha` 连带部署（幂等）。
- `reconfigure` / `upgrade` / `verify`：tag 已同名，新增同名 inventory 组（与 etcd 同 IP），
  site.yml 对应 play 的 `hosts` 从 `etcd` 改为各自组名。
- `uninstall`：tag `destroy` → `uninstall`；`deploy.sh destroy` **action 名不变**，
  但其默认 ACTION_TAGS 改为 `uninstall`。

### 2.3 C 类拆分与清理

- 监控拆 5 个完全独立 role：`victoriametrics` / `vmalert` / `victorialogs` / `alertmanager` / `grafana`，
  每个有自己的 `deploy.yml` / `precheck.yml` / handler / 模板；site.yml 各加同名 play。
  `victoriametrics` role 收窄为只部署自身组件（原来是 5 个循环）。
  **配置仍集中在 `victoriametrics.components.<name>`**，不重复定义。
- `pg-primary-candidate` / `pg-repo`：全仓零引用的死组，直接删除。

## 3. 关键实现与踩坑

- **tag 名 ≠ action 名**：`prechecks` / `destroy` 是 CLI action（`winter_ops_action` / deploy.sh case 分支），
  保持原名；只改它们映射到的 tag。改 action 名会破坏用户习惯与文档。
- **新 role 必须建服务账号**：拆分后的 vmalert role 初次部署 `chown winter-monitor` 失败——
  原一体化 role 有"Create service account"任务，生成新 role 时漏了。4 个新 role 都补上。
- **Grafana 是目录模式**：`unarchive` 要求 `dest` 目录预先存在，新 role 的目录 loop 必须含
  `comp.install_dir` 与 provisioning 子目录，否则 `dest must be an existing dir`。
- **handler 文件生成**：脚本里字符串 `\n` 未转义写成字面 `/n`，导致 YAML 语法错误；
  用 `printf` 重写修复。

## 4. 验证（2026-11-27）

- **三者一致性**（脚本统计）：`roles=26 groups=26 role_tags=26`；
  `missing group=[]`、`missing tag=[]`、`extra group=[]`。
- `ansible-playbook --syntax-check` 通过。
- `prechecks -t system-checks`：三台 `failed=0`。
- 5 个监控 role 分别 `deploy -t <name>`：全部 RC=0，三台 `failed=0`；
  最终 15 个监控服务（5 组件 × 3 台）全部 active。
- `verify` play 在 tag 改名后正常执行（当前环境未部署 Server，故按预期报 8081 不健康，
  证明 tag 路由生效）。

## 5. 已知取舍

- ADR-49 的"5 个监控软件组同主机"语义保留（组仍在、同 IP），但**实现**从"一个 role 循环"
  改为"5 个独立 role"，代码有一定重复（每个 role 自己的 fetch/install/unit/handler）。
- 配置未拆：`victoriametrics.components.<name>` 仍是 5 个组件的唯一配置源，新 role 读取它；
  这样三者同名不牺牲配置集中原则（ADR-50/53）。
- 操作型 role（reconfigure/upgrade/verify/uninstall）的组与 etcd 同 IP，是"操作目标"不是
  "部署目标"，但依然满足三者同名。
