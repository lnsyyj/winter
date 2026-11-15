# deploy · 平台部署框架

按 [ADR-17](../docs/02-研发线/04-架构师/ADR-17-部署框架.md) 建立：结构对齐 kolla-ansible，代码独立，不 fork、不依赖其 Python 包。

## 当前状态

| 阶段 | 命令 | 状态 |
| --- | --- | --- |
| 部署前检查 | `./deploy.sh prechecks -i inventory/multinode` | ✅ **已实现**（只读，交付期用） |
| 收敛部署 | `./deploy.sh deploy -i inventory/multinode` | ✅ **已实现**（制品源 + PostgreSQL + Nginx + 可观测性栈 + Server） |
| 只收敛监控栈/Server | `./deploy.sh deploy --tags observability,server` | ✅ **已实现**（自动带上制品源） |
| PG 高可用 | `./deploy.sh pg-ha -i inventory/pgha` | ✅ **已实现**（etcd + Patroni + HAProxy，三节点） |
| bootstrap-servers | — | ⬜ 未实现 |
| pull / post-deploy / reconfigure / upgrade / destroy | — | ⬜ 未实现 |

未实现的阶段会明确报错退出（退出码 2），**不会假装成功**。

## 离线制品源（ADR-14 / ADR-15）

现场三种源都不可靠（实测：PGDG 官方站超时、EPEL 无 PG17/etcd、pip 的文件站超时），因此所有包**随代码入库**，由管理节点的 Nginx 发布，节点从内网拉取。

```
deploy/artifacts/repo/
├── rpm/pgdg17/     PG 17 的 4 个 RPM（不含 contrib，见下）
├── rpm/base/       haproxy、perl-libs 等基础依赖
├── tarball/        etcd + 可观测性栈 5 个静态包
├── bin/            winter-server（Go 单二进制，交叉编译产物）
├── pip/            patroni 及其依赖 wheel
└── SHA256SUMS.txt  搬运校验清单：cd deploy/artifacts/repo && sha256sum -c SHA256SUMS.txt
```

| 环节 | 做法 |
| --- | --- |
| 有网制包 | `sh deploy/scripts/fetch-artifacts.sh`（可复现；`GHPROXY=` 直连、`GHPROXY=https://ghproxy.net` 走镜像） |
| 搬运校验 | 制品随 git 搬运，现场用 `sha256sum -c` 逐项核对（ADR-14 第二段） |
| 发布 | `./deploy.sh deploy --tags repo`：复制到 `/var/lib/winter/www/repo`，Nginx 明文 **8082**，防火墙只放行内网网段 |
| 拉取 | 角色内 `get_url {{ platform.repo_base_url }}/...`；RPM 用 `dnf --disablerepo='*'` 本地安装，**不刷新任何远端仓库元数据** |

### 可观测性栈制品（版本固定，禁止 latest）

| 组件 | 版本 | 制品 |
| --- | --- | --- |
| VictoriaMetrics | v1.156.0 | `victoria-metrics-linux-amd64-v1.156.0.tar.gz` |
| vmalert | v1.156.0 | `vmalert-linux-amd64-v1.156.0.tar.gz` |
| VictoriaLogs | v1.54.0 | `victoria-logs-linux-amd64-v1.54.0.tar.gz` |
| Alertmanager | 0.35.0 | `alertmanager-0.35.0.linux-amd64.tar.gz` |
| Grafana | 9.5.20 | `grafana-9.5.20.linux-amd64.tar.gz` |
| Server | `17ba4fb` | `bin/winter-server` |

两个刻意的取舍：

- **vmalert 官方不单独发布**，只随 `vmutils`（约 140MB）提供。制包脚本提取 `vmalert-prod`
  后二次打包成约 14MB 的包，避免把全量工具包带进仓库。
- **Grafana 选 9.5.20（83.6MB）**，因为它是最后一个 tar.gz 小于 GitHub 单文件 100MB 上限的
  稳定版；11.x 起均 >170MB，升级时必须改走分片或搬运介质。

> 实测：本地经 ghproxy 约 18KB/s（不可用），实机直连 GitHub 约 1.7MB/s（15MB/9s）。
> 制包脚本默认走代理，在能直连的机器上用 `GHPROXY= sh deploy/scripts/fetch-artifacts.sh`。

