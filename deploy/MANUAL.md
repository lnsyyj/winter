# Winter 手动部署运行手册（从零开始）

> 适用版本：`ec3fbbc` 之后（ADR-49 软件名组名/tag）。
> 适用环境：3 节点 `192.168.101.189` / `.197` / `.198`，控制机为 `.189`。
> 前置状态：已全量卸载，三台机器只剩 OS 和 SSH；所有数据目录、RPM、二进制、配置均已删除。
>
> 命令全部在**服务端** `.189` 的 `/root/github/winter/deploy` 目录执行（本机 Windows 的 ansible 有 GBK locale 问题，不要在本地跑）。

---

## 0. 动手前必读（两个关键前提）

1. **必须先设真实密码。** `group_vars/all.yml` 是 gitignore 的，仓库里和当前服务端那一份都是 `CHANGE_ME_*` 占位符。占位符长度虽能通过 assert，但会导致 Server 连不上数据库。第 0.2 步一定要做。
2. **数据已全清，`pg-migrate` 没有源数据。** 系统 PG 5432 现在是空库。如果想验证迁移，先按第 5.1 步造源数据；只想验证部署流程可直接跳过第 5 章。

整个流程预计 ~15-25 分钟（含 RPM 拉取、Harbor 除外）。

---

## 0. 准备

### 0.1 确认代码与环境

```bash
cd /root/github/winter/deploy
git -C /root/github/winter rev-parse --short HEAD   # 期望 ec3fbbc 或更新
./deploy.sh --help | head -40                         # 能打印帮助即 ansible 正常
```

### 0.2 设置真实密码（必须）

编辑 `group_vars/all.yml`，把 `vault_*` 里的 `CHANGE_ME_*` 全部替换为真实值。至少这几项：

| 变量 | 要求 | 用在哪 |
| --- | --- | --- |
| `vault_pg_password` | ≥12 位，无 `@ / : ? #` 等特殊字符（否则 DATABASE_URL 解析失败） | PG 系统库 + HA 库 + Server |
| `vault_jwt_secret` | ≥32 字符 | Server token 签发 |
| `vault_admin_password` | 强密码 | 平台管理员 |
| `vault_grafana_admin_password` | 强密码 | Grafana |
| `vault_keepalived_auth_pass` | VRRP 密码，主备必须一致 | Keepalived |

> 提示：`harbor_*` / `vault_dns_tsig_secret` 只在启用 Harbor / DNS 时需要。
> 真实密码不要提交到 git（`all.yml` 已在 `.gitignore`）。

快速检查（不应再看到 CHANGE_ME）：

```bash
grep -c 'CHANGE_ME' group_vars/all.yml   # 期望 0（或仅剩不启用组件的占位）
```

### 0.3 校验离线产物

```bash
cd artifacts/repo && sha256sum -c SHA256SUMS.txt | grep -v ': OK' ; cd ../..
# 无输出 = 全部一致。若缺项，回到控制机重新 fetch-artifacts.sh 并同步。
```

产物应包含：PG17 RPM×4、haproxy RPM、etcd tarball、5 个监控 tarball、pip wheels、Server 二进制；Harbor tarball 为可选项（629MB，不入库）。

### 0.4 确认 inventory

```bash
for g in etcd os-init postgresql nginx winter-server repo-nginx keepalived \
         keepalived-director lvs-backend victoriametrics; do
  printf '%-22s ' "$g"; ansible -i inventory/hosts.ini "$g" --list-hosts 2>/dev/null | tail -n +2 | tr '\n' ' '; echo
done
```

期望：`etcd/postgresql/nginx/winter-server/keepalived/victoriametrics` 各 3 台；`repo-nginx/keepalived-director` 各 1 台（`.189`）；`lvs-backend` 2 台（`.197/.198`）。

### 0.5 连通性

```bash
ansible all -i inventory/hosts.ini -m ping
```

三台都 `pong` 再继续。

---

## 1. 系统检查与初始化

```bash
# 1.1 只读预检（资源阈值按当前机器；不通过也能看清楚缺什么）
./deploy.sh prechecks -t os-init,postgresql \
  -e '{"prechecks_strict_resources":false,"prechecks_strict_ports":false}'

# 1.2 系统初始化（基础工具 + chrony）
./deploy.sh deploy -t os-init

# 1.3 基础依赖与内核参数
./deploy.sh deploy -t bootstrap
```

**验证**：`ansible all -m shell -a 'systemctl is-active chronyd'` 三台 `active`。

---

## 2. 基础组件（按此顺序，repo 必须最先）

