# deploy · 平台部署框架

按 [ADR-17](../docs/02-研发线/04-架构师/ADR-17-部署框架.md) 建立：结构对齐 kolla-ansible，代码独立，不 fork、不依赖其 Python 包。

## 当前状态

| 阶段 | 命令 | 状态 |
| --- | --- | --- |
| 部署前检查 | `./deploy.sh prechecks -i inventory/multinode` | ✅ **已实现**（只读，交付期用） |
| 收敛部署 | `./deploy.sh deploy -i inventory/multinode` | ✅ **已实现**（制品源 + PostgreSQL + Nginx） |
| PG 高可用 | `./deploy.sh pg-ha -i inventory/pgha` | ✅ **已实现**（etcd + Patroni + HAProxy，三节点） |
| bootstrap-servers | — | ⬜ 未实现 |
| pull / post-deploy / reconfigure / upgrade / destroy | — | ⬜ 未实现 |

未实现的阶段会明确报错退出（退出码 2），**不会假装成功**。

## PostgreSQL 高可用（etcd + Patroni + HAProxy）

```bash
./deploy.sh pg-ha -i inventory/pgha                  # 全量
./deploy.sh pg-ha -i inventory/pgha --tags etcd      # 只做 DCS
./deploy.sh pg-ha -i inventory/pgha --tags patroni   # 只做 PG 集群
./deploy.sh pg-ha -i inventory/pgha --tags haproxy   # 只做入口
```

| 组件 | 作用 | 端口（`group_vars.pg_cluster`） |
| --- | --- | --- |
| etcd ×3 | 存集群状态、Raft 选主（必须奇数） | 2379 / 2380 |
| Patroni ×3 | 管理 PG 进程、复制、切换、rewind | REST 8008 |
| PostgreSQL ×3 | 一主两从 | **5433** |
| HAProxy ×3 | 应用唯一入口，按 Patroni `/primary` 选主 | **5434** |

> 5433/5434 而非 5432：`.194` 上 5432 已被既有业务（argus）占用，端口一律在
> `group_vars` 的 `pg_cluster` 里改，role 内不硬编码。

### 实测结果（2026-11-15，三节点）

| 场景 | 结果 |
| --- | --- |
| bootstrap | pg-b 成为 Leader，另两台 `streaming`，Lag 0 |
| 计划切换 | `patronictl switchover` → pg-a 接任，TL+1，入口自动跟随 |
| **节点失联**（停 patroni + `kill -9 postgres`） | **10 秒内自动切换**到 pg-c，HAProxy 无缝指向新主 |
| 故障节点恢复 | 重启后自动 `rewind` 并以 streaming 副本回归，Lag 0 |
| 写入校验 | 经 5434 建表插入成功，副本同步 |

### 三条必须知道的坑（都是实机踩出来的）

1. **`listen_addresses` / `port` 不能写在 `bootstrap.dcs.postgresql.parameters`**：Patroni 会校验失败并忽略，
   表现是主库只监听 localhost、副本 `pg_basebackup` 挂住。监听地址与端口由 `postgresql.listen/port` 决定。
2. **`bootstrap.users` 与 `post_bootstrap` 脚本执行顺序不保证**：脚本里必须幂等建角色（含 `replicator`）与应用库。
3. **SELinux Enforcing 的机器上要两步**：`semanage port -a -t http_port_t -p tcp 5434`（能绑端口）+
   `setsebool -P haproxy_connect_any on`（能连后端 5433/8008）。缺任一个的表现分别是
   `cannot bind socket` 与 `server closed the connection unexpectedly`。

### 常用运维命令

```bash
export PATH=/usr/pgsql-17/bin:$PATH
patronictl -c /etc/patroni/patroni.yml list            # 看拓扑
patronictl -c /etc/patroni/patroni.yml switchover      # 计划内切换
patronictl -c /etc/patroni/patroni.yml failover        # 强制切主
etcdctl --endpoints=http://127.0.0.1:2379 endpoint health --cluster
```

## 离线制品源（ADR-15）

现场三种源都不可靠（实测：PGDG 官方站超时、EPEL 无 PG17/etcd、pip 的文件站超时），因此所有包**随代码入库**，由管理节点的 Nginx 发布，节点从内网拉取。

```
deploy/artifacts/repo/
├── rpm/pgdg17/   PG 17 的 4 个 RPM（server / libs / client；不含 contrib，见下）
├── rpm/base/     haproxy、perl-libs 等基础依赖
├── tarball/      etcd 官方静态包
└── pip/          patroni 及其依赖 wheel
```

| 环节 | 做法 |
| --- | --- |
| 发布 | `./deploy.sh deploy --tags repo`（`roles/local_repo`）：把 artifacts 复制到 `/var/lib/winter/www/repo`，Nginx 明文监听 **8082**，防火墙只放行内网网段 |
| 拉取 | 角色内用 `get_url` 取 `{{ platform.repo_base_url }}/...`，RPM 用 `dnf --disablerepo='*'` 本地安装，**不触发任何远端仓库元数据刷新** |
| 新增包 | 下载到对应子目录 → 提交 → 重跑 `--tags repo` → 目标机再跑对应角色 |

