# ADR-53: 所有 role 配置归位到软件字典

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-50 配置项命名统一为软件名](./ADR-50-配置项命名统一为软件名.md)、[ADR-52 etcd 配置独立](./ADR-52-etcd配置独立于pg_cluster.md) |

## 1. 问题

用户指出 ADR-52 只修了 etcd，要求"其他 role 也检查一遍"。
对 76 个顶层配置键逐个分析"被哪些 role 使用"后发现两类残留问题（与 etcd 原状同源）：

**问题 1 · 软件配置寄生在 `pg_cluster` 下**（PG HA 集群最初包揽了所有 HA 组件配置）：

| 配置 | 实际使用方 | 引用数 |
| --- | --- | --- |
| `pg_cluster.haproxy.listen_port` / `check_*` | haproxy role | 13+ |
| `pg_cluster.port` / `rest_port` / `data_dir` | patroni role（haproxy/reconfigure/verify 读）| 17+ |

**问题 2 · 扁平变量散落在软件字典外**（命名已带软件名前缀，但没进对应字典，
无法用 `<软件名>.<键>` 统一访问，也容易和字典内同名键冲突）：

| 旧扁平键 | 目标 |
| --- | --- |
| `haproxy_role.*`（rpm/rpm_subdir/config） | `haproxy.*` |
| `harbor_compose_command` / `harbor_staging_dir` | `harbor.compose_command` / `.staging_dir` |
| `keepalived_config_path` / `_check_script` / `_lvs_check_script` / `_service` | `keepalived.*` |
| `winter_server_run_user` / `_health_retries` / `_health_delay` | `winter_server.*` |
| `lvs_realserver_sysctl_path` / `_helper` / `_unit` / `_vip` | `lvs_realserver.*` |
| `victoriametrics_data_dir` / `_config_dir` / `_retention_days` | `victoriametrics.*` |

## 2. 决策：每个软件的配置只在自己的字典里

沿用 ADR-52 的 `<软件名>_defaults` + `<软件名>` 合并模式（除已有的 `patroni`/`harbor` 等直接字典）：

- `pg_cluster` 收敛为**纯集群策略/网络**：`scope`、`allow_single_node_bootstrap`、`vip_iface_cidr`。
- Patroni 的 `port` / `rest_port` / `data_dir` → `patroni.*`。
- HAProxy 的 `listen_port` / `check_interval` / `check_fall` / `check_rise` → `haproxy.*`（与 `haproxy_role.*` 合并）。
- 所有扁平变量并入对应软件字典，删除扁平定义。

`all.yml` 头部"配置归属索引"同步改为字典形式（`haproxy.*`、`patroni.*`、`keepalived.*` 等）。

## 3. 关键实现与踩坑

- **字典内自引用陷阱**：把 `victoriametrics_data_dir` 并进 `victoriametrics.data_dir` 后，
  字典内部 `components.*.args` 里原本引用扁变量的字符串变成了 `{{ victoriametrics.data_dir }}`
  自引用——Jinja 在字典尚未构造完成时解析它，导致 `victoriametrics.enabled` 求值时报
  `unhandled exception while templating`。修法：字典内部用字面值（`/var/lib/winter/monitoring` 等），
  只有 role/template 从外部引用 `victoriametrics.*`。全仓扫描确认无其他字典自引用。
- **配置丢失陷阱**：批量脚本把 `winter_server_run_user` / `harbor_compose_command` 等扁平键删掉后，
  插入字典的锚点字符串未匹配（文件里目标块结构和假设不同），导致这些键一度"既不在字典也不在扁平"。
  用 YAML 解析逐字典校验 expected keys 才发现。修法：Edit 工具精确插入 + 逐键校验。
- **group_vars 作用域**：验证用的临时 playbook 必须放在 `deploy/` 目录下；放 `/tmp/` 时
  Ansible 不会加载 `deploy/group_vars/all.yml`，报"变量未定义"是假象。

## 4. 已知取舍

- `pg_cluster.scope` 保留在集群层（Patroni scope 是集群标识，不是单机软件参数）。
- `pg_cluster.vip_iface_cidr` 保留（被 pg_hba、Patroni、HAProxy、Nginx、winter-server 共用）。
- 历史 ADR-45 / ADR-50 里的旧配置名保留原文，以本文为准。

## 5. 验证（2026-11-25）

- YAML 解析通过；逐字典校验 expected keys 无缺失；无字典内自引用。
- 全仓无 `pg_cluster.port/rest_port/data_dir/haproxy`、`haproxy_role`、`*_compose_command` 等旧名残留。
- 真机变量解析（deploy 目录下 playbook）：8 类配置全部取到正确值
  （haproxy=5434、patroni=5433/8008、keepalived=config_path、server=winter-server/20、
  harbor=docker-compose、lvs=helper/unit、vm=data_dir/30、etcd=2379/data_dir）。
- `ansible-playbook --syntax-check` 通过；etcd 幂等重跑 `changed=0 failed=0`；
  `prechecks -t os-init,repo-nginx` 三台 `failed=0`。
