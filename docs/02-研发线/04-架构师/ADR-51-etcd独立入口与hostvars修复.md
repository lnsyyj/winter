# ADR-51: etcd 独立部署入口与扁平 inventory 的 hostvars 修复

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-48 扁平 inventory](./ADR-48-扁平inventory每进程一组.md)、[ADR-50 配置项命名统一](./ADR-50-配置项命名统一为软件名.md) |

## 1. 问题

用户要求"在 deploy/docs 放部署文档，先写一个手动部署 etcd 的文档"。
核查发现两个阻碍：

1. **etcd 没有独立入口**：虽然有 `roles/etcd`（完整实现）和 `[etcd]` 组，
   但 `site.yml` 没有 `tags: [etcd]` 的 play，只能通过 `postgresql-ha` 连带部署，
   无法"只部署 etcd"。
2. **扁平 inventory 引入的 hostvars bug**：ADR-48 把主机改成裸 IP 且删掉
   `[all:vars]` 后，`ansible_host` 只在**当前主机**的 connection vars 里有值，
   `hostvars[其他主机]['ansible_host']` 返回 UNDEF。etcd 模板的 `--initial-cluster`
   遍历 `[etcd]` 组取其他成员的 `ansible_host`，直接失败：
   `AnsibleUndefinedVariable: 'HostVarsVars object' has no attribute 'ansible_host'`。

## 2. 决策

### 2.1 etcd 成为独立组件

- `site.yml` 新增 play：`- name: etcd cluster (DCS for Patroni)`，`hosts: etcd`、
  `tags: [etcd]`、`roles: [etcd]`，位置在 repo-nginx 之后（依赖 tarball）、postgresql 之前。
- `deploy.sh` 的 COMPONENT_TAGS 和帮助文本加入 `etcd`。
- 命令：`./deploy.sh deploy -t etcd`。`postgresql-ha` 仍连带部署 etcd（重复跑幂等）。

### 2.2 跨主机地址统一用 inventory_hostname

裸 IP inventory 下 `inventory_hostname` 就是主机地址，且在**所有**主机的 hostvars 里
始终存在。把以下位置的 `hostvars[X]['ansible_host']` 改为 `hostvars[X]['inventory_hostname']`：

- `roles/etcd/templates/etcd.service.j2`（`--initial-cluster` 成员 peer 地址）
- `roles/etcd/tasks/deploy.yml`（quorum 检查的 endpoints）
- `roles/haproxy/templates/haproxy.cfg.j2`（backend server 地址）
- `roles/patroni/templates/patroni.yml.j2`（etcd 地址列表）
- `roles/postgresql/tasks/{scale-in,scale-out,scale-out-member}.yml`
- `roles/victoriametrics/tasks/{scale-in,scale-out}.yml`
- `roles/keepalived/tasks/director.yml`（`map('extract', hostvars, 'ansible_host')`）

**保留**当前主机自身的 `ansible_host` 引用（有值，无问题）。

### 2.3 部署文档独立目录

`deploy/docs/` 专门存放部署手册：
- `README.md` — 索引
- `manual.md` — 从零部署总手册（原 `deploy/MANUAL.md` 迁入）
- `etcd.md` — etcd 单独部署手册（本次新增，含实测输出）

## 3. 关键实现

- etcd 模板的 `--initial-cluster` 仍按 `[etcd]` 组生成，节点名 = IP；
  `etcd_joining` + `etcd_initial_cluster` 分支保留给 scale-out（learner → promote）。
- 防火墙放行 2379/2380 用 `pg_cluster.vip_iface_cidr` 网段。

## 4. 已知取舍

- **inventory_hostname 即 IP**：裸 IP inventory 下两者等价；若以后改回主机名 inventory，
  需在这些位置改回 `ansible_host` 或在 inventory 显式定义该变量。
- **etcd 重建需清数据**：`initial-cluster-state=new` 配已有数据目录时，若成员集合变化会冲突；
  扩容必须走 scale-out 流程（`existing` 状态），不能改 inventory 后直接重跑。

> **后续变更（ADR-52）**：etcd 的端口/目录/二进制配置已从 `pg_cluster.etcd.*` 迁到独立的
> `etcd.*` 块，防火墙网段也从 `pg_cluster.vip_iface_cidr` 改为 `etcd.firewall_cidr`。
> 上文 §3 的描述保留当时实现，以 ADR-52 为准。

## 5. 验证（2026-11-25 真机）

- `./deploy.sh deploy -t etcd`：3 台 `failed=0`，二进制从 repo 拉取成功，
  quorum 检查通过（`started` 成员数 = 3）。
- `etcdctl member list -w table`：3 个成员 `STATUS=started`、`IS LEARNER=false`，
  NAME/PEER/CLIENT 均为对应节点 IP。
- `etcdctl endpoint health`（三端点）：全部 `healthy`，提案提交 10~24ms。
- `ansible-playbook --syntax-check` 通过。
- 修复前复现：`Render unit` 任务报 `HostVarsVars has no attribute 'ansible_host'`；
  修复后同命令成功，证明 hostvars 修法有效。