**已实测**（2026-11-15）：在 `.197`、`.198` 两台**无 PGDG 源的精简系统**上，PG 17 完全从内网源装成（`ok=23 failed=0`），应用账号 `winter` 连接正常。

**两个坑，记下来**：

1. `postgresql17-contrib` 依赖 `libperl` → `perl-libs` → `perl(Carp)/perl(Encode)…` 一整条 perl 链，精简系统上要多拉十几个包。**HA 用不到 contrib，故不纳入**；需要时再补。
2. `dnf download --resolve` 在**已经装过依赖的机器**上会跳过这些依赖（本次就漏了 `perl-libs`）。做法是最后一次 `rpm -qa` 比对，或用干净容器生成闭包。

## 可观测性栈与 Server（ADR-18 / ADR-09 / ADR-33）

`./deploy.sh deploy` 一次收敛；也可只跑 `--tags observability,server`。
组件清单、端口、版本、命令行参数全在 `group_vars/all.yml`，role 不硬编码端口。

| 组件 | 端口（默认） | systemd | 健康端点 |
| --- | --- | --- | --- |
| VictoriaMetrics | 8428 | `winter-victoriametrics` | `/health` |
| vmalert | 8880 | `winter-vmalert` | `/health` |
| VictoriaLogs | 9428 | `winter-victorialogs` | `/health` |
| Alertmanager | 9093 | `winter-alertmanager` | `/-/healthy` |
| Grafana | 3000 | `winter-grafana` | `/api/health` |
| Server | 8081 | `winter-server` | `/healthz` |

- 全部只监听 `127.0.0.1`，以非 root 账号运行；Grafana 对外只经 `https://<domain>/grafana/`
  反代（ADR-09），开箱即带一个指向本机 VictoriaMetrics 的默认数据源。
- 告警链路唯一：`vmalert → Alertmanager → webhook`（ADR-18）；M0 用占位规则，
  Server 的 HMAC webhook 端点待实现，告警可丢不影响任务。
- 指标/日志保留 30 天；**审计在 PG，不进日志系统**（ADR-06 / ADR-32）。
- `observability.enabled: false` 或单个组件 `enabled: false` 可裁剪（ADR-18 打包分层）。
- Server 的 `DATABASE_URL` / `JWT_SECRET` / 管理员口令全部来自 `deploy/passwords.yml`
  （模板见 `passwords.yml.example`），role 显式 assert，不生成默认弱口令。
- 覆盖正在运行的二进制会触发 `ETXTBSY`，Server role 在目标目录 `cp` + 同目录 `mv`
  原子替换 inode，保留下载源文件以保证幂等；内容不变则不重启。

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

## 实机验证记录（2026-11-15 · CentOS Stream 9）

基础层：`./deploy.sh deploy` 跑通 `ok=25 changed=2 failed=0`；二次运行 `changed=0`（幂等）。

| 验证项 | 结果 |
| --- | --- |
| PostgreSQL | 复用已有 17.11 实例，新增 `winter` 库与角色；应用账号可连、错误口令被拒 |
| 既有库 | `appdb`/`postgres`/`template*` 未被改动 |
| Nginx 叠加 | `winter.local` 站点 200、`/healthz` 返回 ok；**同机既有站点（argus）80/443 仍为 200** |
| 密码来源 | `deploy/passwords.yml`（gitignore），未进入版本库 |

### 监控栈与 Server（同日新增）

| 验证项 | 结果 |
| --- | --- |
| 全量收敛 | `./deploy.sh deploy` → `ok=65 changed=22 failed=0` |
| 幂等（TC-26） | 修复后连续两次重跑均 `changed=0` |
| 6 个服务 | `winter-{victoriametrics,vmalert,victorialogs,alertmanager,grafana,server}` 全部 active running + 开机自启 |
| 监听面 | 8428/8880/9093/9428/3000/8081 全部只绑 `127.0.0.1`，只有制品源 8082 对外 |
| 本机健康端点 | 6 个端点全部 200 |
| 内网源 | 8082 上 Grafana 包与 Server 二进制 HEAD 200 |
| 反代 | `/grafana/api/health`、`/api/healthz` 均 200；**既有站点按 IP 访问仍 200** |
| 登录 | systemd 实例登录成功，`/v1/me` 返回 admin |

