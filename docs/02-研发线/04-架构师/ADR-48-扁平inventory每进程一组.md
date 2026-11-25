# ADR-48: 扁平 inventory —— 每进程一组，组内直接写 IP

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-47 每拓扑独立命名组](./ADR-47-每拓扑独立命名组.md)（本 ADR 取代其组结构）|
| 回滚锚点 | `v0.9-pre-flat-groups` @ `8a8b948` |

## 1. 问题

ADR-47 把 inventory 做成"每拓扑 × 每软件一个组"（`pgha-etcd`、`lvs-dr-nginx` …），
251 行、约 40 个组，还靠通用父组 + `--limit` 交集让 site.yml 的 `hosts:` 工作。用户认为
过于复杂："就是每个进程一个 [] 就可以了，然后下面写 ip"。

## 2. 决策

**扁平化**：`inventory/hosts.ini` 里**一个进程一个组，组下面直接写 IP**。没有拓扑组、
没有父/子嵌套、没有 `--limit` 交集机制。

```ini
[postgresql]
192.168.101.189
192.168.101.197
192.168.101.198

[lvs-dr-director]
192.168.101.189

[lvs-dr-realserver]
192.168.101.197
192.168.101.198
```

组名与 `site.yml` 的 `hosts:` 一一对应：`management` / `system-init` / `postgresql` /
`nginx` / `winter-server` / `harbor` / `keepalived` / `named` / `observability` /
`local-repo` / `agent` / `lvs-dr-director` / `lvs-dr-realserver`。
组件选择用 `-t`，主机子集用 Ansible 原生 `--limit`（按 IP/组）。

## 3. 关键风险与缓解

- **节点名变成 IP**：`inventory_hostname` 现在是 IP，Patroni `name`、etcd `--name`、
  keepalived `router_id` 会从 `ctl`/`node-b` 变成 IP 字符串。现有 HA 集群成员名是
  `ctl`/`node-b`/`node-c`，**重跑 `pg-ha` 会用新名字、可能创建新集群导致数据不一致**。
  用户明确接受此风险。缓解：改 inventory 前已备份当前 HA 库到
  `/root/winter-backup-20261125/winter.dump`（sha256 `b18b65df...`）。
  仅改 inventory 文件不会触发重建；实际重建前必须再次备份并规划数据迁移。
- **group_vars 中 `pg_primary_candidate`/`pg_repo` 仍保留**，值为 IP。

## 4. 放弃什么

- ADR-47 的"一个拓扑一眼看清软件分布"——换来极简结构（~60 行、13 个组）。
- 别名主机名（`ctl`/`pg-a`）的可读性——日志/节点标识里将出现 IP。

## 5. 连接用户与 Python 解释器的归属

inventory 不再写 `[all:vars] ansible_user / ansible_python_interpreter`（原是历史遗留）：

- **连接用户**：跟控制机当前用户（本项目为 root）；如需普通用户 + sudo，在 `ansible.cfg`
  用 `remote_user` + `become`，或按 inventory 分组单独设——不在全局写死。
- **Python 解释器**：由 `ansible.cfg` 的 `interpreter_python = auto_silent` 自动发现，
  不硬编码 `/usr/bin/python3`（硬编码会失去自动发现的灵活性）。

实测：去掉 `[all:vars]` 后 `ansible -m ping` 仍返回 pong、Python 自动发现正常。

## 6. 验证（2026-11-25）

- inventory 解析 RC=0；`[management]`=3 台、`[lvs-dr-director]`=1 台。
- site.yml syntax-check 通过。
- deploy.sh 去掉默认 `--limit all-in-one`（扁平 inventory 无该组）。
- 去掉 `[all:vars]` 后 ping/Python 发现正常（见第 5 节）。
