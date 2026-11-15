# prechecks · 部署前检查（只读）

对应安装部署手册阶段 C3、测试用例 TC-26。**本 role 不得修改系统**：不装包、不写 sysctl、不改防火墙。

## 检查项

| 文件 | 检查内容 | 失败会怎样 |
| --- | --- | --- |
| `tasks/main.yml` | 分组合法性（不能同时在 management 与 ceph_nodes） | 直接失败，指出该主机应属哪组 |
| `tasks/os.yml` | CentOS Stream 9、podman 存在 | 失败并提示基线已锁定（ADR-11） |
| `tasks/resources.yml` | 管理面 15C/24G/350G；被管节点数据盘 ≥3 | 失败并给出实际值与本项目估算来源 |
| `tasks/time.yml` | chronyd 运行、偏移 ≤1s | 失败；时间漂移会让 cephadm 直接失败 |
| `tasks/kernel.yml` | overcommit_memory / max_map_count | 默认只报告；`enforce_kernel: true` 时失败 |
| `tasks/network.yml` | 默认路由、域名解析、SELinux 状态 | 无路由即失败（出站链路不成立） |
| `tasks/ports.yml` | 管理节点 80/443 空闲 | 失败并给出查占用命令 |
| `tasks/artifacts.yml` | 内网源 / Harbor 可达（默认关闭） | 失败并提示先做阶段 C1 |

## 失败时看哪里

1. 先看 `fail_msg`——它应包含「期望值 / 实际值 / 怎么修」。如果只写了「检查失败」，说明这个任务需要被改写。
2. 单独重跑某一类，避免被其他失败淹没：
   ```bash
   ./deploy.sh prechecks -i inventory/multinode --tags prechecks.time
   ```
3. 多节点时用 `--limit` 缩小范围：
   ```bash
   ./deploy.sh prechecks -i inventory/multinode --limit node02
   ```
4. 仍然看不懂时，确认 inventory 分组是否写对——大量「变量未定义」其实是主机落错了组。

## 变量

全部在 `deploy/group_vars/all.yml` 的 `prechecks:` 下，不在本 role 里硬编码。role 的 `defaults/main.yml` 只是兜底值。

## 已知取舍

- **数据盘检查默认提示模式**：M0 的虚拟机常只有一块盘，强制校验会挡住首次跑通；正式交付请设 `strict_data_disks: true`。
- **内核参数默认只报告**：它们应由 `bootstrap-servers` 写入，而该阶段尚未实现。
- **制品可达性默认关闭**：C1 起源之前，检查源必然失败，属于误报。
