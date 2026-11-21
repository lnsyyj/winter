# ADR-42 · 单入口部署架构（对齐参考运维框架）

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-21 |
| 决策人 | 项目负责人 |
| 关闭 | 部署入口分散在 25 个 stage playbook、同一组件的部署/预检动作散落各处 |
| 关联 | [ADR-17 部署框架](ADR-17-部署框架.md) · [ADR-41 stage 命名对齐软件名](ADR-41-stage命名对齐软件名.md) · 参考框架 `yunhai-deploy-ops/tools-playbooks/site.yml` |

---

## 1. 背景与动机

原框架（ADR-17/41）：`deploy.sh` 按 stage 分发到 25 个独立 playbook，一个组件的「预检 / 部署 / 扩容」动作散落在不同文件；编辑 inventory 时组名与进程名不一致。用户要求对齐参考运维框架（`yunhai-deploy-ops`）的做法：

- 单一 `site.yml` 入口，`autumn_ops_action` 变量选动作；
- 每个组件一个 play/role，role 的 `tasks/main.yml` 用 `include_tasks: "{{ action }}.yml"` 分发；
- inventory 按操作组织，一次操作打开需要的组件组。

## 2. 决策

### 2.1 从参考框架照搬的部分

1. **单一入口 `playbooks/site.yml`**：所有部署/预检动作从它进入。每个组件一个 play，`hosts` = 组件组，`tags` = role 名，`any_errors_fatal: true`。
2. **role 内动作分发**：16 个组件 role 的 `tasks/main.yml` 改为一行 `include_tasks: "{{ winter_ops_action | default('deploy') }}.yml"`，部署逻辑迁 `deploy.yml`，预检迁 `precheck.yml`。命令 `prechecks` 映射为 `winter_ops_action=precheck`（与参考框架一致的单数命名）。
3. **操作型 inventory / 变量**：新增 `inventory/operations/*`（一次操作一个主机清单）与 `group_vars/operations/*.yml`（操作特定变量，如 `pg_scale.nodes`、`server_ha.database_host`），用 `-e @file` 注入。
4. **ansible.cfg**：`gathering=explicit` + fact caching + `force_valid_group_names=ignore`（带连字符的组名保持原样）。

### 2.2 与参考框架的偏差（必须说明，非疏漏）

参考框架的 play 都是**单 host 组、单 role、无跨 play 状态**，所以能全部平铺在 site.yml。winter 有 7 个**跨主机、有状态、强顺序**的事务编排，无法压成单 role：

| 编排 | 为什么不能压成单 role |
|---|---|
| `pg-ha` / `pg-scale-out` / `pg-scale-in` / `pg-migrate` | set_fact 链、`etcd_initial_cluster` 从 etcdctl 输出解析、run_once 共享 register、逐节点 learner/promote、serial:1 |
| `observability-scale-out/in` | localhost play 用 add_host 建动态组，后续 play 依赖；最后重写 nginx upstream |
| `app-ha` | 4 个 play 分属 winter-server/harbor/nginx/keepalived 四个主机组，各自计算 upstream/DB 端点 |

这些继续以**独立 playbook**存在，由 `site.yml` 用 `import_playbook` 按依赖顺序收编（CLI 仍只有 site.yml 一个入口），其 `deploy.sh` 命令形如 `deploy -t <编排tag> -e @group_vars/operations/<op>.yml`。

**生命周期动作**（destroy / verify / reconfigure / upgrade）**不 import 进 site.yml**：它们内部 task tag 与组件名重叠（如 upgrade 内有 `postgresql` tag），import 后会泄漏到 `-t postgresql` 的常规部署，可能误触发升级/卸载。`deploy.sh` 对这些动作直连原 playbook。同理，组件动作自动附加 `--skip-tags` 排除全部操作 tag。

### 2.3 命令接口（彻底切换，无兼容别名）

```
./deploy.sh <action> [-i inventory] [-t tag,...] [-e @vars.yml] [ansible args...]
action = deploy | prechecks | destroy | verify | reconfigure | upgrade
```
- 不带 `-t`：`deploy`/`prechecks` 只跑组件；事务操作必须显式 `-t <编排tag>` 选中（防误执行 migrate/scale）。
- 旧 stage 名（如直接 `./deploy.sh pg-ha`）全部移除。

## 3. 为什么这样设计

| 备选 | 放弃原因 |
|---|---|
| 全部平铺成单 role（用户选项三） | 事务编排有跨 play 状态和顺序，压平会破坏已真机验证的扩容/迁移链路 |
| 生命周期动作也 import 进 site.yml | 内部 tag 与组件名冲突，`-t postgresql` 会触发 upgrade/reconfigure 任务——已实测确认，危险 |
| 保留双层入口（旧 stage 别名） | 用户要求彻底切换 |
| 不动 inventory 分组 | 用户要求增加操作型 inventory；同时保留拓扑 inventory 并存的灵活性 |

## 4. 影响

| 对象 | 变化 |
|---|---|
| `playbooks/site.yml` | 新增唯一入口：12 个组件 play + 7 个 import_playbook 事务编排 |
| `deploy.sh` | 改为动作入口；生命周期动作直连原 playbook；自动 skip-tags |
| 16 个组件 role | `tasks/main.yml` 改分发器，新增 `deploy.yml` / `precheck.yml` |
| `inventory/operations/` `group_vars/operations/` | 新增操作型清单与变量 |
| `ansible.cfg` | fact caching / force_valid_group_names |

## 5. 风险与缓解

| 风险 | 缓解 |
|---|---|
| tag 泄漏触发危险操作 | 组件动作强制 `--skip-tags <所有操作tag>`；生命周期动作不 import |
| include_tasks 运行时变量求值 | 用 `include_tasks` 而非 `import_tasks`，保持延迟求值 |
| 旧 playbook 与 role 分发器并存期行为漂移 | 分发器 `default('deploy')`；过渡期旧 stage 仍可调用旧 playbook（但 CLI 已不暴露） |
| 角色 precheck 无对应文件 | 为 7 个无 prechecks 的 role 补空 `precheck.yml` |
| 回滚 | 改造前打 tag `v0.2-pre-siteyml`（commit 9ee4135），git revert 逐提交回滚 |

## 6. 验证

（见提交记录与真机回归：site.yml syntax-check、prechecks 动作执行、tag 隔离验证。）

## 7. 什么情况下推翻

- Ansible 提供动态 `include_playbook` 且能保证跨 play 状态一致 → 可去掉 import_playbook 拆分。
- 项目引入 kolla-ansible 式更完整的 action 框架 → 以新框架为准，本 ADR 标 Superseded。