```bash
# 2.1 内部制品仓库（Nginx 8082）——后续所有 RPM/wheel/tarball 都从这里拉
./deploy.sh deploy -t repo-nginx
curl -s -o /dev/null -w 'repo=%{http_code}\n' http://192.168.101.189:8082/

# 2.2 系统 PostgreSQL 5432（装 RPM、initdb、建 winter 库和账号）
./deploy.sh deploy -t postgresql
ansible postgresql -m shell -a "sudo -u postgres psql -tAc \"select datname from pg_database where datname='winter'\""

# 2.3 Nginx 接入层（winter.conf、自签证书、8081 upstream）
./deploy.sh deploy -t nginx

# 2.4 监控全家桶（VictoriaMetrics / vmalert / VictoriaLogs / Alertmanager / Grafana）
./deploy.sh deploy -t victoriametrics

# 2.5 管理 Server（Go 二进制 + systemd，连 5432）
./deploy.sh deploy -t winter-server
```

**验证**：

```bash
# 各节点服务状态
for ip in 189 197 198; do echo -n "node$ip: "; ssh root@192.168.101.$ip \
  'for s in nginx winter-server winter-victoriametrics winter-vmalert winter-victorialogs winter-alertmanager winter-grafana postgresql-17; do printf "%s=%s " $s $(systemctl is-active $s 2>/dev/null); done; echo'; done

# Server 健康
ssh root@192.168.101.189 'curl -s http://127.0.0.1:8081/healthz'

# Nginx 入口
curl -sk -o /dev/null -w 'https443=%{http_code}\n' https://192.168.101.189/
```

**如果 `winter-server` 起不来**：先看 `journalctl -u winter-server -n 50 --no-pager`，绝大多数是 `DATABASE_URL` 密码错误——回到 0.2 检查 `vault_pg_password`。

此时已完成单节点基础部署。想停在这里也行；下面继续做 HA。

---

## 3. PostgreSQL HA（3 节点 etcd + Patroni + HAProxy）

> inventory 里 `[etcd]` 已是 3 台，直接 bootstrap，不需要 `allow_single_node_bootstrap`。

```bash
./deploy.sh deploy -t postgresql-ha
```

**验证**：

```bash
ssh root@192.168.101.189 'patronictl -c /etc/patroni/patroni.yml list'
# 期望 1 Leader + 2 Replica，State=running

for ip in 189 197 198; do echo -n "node$ip: "; ssh root@192.168.101.$ip \
  'printf "etcd=%s patroni=%s haproxy=%s " $(systemctl is-active etcd) $(systemctl is-active patroni) $(systemctl is-active haproxy); echo'; done
```

端口说明：`5432` = 系统库；`5433` = Patroni HA 实例；`5434` = HAProxy 入口（应用应连这里）。

---

## 4. 数据迁移（5432 系统库 → 5434 HA 入口）

> **停机操作**：会停 `winter-server`，迁完再起。生产环境先公告维护窗口。

### 4.1 准备源数据（数据已清空，必须做）

```bash
# 在系统 PG 5432 建几张测试表，模拟业务数据
ssh root@192.168.101.189 "sudo -u postgres psql -c \"
  CREATE TABLE IF NOT EXISTS users(id serial primary key, name text, created_at timestamptz default now());
  INSERT INTO users(name) SELECT 'user-'||g FROM generate_series(1,100) g;
  GRANT SELECT ON ALL TABLES IN SCHEMA public TO winter;\""
```

### 4.2 执行迁移

```bash
./deploy.sh deploy -t postgresql-migrate
```

**验证**：

```bash
ssh root@192.168.101.189 'PGPASSWORD=$(grep -oP "(?<=://winter:)[^@]+" /etc/winter/server.env | head -1 | python3 -c "import sys,urllib.parse;print(urllib.parse.unquote(sys.stdin.read().strip()))") psql -h 127.0.0.1 -p 5434 -U winter -d winter -tAc "select count(*) from users"'
# 期望 100
cat /etc/winter/pg-migrated   # marker 存在 = 迁移已完成（幂等）
```

---

## 5. 应用层 HA（Server 多副本 + Nginx upstream + Keepalived VIP）

```bash
./deploy.sh deploy -t application-ha \
  -e '{"ha_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34"},
       "winter_server_ha_overrides":{"database_host":"192.168.101.189"}}'
```

> `interface` 按实际网卡名调整；`database_host` 填 HAProxy 入口所在节点。
> 此步会把 Server 的连接从 5432 切到 5434。

**验证**：

```bash
ssh root@192.168.101.197 'curl -sk -o /dev/null -w "VIP:443=%{http_code}\n" --max-time 6 https://192.168.101.250/'
# 期望 200
```

---

## 6. LVS/DR 四层负载（可选，需至少 2 个 Nginx）

```bash
./deploy.sh deploy -t lvs-director,lvs-realserver \
  -e '{"ha_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34","lvs":{"enabled":true,"scheduler":"rr"}},
       "winter_server_ha_overrides":{"database_host":"192.168.101.189"}}'
```

