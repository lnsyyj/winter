# ADR-50: 配置项命名统一为软件名

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-25 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-49 组名统一为软件名](./ADR-49-组名统一为软件名.md)、[ADR-45 配置与密码统一到 all.yml](./ADR-45-配置与密码统一到不入库的all.yml.md) |
| 回滚锚点 | `v0.11-pre-config-role-rename` @ `ec3fbbc` |

## 1. 问题

用户反馈：打开 `deploy/group_vars/all.yml` 看不出每个配置项属于哪个 role。
根因是 ADR-49 只统一了 inventory 组名 / tag / play 名，`all.yml` 的配置键和 role 目录名
仍留着概念名与缩写，四者不一致：

| 配置键 / role 目录 | 实际归属 | 问题 |
| --- | --- | --- |
| `monitoring_*`（10 项）、`obs_health_*`（2 项） | `observability` role / 5 个监控软件 | 一个叫 monitoring 一个叫 obs，role 目录叫 observability，组名又拆成 victoriametrics 等 5 个 |
| `server_run_user` / `server_health_*` / `server_ha*` | `winter-server` role | "server" 太泛 |
| `dns_defaults` / `dns` / `dns_overrides` | `named` role（BIND 9） | 协议名当配置名 |
| `local_repo` / `repo_listen_port` / role 目录 `local-repo` | repo nginx 站点 | 缩写 |
| `system_init*`（3 项）/ role 目录 `system-init` | os-init role | 动作名，组名已是 `os-init` |
| `agent*`（3 项）/ role 目录 `agent` | winter-agent role | 二进制名是 `winter-agent`（§4.9） |
| `realserver_*`（4 项）/ role 目录 `realserver` | LVS RealServer | 缺 `lvs-` 前缀，且与 `lvs-realserver` 目录重复 |

## 2. 决策：配置键 = 所属 role/操作的软件名前缀

用户从三个方案中选 **方案 B：软件名全链路统一**——组名、tag、role 目录、配置项四者一致。
沿用 ADR-49 已定的主名：`observability` → `victoriametrics`（代码里的监控节点代表组）。

### 2.1 role 目录改名

| 旧目录 | 新目录 |
| --- | --- |
| `roles/local-repo` | `roles/repo-nginx` |
| `roles/observability` | `roles/victoriametrics` |
| `roles/system-init` | `roles/os-init` |
| `roles/agent` | `roles/winter-agent` |
| `roles/realserver` | **删除**（死代码；功能已由 `roles/lvs-realserver` 取代，仅 `nginx/tasks/ha.yml` 还引用旧名，改指新 role）|

未改名的 role 本就是软件名或操作名：`postgresql` / `nginx` / `winter-server` / `harbor` /
`keepalived` / `named` / `etcd` / `patroni` / `haproxy` / `lvs-director` / `lvs-realserver` /
`bootstrap` / `system-checks` / `uninstall` / `verify` / `reconfigure` / `upgrade`。

### 2.2 配置键改名

| 旧前缀 | 新前缀 | 归属 role |
| --- | --- | --- |
| `monitoring_*` / `obs_*` / `observability` | `victoriametrics_*` / `victoriametrics` | victoriametrics |
| `server_run_user` / `server_health_*` / `server_ha*` | `winter_server_run_user` / `winter_server_health_*` / `winter_server_ha*` | winter-server |
| `dns_defaults` / `dns` / `dns_overrides` | `named_defaults` / `named` / `named_overrides` | named |
| `system_init_defaults` / `system_init` / `system_init_overrides` | `os_init_defaults` / `os_init` / `os_init_overrides` | os-init |
| `agent_defaults` / `agent` / `agent_overrides` | `winter_agent_defaults` / `winter_agent` / `winter_agent_overrides` | winter-agent |
| `local_repo` / `repo_listen_port` | `repo_nginx` / `repo_nginx_listen_port` | repo-nginx |
| `realserver_sysctl_path` / `_helper` / `_unit` / `_vip` | `lvs_realserver_*` | lvs-realserver |

role 内局部变量同步统一（如 `obs_components` → `victoriametrics_components`、
`system_init_chrony_unit` → `os_init_chrony_unit`、`monitoring_scale_targets` 动态组 →
`victoriametrics_scale_targets`）。

## 3. 关键实现

- `all.yml` / `all.yml.example` 头部新增**配置归属索引**：`配置项 → role（tags）`，
  打开文件第一屏即可查到归属，这是本次改动的核心交付。
- AGENTS.md §4.9 新增"配置项同名原则"：新增配置必须先按索引定位前缀；禁止概念名 / 缩写前缀。
- `*_defaults` / `*_overrides` / 合并结果的字典模式不变，只改名字，不改结构：
  `<name>: "{{ <name>_defaults | combine(<name>_overrides | default({}, true), recursive=True) }}"`。
- 所有 role task / template / site.yml / deploy.sh / MANUAL.md / README.md 的引用一次性全量替换；
  AGENTS.md 的历史变更记录与 docs/ 下的历史 ADR **不改**（保留当时的真实名称，供追溯）。

## 4. 已知取舍

- **`victoriametrics` 一词多义**：既是组名、role 目录名，也是配置项前缀和组件名；
  `victoriametrics` role 仍管理 5 个监控二进制（vmalert/victorialogs/alertmanager/grafana），
  与 ADR-49 的主组语义一致。
- **`winter_agent` vs `winter-agent`**：配置变量用下划线（Jinja identifier 不能含连字符，
  否则被解析为减法），role 目录 / tag 用连字符。
- **`realserver` 删除**：`nginx/tasks/ha.yml` 改为调用 `lvs-realserver`，LVS 行为不变
  （新 role 本就带强制 LVS 模式和 `[lvs-backend]` 组校验）。

## 5. 验证（2026-11-25）

- 服务端 `ansible-playbook --syntax-check -i inventory/hosts.ini site.yml` 通过
  （仅 `victoriametrics_scale_targets` 未匹配警告——动态组，scale-in 时才由 add_host 创建，符合预期）。
- 组解析：`os-init`/`victoriametrics` 各 3 台、`repo-nginx` 1 台、`winter-agent` 0 台（空组，符合现状）、
  `lvs-backend` 2 台。
- 真机 prechecks：`-t os-init` RC=0，三台各 `ok=8 failed=0`；
  `-t os-init,victoriametrics,postgresql` 中 victoriametrics 因机器已卸载、配置目录不存在而报
  actionable 失败（正是 precheck 职责），变量名已解析为 `victoriametrics.config_dir`。
- 全仓扫描：代码中无残留 `monitoring_*` / `obs_*` / `server_*` / `agent_*` 配置引用
  （仅 ADR 历史注释里保留 `roles/observability-scale-in/...` 溯源路径）。