实机暴露并修复的三个问题：

1. `unarchive` 要求 `dest` 目录**已存在**：Grafana 只建了父目录 `/opt/winter` → 补建安装目录；
2. `local_repo` 把每次 `firewall-cmd --reload` 输出的 `success` 当成变更 →
   改用 `--query-rich-rule` 判定，只在新增加规则时报 changed；
3. `winter-server` 的 `mv` 消耗了下载源文件，导致每次重跑都重新 `get_url` →
   改为复制到目标目录 + 同目录 `mv` 原子替换，源文件保留。

### 基础层实机暴露并修复的五个问题（都是「纸面看不出、跑起来才炸」的）：

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
├── deploy.sh                  # 阶段分发（对齐 kolla-ansible 的 CLI 形态）
├── ansible.cfg
├── inventory/                 # all-in-one / multinode / pgha
├── group_vars/all.yml          # 唯一配置入口（对应 kolla 的 globals.yml）
├── passwords.yml.example      # 敏感值模板；实际 passwords.yml 不入库
├── scripts/fetch-artifacts.sh # 有网制包（可复现，版本固定）
├── artifacts/repo/            # 离线制品库（随代码入库）
├── playbooks/                 # prechecks / deploy / pg-ha
└── roles/
    ├── prechecks/             # 只读检查
    ├── local_repo/            # 发布内网制品源（8082）
    ├── postgresql/ nginx/     # 库与接入层
    ├── etcd/ patroni/ haproxy/# PG 高可用
    ├── observability/         # VM/vmalert/VictoriaLogs/Alertmanager/Grafana
    └── winter-server/         # Go 单二进制 + systemd
```

## 快速开始

```bash
# 1. 控制机需有 ansible-core（版本见 docs BOM 约定）
ansible-playbook --version

# 2. 按现场改 inventory 与 group_vars/all.yml，并准备 passwords.yml
cp passwords.yml.example passwords.yml && vi passwords.yml

# 3. 只读预检
./deploy.sh prechecks -i inventory/multinode

# 4. 全量收敛（数据库 + 接入层 + 监控栈 + Server）
./deploy.sh deploy -i inventory/multinode

# 5. 二次运行确认幂等（应 changed=0）
./deploy.sh deploy -i inventory/multinode
```

可用的 tag：`repo` / `postgresql` / `nginx` / `observability` / `server` / `etcd` / `patroni` / `haproxy`，
以及 `prechecks.os` / `prechecks.resources` / `prechecks.time` / `prechecks.kernel` / `prechecks.network` / `prechecks.ports` / `prechecks.artifacts` / `prechecks.groups`。

## 设计约束（改动前必读）

1. **prechecks 必须只读**：所有 `command` 都要 `changed_when: false`；不得安装包、不得写 sysctl、不得改防火墙。
2. **配置只在 `group_vars/all.yml`**：不要在 playbook 或 role 里硬编码 IP、端口、路径。
3. **不写 hostname 判断**：用 inventory 分组（`management` / `ceph_nodes`），不要 `when: inventory_hostname == ...`。
4. **失败信息必须可操作**：`fail_msg` 要包含「期望值 / 实际值 / 怎么修」。
5. **敏感信息不进仓库**：密码、密钥只放 `passwords.yml`（`.gitignore` 已排除）。
6. **目标机不编译**：Go 组件一律交叉编译成单二进制进 `artifacts/repo/bin/`（ADR-14）。

## 与文档的关系

- 手册阶段 C 的命令与此处一致：`docs/07-新兴与稀缺线/20-技术文档工程师/安装部署手册.md`
- 检查项与验收口径：`docs/03-测试线/06-测试工程师/测试策略与验收用例.md`（TC-26 要求 `prechecks` 不改动系统、`deploy` 二次 `changed=0`）
- 前置条件来源：`docs/04-运维线/09-SRE-DevOps/部署与监控告警方案.md` §1 / §2
