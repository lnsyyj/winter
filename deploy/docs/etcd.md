# 手动部署 etcd（DCS / Patroni 的协调服务）

> 适用：`ec3fbbc` 之后（ADR-50 软件名命名），独立 tag `etcd`（本次新增）。
> 实测环境：3 节点 `192.168.101.189` / `.197` / `.198`，2026-11-25 真机跑通。
> 命令在服务端 `.189` 的 `/root/github/winter/deploy` 目录执行。

etcd 是 Patroni 的分布式协调服务（DCS），保存 PG 集群的 leader 信息与配置。
它**先于 Patroni / HAProxy 部署**，但依赖内部制品仓库提供二进制 tarball。

---

## 0. 前置条件

| 前置 | 为什么 | 怎么确认 |
| --- | --- | --- |
| `repo-nginx` 已部署 | etcd 二进制从 `http://<repo>:8082/tarball/etcd-v3.5.17-linux-amd64.tar.gz` 拉取 | `curl -s -o /dev/null -w '%{http_code}' http://192.168.101.189:8082/` → `200` |
| `[etcd]` 组已定义 | 集群成员从该组生成 `--initial-cluster` | `ansible -i inventory/hosts.ini etcd --list-hosts` → 3 台 |
| firewalld 运行中 | role 会加 2379/2380 的 rich rule（失败不阻塞，但生产应放行） | `systemctl is-active firewalld` → `active` |
| 端口 2379/2380 空闲 | 与已有 etcd 冲突会导致新实例起不来 | `ss -lntp \| grep -E ':2379\|:2380'` → 空 |

**没部署 repo 的话先跑**：

```bash
./deploy.sh deploy -t repo-nginx
```

---

## 1. 配置检查（通常无需改）

etcd 的所有配置集中在 `group_vars/all.yml` 的 `etcd_defaults` / `etcd` 块中，
**不寄生在 `pg_cluster` 下**（ADR-52：etcd 是独立 role，端口/目录/二进制都围绕 etcd 自己配置）：

```bash
grep -A12 '^etcd_defaults:' group_vars/all.yml
```

| 配置项                | 默认值                            | 说明                        |
| --------------------- | --------------------------------- | --------------------------- |
| `etcd.client_port`    | `2379`                            | 客户端读写端口              |
| `etcd.peer_port`      | `2380`                            | 节点间 Raft 通信端口        |
| `etcd.data_dir`       | `/var/lib/etcd`                   | 数据目录（mode 0700）       |
| `etcd.bin_dir`        | `/usr/local/bin`                  | etcd / etcdctl 安装目录     |
| `etcd.tarball`        | `etcd-v3.5.17-linux-amd64.tar.gz` | 内部 repo 中的静态包        |
| `etcd.tarball_subdir` | `tarball`                         | repo 下的子目录             |
| `etcd.firewall_cidr`  | `192.168.101.0/24`                | 防火墙放行 2379/2380 的网段 |

覆盖示例（无需改文件）：

```bash
./deploy.sh deploy -t etcd -e '{"etcd_overrides":{"client_port":2379,"data_dir":"/data/etcd"}}'
```

> `pg_cluster.vip_iface_cidr` 仍存在，但只服务 PG / Patroni / HAProxy / Nginx；
> etcd role 已改用自己独立的 `etcd.firewall_cidr`。

> **注意**：节点名 = `inventory_hostname`。扁平 inventory 里主机是裸 IP，
> 所以 etcd 成员名就是 IP（见下方验证输出）。这与旧版按主机名命名不同，重建 cluster 时数据目录需清空。

---

## 2. 部署

```bash
cd /root/github/winter/deploy
./deploy.sh deploy -t etcd
```

`winter_ops_action=deploy`，`site.yml` 里的 `etcd cluster (DCS for Patroni)` play 生效：
`hosts: etcd` → `role: etcd`。

**role 内部按顺序做**（对应 `roles/etcd/tasks/deploy.yml`）：

1. 建 `/var/tmp/winter-etcd` 暂存目录
2. 从内部 repo 下载 tarball
3. 解压，复制 `etcd` / `etcdctl` 到 `/usr/local/bin`
4. 建数据目录 `/var/lib/etcd`（0700）
5. 渲染 `/etc/systemd/system/etcd.service` — `--initial-cluster` 由 `[etcd]` 组 3 个 IP 拼出，`--initial-cluster-state=new`
6. 启动并 enable etcd，重载 systemd
7. 防火墙放行 2379/2380（仅 `vip_iface_cidr` 网段）
8. 等待 quorum：轮询 `etcdctl member list` 直到 started 成员数 = `[etcd]` 组大小（120s 超时）
9. 打印成员表

**预期结果**：三台 `failed=0`，etcd role 的 `restart etcd` / `Wait for cluster health` 都执行成功。

---

## 3. 验证（实测输出）

