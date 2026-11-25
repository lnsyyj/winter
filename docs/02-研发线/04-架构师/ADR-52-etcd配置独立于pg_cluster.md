# ADR-52: etcd 配置独立于 pg_cluster

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-50 配置项命名统一为软件名](./ADR-50-配置项命名统一为软件名.md)、[ADR-51 etcd 独立入口](./ADR-51-etcd独立入口与hostvars修复.md) |

## 1. 问题

用户指出 `deploy/docs/etcd.md` 的配置表里仍有 `pg_cluster.etcd.client_port` 等：
etcd 已经是独立 role（ADR-51），但端口、数据目录仍寄生在 PG 集群配置 `pg_cluster.etcd.*` 下，
二进制配置又叫 `etcd_role.*`。etcd 的配置分散在三处，违背 ADR-50"配置项前缀 = 所属软件名"：

| 旧位置 | 内容 |
| --- | --- |
| `etcd_role.tarball` / `.bin_dir` | 二进制包与安装目录 |
| `pg_cluster.etcd.client_port` / `.peer_port` / `.data_dir` | 端口与数据目录 |
| `pg_cluster.vip_iface_cidr` | etcd role 拿它当防火墙网段 |

## 2. 决策：所有 etcd 配置收敛到 `etcd.*`

按现有 role 配置模式（`postgresql` / `nginx` / `harbor` 都是 `<软件名>_defaults` + `<软件名>` 合并），
新建独立的 `etcd_defaults` / `etcd`：

```yaml
etcd_defaults:
  client_port: 2379
  peer_port: 2380
  data_dir: /var/lib/etcd
  bin_dir: /usr/local/bin
  tarball: etcd-v3.5.17-linux-amd64.tar.gz
  tarball_subdir: tarball
  firewall_cidr: "192.168.101.0/24"
etcd: "{{ etcd_defaults | combine(etcd_overrides | default({}, true), recursive=True) }}"
```

映射：

| 旧 | 新 |
| --- | --- |
| `etcd_role.tarball` / `.tarball_subdir` / `.bin_dir` | `etcd.tarball` / `.tarball_subdir` / `.bin_dir` |
| `pg_cluster.etcd.client_port` / `.peer_port` / `.data_dir` | `etcd.client_port` / `.peer_port` / `.data_dir` |
| etcd role 用的 `pg_cluster.vip_iface_cidr` | `etcd.firewall_cidr` |

删除 `etcd_role`、`etcd_nodes`（未使用）、`pg_cluster_defaults.etcd` 整块。

## 3. 关键实现

- 用 `-e '{"etcd_overrides":{...}}'` 覆盖，不污染 PG 配置。
- `pg_cluster.vip_iface_cidr` **保留**：仍被 Patroni / HAProxy / Nginx / winter-server 使用
  （pg_hba 与各自防火墙），只是 etcd role 不再引用它。
- 16 处引用一次性同步：`roles/etcd/{tasks/deploy.yml,templates/etcd.service.j2}`、
  `roles/patroni/templates/patroni.yml.j2`、`roles/postgresql/tasks/{scale-in,scale-out-member}.yml`。
- `deploy/docs/etcd.md` 配置表改成 `etcd.*`；`deploy/docs/README.md` 索引与 ADR-51 加后续变更说明。

## 4. 已知取舍

- **默认值重复**：`etcd.firewall_cidr` 与 `pg_cluster.vip_iface_cidr` 默认值相同（192.168.101.0/24），
  现在两者可独立调整；若现场要改网段，两处都要检查。这是解耦的代价。
- **历史 ADR 保留原样**：ADR-51 §3 仍写 `pg_cluster.vip_iface_cidr`，加脚注指向本文，不重写历史。

## 5. 验证（2026-11-25）

- YAML 解析通过；代码中无 `pg_cluster.etcd.*` / `etcd_role.*` 残留。
- 真机 `deploy -t etcd` 幂等重跑 `changed=0 failed=0`（配置从新 `etcd.*` 取值，行为与旧值一致）。
- `etcdctl member list` 三成员 started、`endpoint health` 三端点 healthy。
