# ADR-44: 编排逻辑归入软件 role（方案 X）

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-23 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-43 全量逻辑入 role](./ADR-43-全量逻辑入role.md) |
| 回滚锚点 | `v0.4-pre-role-split` @ `4b7097a` |

## 1. 问题

ADR-43 把全部逻辑迁入 role 后，仍存在一类"编排 role"：`postgresql-ha` 内部 include
postgresql/etcd/patroni/haproxy 四个软件 role，`application-ha` include 五个软件 role，
`observability-scale-*` / `destroy` 同理。这与参考框架 `yunhai-deploy-ops` 的
"一个软件一个 role"原则不一致——编排 role 名字像软件 role，内里却是跨软件调度器，
定位和查找都别扭。

## 2. 决策

**彻底拆散编排 role，逻辑归入各软件 role**（方案 X）：

1. 19 个编排 slice 原样迁入对应软件 role 的 action 文件：
   - `postgresql-ha` → `postgresql/tasks/ha.yml`
   - `postgresql-scale-out` → `postgresql/tasks/scale-out.yml` + `scale-out-member.yml`
   - `postgresql-scale-in` → `postgresql/tasks/scale-in.yml`
   - `postgresql-migrate` → `postgresql/tasks/migrate.yml`
   - `application-ha/{server,harbor,nginx,keepalived}` → 各软件 role 的 `ha.yml`
   - `observability-scale-*` → `observability`/`nginx` 的 `scale-out.yml`/`scale-in.yml`
   - `destroy` → `uninstall`/`agent` 的 `destroy.yml`
2. **原样搬迁**：slice 内的 `set_fact → include_role` 链完整保留（如 keepalived 计算
   LVS backend 再 include 自己）。site.yml 用 `include_role tasks_from: <action>` 挂载，
   slice 内的 `include_role name: <软件>` 通过 main.yml dispatcher（`winter_ops_action=deploy`）
   重新进入该软件 role 的 `deploy.yml`——自引用不循环（已真机验证）。
3. `site.yml` 的 18 个编排 play 改名为 `Operate <动作> - <软件>`，role 引用改为软件 role +
   `tasks_from`。**tag 名保持不变**，`deploy.sh` 的 `-t <tag>` CLI 接口不变。
4. 删除 9 个编排 role：`postgresql-ha`、`postgresql-scale-out/in`、`postgresql-migrate`、
   `application-ha`、`keepalived-operations`、`observability-scale-out/in`、`destroy`。
5. 横切 role（`verify`/`reconfigure`/`upgrade`/`prechecks`）保留——它们不编排软件，是独立动作。

## 3. 命名

- 软件 role 保持官方/二进制名（§4.9）：`postgresql`/`etcd`/`patroni`/`haproxy`/`nginx` 等，
  不加 `operate-` 前缀。
- "操作"语义体现在 site.yml 的 play 名（`Operate <动作> - <软件>`）和 role 内的 action
  文件名（`ha.yml`/`scale-out.yml`/`migrate.yml`），不再有 `operate-*` role 目录。

## 4. 放弃什么

- 没有 `operate-` 前缀的 role 目录——若团队规范要求 role 名必须区分软件/操作，本方案不满足
  （选方案 Y 才能满足，但那会保留一层空壳编排 role）。
- PG 扩容状态机（learner/promote）仍集中在 `postgresql/tasks/scale-out-member.yml`，
  没有按 etcd/patroni/haproxy 分成三份——它是逐节点原子事务，拆开会破坏 quorum 顺序。

## 5. 真机回归（2026-11-23，全绿）

- 非破坏：prechecks RC=0、组件 deploy RC=0、app-ha（4 slice → winter-server/harbor/nginx/
  keepalived 各 role 的 ha.yml）RC=0、verify RC=0；3 节点 health ok。
- 破坏性链路：单节点 deploy + 种子数据 → pg-ha 单节点引导（1 成员 etcd）RC=0 →
  pg-migrate（源 5 regions → HA 5 regions，marker 写入）RC=0 →
  pg-scale-out 1→3（3 成员、`IS LEARNER=false`、2 副本）RC=0 → app-ha RC=0。
- 顺带修复既有隐患：verify 的 Server 健康探针用 `127.0.0.1`，HA 模式 Server 绑内网 IP
  导致 connection refused，改用 `ansible_host`（与 ADR-43 修 pg-migrate 同类问题一致）。