**已实测**（2026-11-15）：在 `.197`、`.198` 两台**无 PGDG 源的精简系统**上，PG 17 完全从内网源装成（`ok=23 failed=0`），应用账号 `winter` 连接正常。

**两个坑，记下来**：

1. `postgresql17-contrib` 依赖 `libperl` → `perl-libs` → `perl(Carp)/perl(Encode)…` 一整条 perl 链，精简系统上要多拉十几个包。**HA 用不到 contrib，故不纳入**；需要时再补。
2. `dnf download --resolve` 在**已经装过依赖的机器**上会跳过这些依赖（本次就漏了 `perl-libs`）。做法是最后一次 `rpm -qa` 比对，或用干净容器生成闭包。

## 实机验证记录（2026-11-15 · CentOS Stream 9）

在 M0 环境跑通 `./deploy.sh deploy`，`ok=25 changed=2 failed=0`；二次运行 `changed=0`（幂等）。

| 验证项 | 结果 |
| --- | --- |
| PostgreSQL | 复用已有 17.11 实例，新增 `winter` 库与角色；应用账号可连、错误口令被拒 |
| 既有库 | `appdb`/`postgres`/`template*` 未被改动 |
| Nginx 叠加 | `winter.local` 站点 200、`/healthz` 返回 ok；**同机既有站点（argus）80/443 仍为 200** |
| 密码来源 | `deploy/passwords.yml`（gitignore），未进入版本库 |

实机暴露并修复的五个问题（都是「纸面看不出、跑起来才炸」的）：

1. `deploy/passwords.yml` 不在 Ansible 自动加载路径 → 改为 pre_tasks 显式 `include_vars`；
2. `deploy.sh` 在 clone 后无执行位 → `git update-index --chmod=+x`；
3. PGDG 的 `pg_isready` 不在 postgres 用户 PATH → 改用 `psql -tAc "select 1"` 探活；
4. nginx 已安装仍触发 `dnf`，被不可达的 `pgdg-common` 源拖死 → 先 stat 再决定装；
5. **`group_vars/all.yml` 从未被加载**（不在 playbook/inventory 目录旁）→ 两个 playbook 加 `vars_files`；
   且 role 默认字典被 group_vars 整块替换 → 改为 `*_overrides` + `combine` 合并。

> **交付期 vs 运行期**（ADR-35）：本目录是**交付期**工具，用 ansible 装 Agent、注册证书、下发配置。
> 平台**运行期不持 SSH 凭据**，所有节点操作经 Agent；因此 `prechecks` 只用于装机前确认，
> 纳管与运维时的预检由 Agent 本地执行（Go 实现，检查项与本 role 保持一致）。

## 目录

```
deploy/
├── deploy.sh              # 阶段分发（对齐 kolla-ansible 的 CLI 形态）
├── ansible.cfg
├── inventory/
│   ├── all-in-one         # 单机（本机 localhost）
│   └── multinode          # 分组：management / ceph_nodes / monitor
├── group_vars/all.yml     # 唯一配置入口（对应 kolla 的 globals.yml）
├── playbooks/prechecks.yml
└── roles/prechecks/       # 只读检查，不修改系统
```

## 快速开始

```bash
# 1. 准备：控制机需有 ansible-core（版本见 docs BOM 约定，勿用发行版自带的过旧版本）
ansible-playbook --version

# 2. 按现场改 inventory 与 group_vars/all.yml
vi inventory/multinode
vi group_vars/all.yml

# 3. 跑检查
./deploy.sh prechecks -i inventory/multinode

# 4. 只跑某一类检查（排障时用）
./deploy.sh prechecks -i inventory/multinode --tags prechecks.time
```

可用的 tag：`prechecks.os` / `prechecks.resources` / `prechecks.time` / `prechecks.kernel` / `prechecks.network` / `prechecks.ports` / `prechecks.artifacts` / `prechecks.groups`。

## 设计约束（改动前必读）

1. **prechecks 必须只读**：所有 `command` 都要 `changed_when: false`；不得安装包、不得写 sysctl、不得改防火墙。检查本身要是改了系统，排障时就无法信任它。
2. **配置只在 `group_vars/all.yml`**：不要在 playbook 或 role 里硬编码 IP、端口、路径。
3. **不写 hostname 判断**：用 inventory 分组（`management` / `ceph_nodes`），不要 `when: inventory_hostname == ...`。
4. **失败信息必须可操作**：`fail_msg` 要包含「期望值 / 实际值 / 怎么修」，只写「检查失败」等于没写。
5. **敏感信息不进仓库**：密码、密钥只放 `passwords.yml`（后续阶段引入，`.gitignore` 已排除）。

## 与文档的关系

- 手册阶段 C 的命令与此处一致：`docs/07-新兴与稀缺线/20-技术文档工程师/安装部署手册.md`
- 检查项与验收口径：`docs/03-测试线/06-测试工程师/测试策略与验收用例.md`（TC-26 要求 `prechecks` 不改动系统、`deploy` 二次 `changed=0`）
- 前置条件来源：`docs/04-运维线/09-SRE-DevOps/部署与监控告警方案.md` §1 / §2
