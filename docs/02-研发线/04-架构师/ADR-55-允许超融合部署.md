# ADR-55: 允许超融合部署，移除节点组互斥限制

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-26 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-49 组名统一为软件名](./ADR-49-组名统一为软件名.md)、[ADR-54 禁止 hosts: all](./ADR-54-禁止hosts-all全用显式组.md) |

## 1. 问题

用户指出 `roles/system-checks/tasks/precheck.yml` 的 Group validity 断言：

```yaml
- not ('management' in group_names and 'ceph_nodes' in group_names)
```

它禁止一台主机同时属于 `management` 和 `ceph_nodes`，直接**堵死超融合部署**
（单机同时承担管理面与计算/存储节点）。用户明确要求："允许超融合部署，不要设限"。

同时发现 ADR-49 改名后遗留的系统性 bug：`os.yml` / `ports.yml` / `resources.yml` 里共 11 处
`group_names` 判断仍用旧组名 `management` / `ceph_nodes`，而扁平 inventory 里这些组早已不存在——
这些检查（podman 存在性、端口占用、资源阈值、数据盘）**从来没真正执行过**（when 恒为 false）。

## 2. 决策：一台主机可属于任意多个软件组

### 2.1 移除互斥断言

Group validity 只保留唯一硬规则：主机必须至少属于一个组。允许多组并存，
fail_msg 明确提示"one host may join multiple groups for hyper-converged deployment"。

### 2.2 修复失效的 group_names 判断

| 文件 | 旧判断 | 新判断 | 理由 |
| --- | --- | --- | --- |
| `precheck.yml` | management + ceph_nodes 互斥 | 仅 `group_names > 0` | 允许超融合 |
| `os.yml` | podman 检查限两组 | 对所有主机执行 | 任何角色都可能需要 podman |
| `ports.yml` | 端口检查限 management | 限 `nginx` 或 `winter-server` 组 | 真正监听 80/443 的主机 |
| `resources.yml` | 资源阈值限 management | 对所有主机执行 | 超融合机器承载多角色，都要达标 |
| `resources.yml` | 数据盘检查限 ceph_nodes | 限 `etcd` 组 | 当前框架只有 etcd 持有集群数据 |

## 3. 关键实现

- 保留 `prechecks.management_min_*` / `prechecks.ceph_node_min_data_disks` 等**配置变量名**不变
  （它们是阈值命名，不是组名；ADR-50 前缀体系已确立，改变量名会破坏兼容）。
- `strict_resources` / `strict_ports` 开关继续有效——新判断只是让检查真正生效，不改变严格度策略。
- 超融合部署示例（一台机器加入多个组）：
  ```
  [system-checks] 192.168.101.189
  [etcd]          192.168.101.189
  [postgresql]    192.168.101.189
  [nginx]         192.168.101.189
  [winter-server] 192.168.101.189
  ```

## 4. 已知取舍

- 资源阈值现在对所有主机执行（此前因旧组名恒 false 而从未生效）。小机器若不满足阈值，
  需调 `prechecks.management_min_*` 或设 `prechecks_strict_resources=false`；
  这是"检查真正生效"的代价，不是新增限制。
- 数据盘检查挂到 `[etcd]` 沿用旧 Ceph OSD 语义（`strict_data_disks` 默认 false，不阻塞）。

## 5. 验证（2026-11-26）

- 本地语法检查通过。
- 真机预检（默认 inventory）：`prechecks -t os-init,repo-nginx` 三台 `failed=0`。
- **超融合专项验证**：临时 inventory 让 `.189` 同时属于 `system-checks`/`etcd`/`postgresql`/
  `nginx`/`winter-server`/`os-init`/`repo-nginx` 七组，`prechecks -t prechecks` 结果
  `Group validity` 任务 `ok`、`failed=0`——互斥限制已移除。
