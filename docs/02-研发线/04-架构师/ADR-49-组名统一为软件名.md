# ADR-49: inventory 组名统一为软件名

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-48 扁平 inventory](./ADR-48-扁平inventory每进程一组.md) |
| 回滚锚点 | `v0.10-pre-group-rename` @ `c8f86ef` |

## 1. 问题

ADR-48 的扁平 inventory 里仍有概念名组：`[observability]`（看不出是 5 个监控软件）、
`[management]`、`[system-init]`、`[local-repo]`、`[agent]`、`[lvs-dr-director]` 等。
用户要求"用软件的名字命名，看得出部署什么"。

## 2. 决策：全部改为软件名，不留旧名

| 旧组名 | 新组名 | 依据 |
| --- | --- | --- |
| `management` | `etcd` | PG HA 节点都跑 etcd（主协调软件）|
| `system-init` | `os-init` | OS 初始化（动作名，非单软件）|
| `local-repo` | `repo-nginx` | 软件是 nginx（区别于 web nginx）|
| `agent` | `winter-agent` | 二进制 winter-agent（§4.9）|
| `observability` | `victoriametrics`（主组，代码引用）+ `vmalert` / `victorialogs` / `alertmanager` / `grafana`（各一组，同主机）| 5 个软件名，二进制见 all.yml |
| `lvs-dr-director` | `keepalived-director` | 软件 keepalived + 角色后缀（避免与 3 台 [keepalived] 合并导致 LVS 坏）|
| `lvs-dr-realserver` | `lvs-backend` | LVS 后端 |
| `pg_primary_candidate` / `pg_repo` | `pg-primary-candidate` / `pg-repo` | 连字符规范化 |

## 3. 关键实现

- 28 个代码文件的 `groups['旧名']` / `hosts: 旧名` 全量替换为新名（脚本精确匹配）。
- all.yml.example 的 `repo_base_url`（Jinja 字符串内嵌 `groups['local-repo']`）也同步改为
  `repo-nginx`——这是脚本第一遍漏掉的（字符串嵌套），真机 prechecks 报
  `dict object has no attribute 'local-repo'` 后修复。
- 服务端不入库的 `all.yml` 需单独 SFTP 同步（bundle 不含它）。

## 4. 已知取舍

- **`etcd` 组语义**：physics 上是 PG HA 三节点（也跑 patroni/haproxy），用主协调软件 etcd 命名。
- **5 个监控软件组同主机**：`victoriametrics` 作为代码里的"监控节点集合"代表，其余 4 组并列列出相同 IP。
- **LVS Director 名**：`keepalived-director`（1台）与 `keepalived`（3台）分开，避免三台都成 Director。

## 5. 验证（2026-11-25）

- site.yml syntax-check 通过；组解析：etcd=3、victoriametrics=3、keepalived=3、
  keepalived-director=1、lvs-backend=2、repo-nginx=1、os-init=3、winter-agent=0（空组，符合现状）。
- `./deploy.sh prechecks -t os-init,nginx` RC=0，3 台各 ok=16 failed=0。
