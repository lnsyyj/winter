# ADR-39 · PostgreSQL 单节点 HA 引导态与 1→3 在线扩容

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-19 |
| 决策人 | 项目负责人 |
| 关闭 | 部署框架「从单节点在线扩容到多节点」路径 |
| 关联 | [ADR-10 MVP 单节点](ADR-10-最小可用范围.md) · [ADR-17 部署框架](ADR-17-部署框架.md) · [ADR-31 备份与恢复](ADR-31-备份与恢复.md) |

---

## 1. 背景

原框架把 PostgreSQL 分成两套**互不相通**的形态：

- **单节点**：`deploy` stage 用 `postgresql` role 装系统实例（`:5432`，`/var/lib/pgsql/17/data`），无 etcd / Patroni / HAProxy。
- **HA**：`postgresql-ha` stage 用 3 节点 etcd + Patroni（`:5433`，`/var/lib/pgsql/17/ha`）+ HAProxy（`:5434`）。

两者端口、数据目录、服务都不同，也没有迁移路径。结果「从 1 节点扩到 3 节点」实际是**重建 HA 集群**（覆盖单机形态），不是在线增量扩容；`postgresql-ha` 还硬性断言 `[management] >= 3`，单机根本进不去。

问题不在技术可行性——etcd 和 Patroni 都支持从单成员起步再逐台加入——而在于框架的建模和产品取舍：ADR-10 的 MVP 单机刻意不装 HA 组件以简化交付，但代价是升级到 HA 时没有桥。

## 2. 决策

**单节点 HA 是一个技术上合法、但默认关闭的引导态**，用于支撑真正的 1→3 在线扩容：

1. `postgresql-ha` 允许 **1 成员 etcd + 单节点 Patroni + HAProxy** 的引导态；由 `pg_cluster.allow_single_node_bootstrap`（默认 `false`）显式 opt-in。生产部署保持 `>=3 且奇数` 的硬门槛，避免误把无容错的集群当生产用。**单节点 HA 无容错，仅为引导态，不得用于生产。**
2. 扩容走 `postgresql-scale-out`，**逐节点、按 etcd 的正确语义**加入：
   - 现有成员 `>=3`：逐台 `member add`（投票成员）→ 启动 → 等待 started。
   - 现有成员 `==1`：必须使用 **learner**（etcd v3.4+，本项目 v3.5.17）。`member add --learner` → 启动 → `member promote`，一次一个。因为向单成员集群加投票成员会把 quorum 从 1 抬到 2，而此时只有 1 个已启动成员，cluster 会立刻失去 quorum 而卡死。
   - 现有成员 `==2`：拒绝（偶数不是受支持的稳态）。
3. 最终成员数 = `[management] | length`（新节点必须已在 `[management]` 里），必须为奇数。`postgresql-scale-out` 的旧断言 `(M + N) % 2` 重复计数了已在组内的新节点，可能放过偶数结果，一并修正。
4. 健康检查用 `member list` 统计 `started` 成员，不用 `endpoint health --cluster`：learner 是合法的已加入成员，但拒绝 health RPC，`--cluster` 在 learner 阶段永远无法收敛。

## 3. 为什么这样设计

| 备选 | 为什么放弃 |
| --- | --- |
| 保持两套实例，扩容=重建 | 不是在线扩容；切换时应用短暂停机，单机数据不进 HA 集群 |
| 单节点也强制 3 成员才能起 | 无法从 1 台真机起步验证扩容路径 |
| 1→3 用普通 `member add` | etcd quorum 从 1 抬到 2、仅 1 成员已启动 → cluster 卡死（真机复现：`re-configuration failed due to not enough started members`） |
| 一次性 `member add` 两个 learner | v3.4 只允许 1 个 learner；必须逐台加入并 promote |
| health check 用 `endpoint health --cluster` | learner 返回 `rpc not supported for learner`，健康检查永不通过 |

## 4. 关键约束

- `allow_single_node_bootstrap=true` 只在引导期使用；扩到 3 节点后应视为正式 HA，仍须保证奇数成员与 quorum。
- 逐节点加入的顺序不可颠倒：**先 add（learner）→ 再启动该节点 etcd/Patroni → 再 promote → 才能加下一个**。
- `postgresql-scale-out` 依赖一个「存量成员」作为 `etcdctl` 入口；不能把 `[management]` 全部设为新节点。
- 若上次扩容中途失败留下 learner，重跑前须 `etcdctl member remove` 清理并 wipe 该节点 `/var/lib/etcd`；playbook 已对「重复成员」直接报错而非静默重装。
- 单节点单机 PostgreSQL（5432 系统实例）与单节点 HA（5433 Patroni）仍是两套实例；**本 ADR 不提供二者之间的数据迁移**，选择哪种取决于首次部署方式。

## 5. 影响

| 对象 | 变化 |
| --- | --- |
| `group_vars/all.yml` | 新增 `pg_cluster.allow_single_node_bootstrap`（默认 false） |
| `postgresql-ha.yml` | 断言放宽为 `>=3 奇数` 或 `single-node + opt-in` |
| `postgresql-scale-out.yml` | 修正成员数双重计数；改为逐节点 include；拒绝 2 成员存量 |
| `scale-out-member.yml`（新增） | 单节点 learner 加入/promote、多节点 voting 加入、逐节点健康检查 |
| `roles/etcd/tasks/main.yml` | 健康检查改用 `member list` started 计数；成员列表查询改走存量节点并容错 |
| `roles/patroni`、`roles/haproxy` | 无需改动；通过 play 作用域暴露 role defaults 兼容 `include_role` |

## 6. 验证（真机 189/197/198，2026-11-19）

- Phase 1：单节点 opt-in `postgresql-ha` → 1 成员 etcd、Patroni primary、HAProxy 5434 可用，`PG_HA_RC=0`。
- Phase 2：真实 `postgresql-scale-out` 加 node-b、node-c → `SCALEOUT_RC=0`，3 成员全部 `started` 且 `IS LEARNER=false`，三端点全 healthy，`pg_stat_replication=2`。
- 过程中修复的实际问题：成员数双重计数、单成员加投票成员 quorum 死锁、`regex_replace` 反斜杠被渲染成字面 `\1`、learner 拒绝 health RPC、`include_role` 变量作用域。

## 7. 风险

| 风险 | 缓解 |
| --- | --- |
| 单节点 HA 被误当生产 | 默认 `allow_single_node_bootstrap=false`，opt-in 才可用；本 ADR 明确不得用于生产 |
| learner 未追平就 promote 失败 | promote 带回退重试（120s），追平后才成功 |
| 扩容中途失败留下孤立 learner | 重复成员直接报错；运维手册补充清理步骤（`member remove` + wipe data dir） |
| 从单机 PostgreSQL 切到 HA 无数据迁移 | 首次部署即决定形态；迁移另行设计 |

## 8. 什么情况下推翻

- etcd 增加「单成员直接安全扩展到 3」的原语，learner 流程可简化。
- 项目决定 MVP 默认就用单节点 HA，则取消 opt-in、把 `allow_single_node_bootstrap` 默认改为 true 并同步 ADR-10。
- 出现从单机 PostgreSQL 到 HA 的数据迁移需求，则补迁移 stage 并修订本 ADR 第 4 节。

推翻流程：新开 ADR → 本 ADR 标 `Superseded` → 同步架构设计、部署手册与 `docs/README.md` 索引。
