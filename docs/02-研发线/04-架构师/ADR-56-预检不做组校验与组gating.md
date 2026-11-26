# ADR-56: 预检删除永真的组校验，所有检查对所有被检查主机生效

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-26 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-54 禁止 hosts: all](./ADR-54-禁止hosts-all全用显式组.md)、[ADR-55 允许超融合部署](./ADR-55-允许超融合部署.md) |

## 1. 问题

用户指出：ADR-55 改完后的 `Group validity` 断言（只保留 `group_names | length > 0`）
"有什么用？现在 inventory 只打开 `[system-checks]` 一组，只想跑检查，能检查出什么？"

核查确认两点：

1. **该断言是永真式，零信息量**：`System checks` play 的 `hosts: system-checks`——
   能执行到断言的主机必然已在 `[system-checks]` 组，`group_names | length > 0` 恒为真，
   永远不会失败。
2. **ADR-55 引入副作用**：把端口检查 gating 改成 `nginx`/`winter-server` 组、数据盘检查 gating
   改成 `etcd` 组。用户 inventory 只开 `[system-checks]` 时，这些组不存在 →
   端口检查和数据盘检查被静默跳过，预检形同虚设。

## 2. 决策

- **删除整个 `Group validity` 断言**。主机成员资格由 play 的 `hosts:` 表达（ADR-54）：
  未列入 `[system-checks]` 的机器根本不会被检查，无需在任务里重复校验。
- **移除 system-checks 内所有 `group_names` gating**，让每项检查对"进入本 play 的所有主机"生效：
  - `ports.yml`：端口占用检查（80/443）对所有主机执行。
  - `resources.yml`：数据盘检查对所有主机报告（默认 `strict_data_disks=false` 只 warn）。
  - 保留 `strict_resources` / `strict_ports` 严格度开关，行为不变。
- 超融合原则不变（ADR-55）：一台主机可属于任意多组，预检不因组名而跳过检查。

## 3. 关键实现

- `precheck.yml` 用注释说明为何无组校验（`hosts:` 已保证 + 超融合允许多组）。
- 数据盘检查措辞从"OSD 专属"改为"可选额外数据盘"，标注是遗留 Ceph 检查，避免误导。
- 保留 `prechecks.management_min_*` 等配置变量名（阈值名，非组名）。

## 4. 验证（2026-11-26）

用用户真实 inventory（**只有 `[system-checks]` 三台**）实跑 `deploy.sh prechecks -t prechecks`：

- `Group validity` 任务数 = 0（已删除）。
- 端口检查从"跳过"变为"执行"：查出 `WARN port 80/443 is in use`。
- 三台 `failed=0`，资源不足也如实报告（CPU 2 核 < 15、内存 3622MB < 24576MB，report-only 模式）。
- 实际执行的检查项 30 类：OS 基线、podman、资源/磁盘、时钟同步、内核参数、
  默认路由、DNS 解析、SELinux、端口占用、内部 repo/Harbor 可达性。

## 5. 结论

"只开 system-checks 跑预检"能查出的是**机器级健康状态**——OS/资源/时钟/网络/端口/仓库可达性，
而不是"组分配是否合理"（后者由 inventory 与 play 的 `hosts:` 在结构上保证，无需运行时断言）。