**验证**：

```bash
ssh root@192.168.101.189 'cat /proc/net/ip_vs'            # 有 ipvs 规则
for h in 197 198; do echo -n "node$h lo-vip="; ssh root@192.168.101.$h 'ip addr show lo | grep -c 192.168.101.250'; done
curl -sk -o /dev/null -w 'VIP:443=%{http_code}\n' https://192.168.101.250/
```

---

## 7. DNS / Harbor（可选）

```bash
# BIND 9 内网 DNS（发布 VIP 的 A 记录）
./deploy.sh deploy -t named -e '{"named_overrides":{"enabled":true},"ha_overrides":{"vip":"192.168.101.250"}}'

# Harbor（需先 FETCH_HARBOR=1 下载 629MB 离线包；builtin 单机模式）
./deploy.sh deploy -t harbor -e '{"harbor_overrides":{"enabled":true,"mode":"builtin"}}'
```

---

## 8. 最终验收

```bash
./deploy.sh verify
```

手动巡检清单：

- [ ] 3 台 `winter-server` active，`/healthz` 返回 200
- [ ] Patroni 1 Leader + 2 Replica
- [ ] HAProxy 5434 可连，`users` 表数据一致
- [ ] Nginx 443 / VIP 443 均返回 200
- [ ] 监控 5 个服务 active，Grafana `/grafana/` 可访问
- [ ] `pg-migrated` marker 存在

---

## 9. 扩容 / 缩容速查

```bash
# PG 加节点（节点必须已在 [etcd] 组）
./deploy.sh deploy -t postgresql-scale-out \
  -e '{"pg_scale_overrides":{"nodes":["192.168.101.197","192.168.101.198"]}}'

# PG 减节点（只能减 Replica，成员数保持奇数且 >=3；默认保留数据）
./deploy.sh deploy -t postgresql-scale-in \
  -e '{"pg_scale_overrides":{"removed_nodes":["192.168.101.198"]}}'

# 监控加节点
./deploy.sh deploy -t victoriametrics-scale-out \
  -e '{"victoriametrics_scale_overrides":{"nodes":["192.168.101.197"]}}'
```

---

## 10. 故障排查

| 现象 | 先看哪里 |
| --- | --- |
| `winter-server` 起不来 | `journalctl -u winter-server -n 50`；90% 是密码或 DATABASE_URL |
| PG 装不上 | repo 8082 是否 200；`artifacts/repo/rpm/pgdg17` RPM 是否齐全 |
| Patroni 起不来 | `journalctl -u patroni -n 50`；etcd 是否健康、5433 是否被占 |
| etcd quorum 不足 | `etcdctl member list`；成员数必须奇数 |
| HAProxy 5434 连不上 | `systemctl is-active haproxy`；后端 Patroni `/primary` 健康 |
| VIP 不通 | `ip addr show` 看 VIP 在不在；`firewall-cmd --list-all` |
| 端口冲突 | `ss -lntp \| grep -E ':80\|:443\|:5432\|:5433\|:5434\|:8081\|:8082'` |

---

## 11. 回滚 / 重来

当前没有任何备份（按要求已删除）。如果跑坏了要重来，只能再次全量卸载：

```bash
# 在 .189 上执行此前用过的卸载脚本（仍在 /root/uninstall_winter_all.sh）
for ip in 189 197 198; do
  [ "$ip" = 189 ] && bash /root/uninstall_winter_all.sh || \
    ssh root@192.168.101.$ip 'bash /root/uninstall_winter_all.sh'
done
```

然后从本手册第 0 章重新开始。

---

## 附：命令速查（按顺序复制）

```bash
cd /root/github/winter/deploy
# 0 准备：设密码、校验产物、ping（见正文）
./deploy.sh prechecks -t os-init,postgresql -e '{"prechecks_strict_resources":false,"prechecks_strict_ports":false}'
./deploy.sh deploy -t os-init
./deploy.sh deploy -t bootstrap
./deploy.sh deploy -t repo-nginx
./deploy.sh deploy -t postgresql
./deploy.sh deploy -t nginx
./deploy.sh deploy -t victoriametrics
./deploy.sh deploy -t winter-server
./deploy.sh deploy -t postgresql-ha
./deploy.sh deploy -t postgresql-migrate         # 需先造源数据
./deploy.sh deploy -t application-ha -e '{"ha_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34"},"winter_server_ha_overrides":{"database_host":"192.168.101.189"}}'
./deploy.sh deploy -t lvs-director,lvs-realserver -e '{"ha_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34","lvs":{"enabled":true,"scheduler":"rr"}},"winter_server_ha_overrides":{"database_host":"192.168.101.189"}}'
./deploy.sh verify
```
