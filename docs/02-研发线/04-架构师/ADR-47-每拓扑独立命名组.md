# ADR-47: 每拓扑独立命名组（组名即软件角色）

| 项目 | 内容 |
| --- | --- | 
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-46 统一 inventory 与 LVS 角色拆分](./ADR-46-统一inventory与LVS角色拆分.md) |
| 回滚锚点 | `v0.8-pre-per-topology-groups` @ `6a3de01` |

## 1. 问题

ADR-46 把 inventory 合成一个文件，但 9 个组件组（postgresql/nginx/winter-server/harbor/
keepalived/named/observability/local-repo/system-init/agent）仍全是 `:children management`——
所有机器在所有组，从 inventory 看不出"哪个软件跑在哪台机器"。用户参照
`[lvs-dr-director]`/`[lvs-dr-realserver]` 的"组名即角色"风格，要求所有软件都这样表达。

## 2. 决策

1. **每个拓扑 × 每个软件一个明确命名的组**，host 直接列出。命名 `<拓扑>-<软件>`：
   `all-in-one-postgresql`、`multinode-nginx`、`pgha-etcd`、`lvs-dr-winter-server` 等。
   组名即角色，一眼看出该拓扑里哪个软件跑在哪些机器。
2. **保留通用父组**（`postgresql`/`nginx`/... 用 `:children` 聚合所有拓扑的同名组），
   使 `site.yml` 的 `hosts: postgresql` 无需修改；`--limit <拓扑>` 取交集，
   自动只作用于该拓扑的主机。
3. **空拓扑组保留定义**（如 `pgha-nginx`、`pgha-winter-server`），确保父组 `:children`
   解析不报错。
4. LVS 的 `[lvs-dr-director]`/`[lvs-dr-realserver]` 风格不变，作为"组名即角色"的范例。

## 3. 期望拓扑

| 软件 | all-in-one | multinode | pgha | lvs-dr |
| --- | --- | --- | --- | --- |
| system-init | localhost | 全部4台 | 全部3台 | 全部3台 |
| postgresql | localhost | mgr01 | pg-a/b/c | ctl/node-b/node-c |
| nginx | localhost | mgr01 | （空）| node-b/c |
| winter-server | localhost | mgr01 | （空）| ctl/node-b/node-c |
| harbor | localhost | mgr01 | （空）| ctl/node-b/node-c |
| keepalived | localhost | mgr01 | （空）| ctl（Director）|
| named | localhost | mgr01 | （空）| ctl |
| observability | localhost | mgr01 | （空）| node-b/c |
| local-repo | localhost | mgr01 | pg-c | ctl |
| agent | （空）| node01/02/03 | （空）| （空，另有 scale-out-agent）|

## 4. 放弃什么

- **inventory 膨胀**：从 ~90 行增至 ~250 行（约 40 个组）。换来的是每个拓扑的软件分布一眼可读。
- **拓扑变化时组要同步改**：加一台机器要同时加进它承担的每个软件组（而非只进 management）。
  这是"显式表达"的必然代价。

## 5. 真机回归（2026-11-25，通过）

- inventory 解析 RC=0；`[management]`=11 台、`[postgresql]`=8 台、`[lvs-dr]`=3 台。
- `--limit pgha` 时 `hosts: postgresql` → 仅 pg-a/b/c；`--limit lvs-dr` 时 `hosts: nginx` → 仅 node-b/c（交集生效）。
- `./deploy.sh prechecks -i inventory/hosts.ini --limit lvs-dr -t system-init,nginx` RC=0，3 节点 failed=0。
- site.yml syntax-check 通过。

## 6. 已被 ADR-48 取代（2026-11-25）

本 ADR 的"每拓扑独立命名组 + 父组 + --limit 交集"结构过于复杂（251 行、~40 组），
用户要求极简。[ADR-48 扁平 inventory](./ADR-48-扁平inventory每进程一组.md) 将其取代：
一个进程一个组、组内直接写 IP，无拓扑组/父组。本 ADR 保留为历史决策记录。
