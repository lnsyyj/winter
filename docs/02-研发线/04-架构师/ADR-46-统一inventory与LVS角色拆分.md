# ADR-46: 统一 inventory 与 LVS 角色拆分

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-24 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-45 配置与密码统一到不入库的 all.yml](./ADR-45-配置与密码统一到不入库的all.yml.md) |
| 回滚锚点 | `v0.6-pre-inventory-merge` @ `29dffa2` |

## 1. 问题

`deploy/inventory/` 有 7 个文件（all-in-one / multinode / pgha + 4 个 operations/*）。用户认为文件过多、
职责重叠，要求合并为一个。核心障碍：LVS/DR 依赖 `[keepalived]`（Director）与 `[nginx]`（RealServer）
不重叠，而通用拓扑里两者都是 `management` 子组（重叠）——一个静态 inventory 无法同时表达两种分组。

## 2. 决策

1. **7 个 inventory 合并为 `inventory/hosts.ini`**。拓扑用组区分（`all-in-one` / `multinode` /
   `pgha` / `lvs-dr` / `scale-out-agent`），命令用 `--limit <拓扑>` 选择（ADR-46）。
2. **LVS/DR 的分工由 inventory 组名直接表达**：`[lvs-dr-director]` 与 `[lvs-dr-realserver]`
   两个组，组名即角色（一眼可看出谁是 Director / RealServer）。`[lvs-dr]` 是二者父组，
   `--limit lvs-dr` 仍可选整个拓扑。这样统一 inventory 里 `keepalived`/`nginx` 都指向
   `management`（可重叠）也不影响 LVS。
3. **LVS 拆成两个独立 role**：`lvs-director`（VRRP + ipvs `virtual_server`）、`lvs-realserver`
   （loopback VIP + ARP 抑制）。逻辑从 `keepalived` / `realserver` role **复制**而来（不是移动），
   旧 role 保持不动、仍可单独调用。
4. `site.yml` 的两个 director play 分别用 `hosts: lvs-dr-director` / `hosts: lvs-dr-realserver`，
   直接挂 `lvs-director` / `lvs-realserver` role（无需 when 过滤）。

## 3. 关键实现

- `lvs-director` 用 `set_fact` 从 `groups['lvs-dr-realserver']` 取 `lvs_realservers`、
  从 `groups['lvs-dr-director']` 取 VRRP peer 列表；强制 `ha.lvs.enabled=true`。
- `keepalived.conf.j2` 的 `lb_nodes` 从 `groups['keepalived']` 改为 `lvs_directors`，
  `node_index` 用 `ansible_host` 匹配。
- 删除非 LVS 模式的 `check_nginx.sh` 健康检查任务（lvs-director 恒为 LVS 模式）。

## 4. 放弃什么

- **inventory 的直观性下降**：从"一个文件一个拓扑"变成"一个文件多拓扑 + --limit"，命令变长。
- **组归属成为 LVS 分工的唯一来源**：Director/RealServer 变更需改 inventory 组里的主机，
  不能靠 `-e` 临时覆盖（换来的是分工一眼可读）。

## 5. 真机回归（2026-11-24，全绿）

- 统一 inventory 解析：`[lvs-dr-director]` = ctl、`[lvs-dr-realserver]` = node-b/node-c 正确。
- `./deploy.sh deploy -i inventory/hosts.ini --limit lvs-dr -t lvs-director,lvs-realserver`
  真跑 RC=0；ctl 执行 Director role、node-b/c 执行 RealServer role。
- VIP 从外部节点访问 `https://192.168.101.250` 返回 200；`/proc/net/ip_vs` 显示两个 RealServer
  均在轮转（rr），LVS/DR 能力未退化。
