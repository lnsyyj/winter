# ADR-43: 全量逻辑入 role，site.yml 纯薄编排

- 状态：已确认
- 日期：2026-11-23
- 决策者：架构师 ④
- 前置：[ADR-42 单入口部署架构](./ADR-42-单入口部署架构.md)

## 背景

ADR-42 建立了单一 `site.yml` 入口 + `winter_ops_action` 分发，但留下了不一致：组件部署逻辑在
role 里，而扩容/卸载/数据迁移/lifecycle 等编排仍在 25 个独立 playbook 中，`site.yml` 用
`import_playbook` 收编。用户参照 `yunhai-deploy-ops/tools-playbooks/site.yml` 指出：其 `site.yml`
里每个 play 只有 `hosts + roles`，所有逻辑（含扩容）都在 `roles/<name>/tasks/{main,deploy,precheck}.yml`，
结构更统一、更易定位。本 ADR 决定对齐该范式。

## 决策

1. **所有 task 逻辑迁入 role**。`deploy/playbooks/` 下只保留 `site.yml` 一个文件；其余 25 个
   playbook 全部删除。每个操作对应一个 role：
   - 单主机组事务：`postgresql-ha`、`postgresql-scale-out`、`postgresql-scale-in`、`postgresql-migrate`
   - 多主机组操作：`application-ha`（4 片）、`destroy`（2 片）、`keepalived-operations`（2 片）、
     `observability-scale-out`（2 片）、`observability-scale-in`（4 片）
   - lifecycle：`verify`、`reconfigure`、`upgrade`
2. **site.yml 只做薄编排**：每个 play 仅含 play 级关键字（`hosts`/`roles`/`tags`/`vars_files`/
   `serial`/`when`），零 `tasks:` 业务逻辑。多主机组操作用 `include_role tasks_from: <slice>`
   把同一 role 的不同 task 文件挂到不同 `hosts` 的 play。
3. **role 内 `main.yml` 统一分发**：`include_tasks: "{{ winter_ops_action | default('deploy') }}.yml"`。
4. **play 级关键字保留在 site.yml**：`vars_files`（如 postgresql-ha/postgresql-scale-out 需暴露 PG 角色 defaults
   给 lazy Jinja 求值）、`serial: 1`（postgresql-scale-in/postgresql-migrate）、`when`（组件 enabled 门禁）。
5. **deploy.sh 所有 action 统一走 site.yml**，含 destroy/verify/reconfigure/upgrade；不再按
   action 分发到不同 playbook。

## 关键实现约束（真机踩坑）

- **静态 `roles:` 列表的 `tasks_from` 不可靠**：多分片 role 必须写成
  `tasks: [{ansible.builtin.include_role: {name, tasks_from}}]`，否则 Ansible 仍加载 role 的
  `main.yml` dispatcher。
- **action 名与旧 playbook 文件名冲突**：`include_tasks: "destroy.yml"` 在 role tasks 目录找不到时
  会回退到 `playbooks/destroy.yml`，把 playbook 当 task 文件解析（`conflicting action statements:
  hosts, gather_facts`）。删除旧 playbook + 给 `uninstall` role 加 `destroy.yml` shim 解决。
- **role defaults 在 `when` 求值时未加载**：`include_role X` 上的 `when: X.enabled` 不能用 X 自己的
  defaults，改用 `X_overrides.enabled | default(true)` 或无门禁。
- **lazy Jinja 跨 role 求值**：`patroni.app_db: '{{ postgresql.db_name }}'` 在 patroni role 的
  `when` 求值时需要 `postgresql` 已在 play 作用域，故 postgresql-ha play 的 `vars_files` 显式加载 4 个
  PG 角色 defaults。
- **`include_tasks` 相对路径**：`scale-out-member.yml` 随 postgresql-scale-out role 迁移，改用
  `{{ role_path }}/tasks/scale-out-member.yml`；observability-scale-in 的 remove 改用
  `include_role observability tasks_from: remove`。
- 机械迁移脚本保证 task 原样搬运（`pre_tasks`+`tasks`+`post_tasks` 顺次合并，`roles:` 转
  `include_role`），避免手抄破坏已验证的 etcd learner/promote、postgresql-migrate stop-the-world 等状态机。

## 结果

- `deploy/playbooks/` 从 26 个文件降为 1 个（site.yml），逻辑 100% 在 role。
- 真机全量回归通过：prechecks、组件 deploy×6、named、application-ha（role 分片 + LVS）、verify、
  destroy 真卸载、单节点 deploy、postgresql-ha（单节点引导）、postgresql-migrate（数据迁移+幂等）、
  postgresql-scale-out（1→3，3 个 voting 成员）、最终健康（3 节点服务 active、数据完整、LVS VIP 200）。
- 顺带修复既有隐患：postgresql-migrate 健康探针在 HA 模式 Server 绑内网 IP 时用 127.0.0.1 探测失败，
  改用 `ansible_host`。

## 回滚锚点

`v0.3-pre-role-migration`（`f41d28a`）。回滚后恢复 25 个 playbook + import_playbook 架构。

## 相关

- ADR-39：单节点 HA 引导与 1→3 在线扩容（etcd learner/promote）
- ADR-40：单节点 PG → HA 数据迁移
- ADR-42：单入口部署架构（本 ADR 的前置，import_playbook 方案被本 ADR 取代）
