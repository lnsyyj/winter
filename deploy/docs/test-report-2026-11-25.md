# 完整测试报告 · 2026-11-25

> 环境：3 节点 `192.168.101.189`/`.197`/`.198`，从"仅 etcd+repo-nginx 已部署"状态开始，
> 按 [总手册](./manual.md) 跑完全链路。所有命令在服务端 `.189` 执行。

## 1. 测试结论

**全部通过**。基础部署 8 个组件、HA 3 项、LVS/DR 全部 RC=0；过程中发现并修复 3 个真 bug、
补齐 4 处文档/示例缺失。最终状态：三台 12 个服务全 active，所有健康端点 200，
PG HA 1 Leader + 2 Replica，VIP 外部访问 200。

## 2. 测试结果明细

| 阶段 | 命令 | RC | 关键证据 |
| --- | --- | --- | --- |
| 产物校验 | `sha256sum -c SHA256SUMS.txt` | 0 | 32/32 OK |
| 语法检查 | `ansible-playbook --syntax-check site.yml` | 0 | 无 ERROR |
| 预检 | `prechecks -t os-init,repo-nginx,...` | 2* | nginx 父目录未创建（需先部署 repo-nginx），非 bug |
| os-init | `deploy -t os-init` | 0 | 三台 failed=0 |
| bootstrap | `deploy -t bootstrap` | 0 | 三台 failed=0 |
| repo-nginx | `deploy -t repo-nginx` | 0 | .189 failed=0，8082 可用 |
| etcd | `deploy -t etcd` | 0 | 3 成员 started，endpoint health 全 healthy |
| postgresql | `deploy -t postgresql` | 0 | 三台 initdb + winter 库/账号 |
| nginx | `deploy -t nginx` | 0 | 三台 443 监听 |
| victoriametrics | `deploy -t victoriametrics` | 0 | 5 组件 × 3 台全 active |
| winter-server | `deploy -t winter-server` | 0（修复后）| healthz=200（节点 IP:8081）|
| verify | `deploy.sh verify` | 0 | 三台 failed=0 |
| postgresql-ha | `deploy -t postgresql-ha` | 0（修复后）| 1 Leader + 2 Replica streaming，5434 连 primary |
| winter-server 重部署 | `deploy -t winter-server` | 0 | DATABASE_URL 切到 5434，healthz=200 |
| application-ha | `deploy -t application-ha` | 0 | keepalived 三台 active，VIP 可达 |
| lvs-director/realserver | `deploy -t lvs-director,lvs-realserver` | 0（修复后）| ipvs 2 RealServer，外部 VIP:443=200 |
| pg-migrate | `deploy -t postgresql-migrate` | 2* | 源 5432 已停、HA 已有数据 → 保护性拒绝（预期）|

\* 非组件失败，见"已知限制"。

## 3. 最终状态证据

```
三台 × 12 服务: nginx / winter-server / 5 监控 / etcd / patroni / haproxy / keepalived = active
  (postgresql-17 在 .197/.198 为 inactive — Patroni 接管后 standalone 被正确禁用)

端点:
  VIP:443 (外部)         = 200
  direct:443 (.189)      = 200
  server:8081 × 3        = 200
  vm/vlogs/alert/grafana × 3 = 200

Patroni:
  192.168.101.198  Leader   running
  192.168.101.189  Replica  streaming
  192.168.101.197  Replica  streaming
  5434 pg_is_in_recovery() = f   (primary)

etcd: 3 成员 started，endpoint health 全 healthy
```

## 4. 测试中发现并修复的 bug

### Bug 1 · Server 误判 DB 端点（严重）
`roles/winter-server/tasks/deploy.yml` 的 `server_use_haproxy` auto 判断含
`(groups['etcd'] | length) > 1`——3 节点 inventory 即使 HAProxy 未部署也判定走 5434，
导致单节点阶段 Server 因 5434 不存在而无限重启（healthz 失败）。
与同文件注释"node count is NOT a reliable signal"自相矛盾。
**修复**：只按 HAProxy 服务实际状态判断（`winter_server_haproxy_active.rc == 0`）。

### Bug 2 · patronictl 在 become 环境找不到（中等）
`Check cluster status` 任务报 `patronictl: command not found`，但手动执行成功。
根因：Ansible become 后 PATH 重置为 `/sbin:/bin:/usr/sbin:/usr/bin`，不含 pip3 安装目录
`/usr/local/bin`。**修复**：任务里用绝对路径 `/usr/local/bin/patronictl`。

### Bug 3 · LVS/DR 跨主机地址 + rp_filter（中等）
a) `hostvars[...]['ansible_host']` / `map('extract', hostvars, 'ansible_host')` 在扁平 inventory
下为 UNDEF（ADR-51 只修了部分，本次补 17 处 + lvs-director 模板）。
b) RealServer sysctl 只配了 ARP 抑制，缺 `rp_filter=0`——从 VIP 发出的响应包被反向路径
校验丢弃，连接卡在 SYN_RECV。**修复**：sysctl 模板加 `rp_filter=0`（all/default/lo）。

## 5. 文档/示例补齐

- `manual.md`：HA 章节补"HA 后必须重部署 Server"（否则 Patroni 停用 5432 后 Server 503）；
  LVS 章节补 `interface` 与 `database_endpoint` 参数、Director 本机不能 curl VIP 的陷阱。
- `deploy.sh` --help / `README.md` / `manual.md`：4 处 HA 示例补 `database_endpoint: haproxy`
  （application-ha 断言必需，实测漏传导致失败）。

## 6. 已知限制（非 bug）

- **预检 nginx 父目录**：全新机器上 nginx precheck 要求 `/var/lib/winter/www` 存在，
  该目录由 repo-nginx role 创建 → 先部署 repo-nginx 再跑 nginx precheck。
- **HA 后系统 PG 5432 停用**：`patroni.disable_standalone_pg=true` 的设计，`pg-migrate`
  （5432→5434）只适用于"先有系统库数据、再建 HA"的顺序；本测试 HA 已建好且数据已同步，
  migrate 的 precheck 正确拒绝覆盖。
- **Director 本机不能 curl VIP**：LVS/DR 下 Director 本机访问自己的 VIP 走 loopback，
  被 ipvs 拦截后 SYN_RECV 超时；必须从外部客户端验证（实测 200）。
- **Keepalived interface 默认 eth0**：本环境网卡是 ens34，必须显式传 `ha_overrides.interface`，
  否则 keepalived 启动失败（已在手册标注）。

## 7. 回滚

本测试环境当前为完整部署态。回到干净环境用此前脚本：

```bash
for ip in 189 197 198; do
  [ "$ip" = 189 ] && bash /root/uninstall_winter_all.sh || \
    ssh root@192.168.101.$ip 'bash /root/uninstall_winter_all.sh'
done
```