### 3.1 systemd 服务

```bash
for ip in 189 197 198; do echo -n "node$ip: "; ssh root@192.168.101.$ip 'systemctl is-active etcd'; done
```

预期：三台都是 `active`。

### 3.2 成员列表

```bash
ssh root@192.168.101.189 '/usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 member list -w table'
```

实测输出：

```
+------------------+---------+-----------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |      NAME       |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+-----------------+-----------------------------+-----------------------------+------------+
|  c386e6668548334 | started | 192.168.101.197 | http://192.168.101.197:2380 | http://192.168.101.197:2379 |      false |
| 688d7f980d334207 | started | 192.168.101.189 | http://192.168.101.189:2380 | http://192.168.101.189:2379 |      false |
| 837ddcfb5e9b5c04 | started | 192.168.101.198 | http://192.168.101.198:2380 | http://192.168.101.198:2379 |      false |
+------------------+---------+-----------------+-----------------------------+-----------------------------+------------+
```

**验收点**：3 个成员 `STATUS=started`、`IS LEARNER=false`，NAME/PEER/CLIENT 都是各自的 IP。

### 3.3 端点健康

```bash
ssh root@192.168.101.189 '/usr/local/bin/etcdctl --endpoints=http://192.168.101.189:2379,http://192.168.101.197:2379,http://192.168.101.198:2379 endpoint health'
```

实测输出：

```
http://192.168.101.198:2379 is healthy: successfully committed proposal: took = 10.137593ms
http://192.168.101.189:2379 is healthy: successfully committed proposal: took = 10.819895ms
http://192.168.101.197:2379 is healthy: successfully committed proposal: took = 23.52769ms
```

**验收点**：3 个端点都 `healthy`，说明 Raft 已提交过提案（quorum 正常）。

### 3.4 读写冒烟（可选）

```bash
ssh root@192.168.101.189 '/usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 put /winter/test ok && /usr/local/bin/etcdctl --endpoints=http://127.0.0.1:2379 get /winter/test'
# 预期: ok / ok
```

---

## 4. 幂等性 / 重复执行

`deploy -t etcd` 可重复执行。但注意：

- 已有集群时，template 仍按 `initial-cluster-state=new` 渲染 → **handler 触发 restart 不会清数据**，
  etcd 以已有数据目录启动，成员关系保持不变（实测重复跑成员 ID 不变）。
- **不要手动改 `[etcd]` 组增删节点后直接重跑**——`initial-cluster` 变了但 `initial-cluster-state=new`
  与已有数据冲突。扩容请走 `postgresql-scale-out`（learner → promote 流程），
  它会设置 `etcd_joining=true` / `etcd_initial_cluster` 让模板用 `existing` 状态。

---

## 5. 故障排查

| 现象 | 原因 / 处理 |
| --- | --- |
| `Fetch etcd static package` 404 | repo-nginx 没部署，或 `artifacts/repo/tarball/` 里没有 etcd tarball |
| `Render unit` 报 `HostVarsVars has no attribute 'ansible_host'` | 已修（ADR-50 后续）：跨主机引用统一用 `inventory_hostname`；若仍出现，检查是否用了旧版 templates |
| `Wait for cluster health` 120s 超时 | 节点间 2380 不通（防火墙 / 路由）；或某节点数据目录残留旧成员信息 |
| 成员 `STATUS=unstarted` | `--initial-cluster` 与数据目录中的 member 不一致；清空 `/var/lib/etcd` 后重跑（**会丢数据**）|
| `endpoint health` 只有部分节点健康 | 少于半数节点存活，quorum 丧失；恢复至少 2/3 节点 |
| 端口被占 | `ss -lntp \| grep -E ':2379\|:2380'` 找到占用进程并处理 |

---

## 6. 完全重建（清空集群，会丢数据）

```bash
# 1) 停服并清数据（三台）
for ip in 189 197 198; do
  ssh root@192.168.101.$ip 'systemctl stop etcd 2>/dev/null; rm -rf /var/lib/etcd /etc/systemd/system/etcd.service /var/tmp/winter-etcd; systemctl daemon-reload'
done

# 2) 重新部署
./deploy.sh deploy -t etcd

# 3) 验证（同上第 3 节）
```

> 仅在确认 etcd 中无重要数据时执行。生产环境重建前先做快照：
> `etcdctl --endpoints=http://127.0.0.1:2379 snapshot save /root/etcd.snapshot`

---

## 7. 与后续步骤的衔接

etcd 就绪后，按顺序继续（见 [总手册](./manual.md)）：

```bash
./deploy.sh deploy -t postgresql        # 系统 PG 5432
./deploy.sh deploy -t postgresql-ha     # Patroni + HAProxy，会复用本集群
```

`postgresql-ha` 里的 etcd role 调用会检测到集群已健康，直接复用，不会重建。
