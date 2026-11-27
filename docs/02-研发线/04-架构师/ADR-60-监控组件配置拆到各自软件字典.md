# ADR-60: 监控组件配置拆到各自软件字典

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-27 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-50 配置项命名统一为软件名](./ADR-50-配置项命名统一为软件名.md)、[ADR-53 配置归位到软件字典](./ADR-53-所有role配置归位到软件字典.md)、[ADR-59 三者同名](./ADR-59-tag-role-inventory三者同名.md) |

## 1. 问题

用户指出 ADR-59 把监控拆成 5 个独立 role 后，`group_vars/all.yml` 的配置没同步改名——
`vmalert` / `victorialogs` / `alertmanager` / `grafana` 四个 role 仍读
`victoriametrics.components.<name>` 和 `victoriametrics_*` 前缀配置。
这违反 ADR-50/53 的"配置前缀 = 所属软件名"，是三者同名（ADR-59）没贯彻到底的配置层缺口。

## 2. 决策：四个组件各有独立字典

在 `group_vars/all.yml` 新建：

```yaml
vmalert/victorialogs/alertmanager/grafana:
  <name>_defaults: {enabled, tarball, binary, port, listen_flag, health_path, args}
  <name>: "{{ <name>_defaults | combine(<name>_overrides | default({}, true), recursive=True) }}"
```

每个组件 role 只读自己的 `<name>.*` 字典。

**共享项保留在 `victoriametrics.*`**：`bin_dir` / `data_dir` / `config_dir` / `run_user` /
`listen_addr` / `retention_days` / `tarball_subdir` 是五个组件共用的运行时与路径，
继续放在 `victoriametrics.*`（监控栈的公共配置），不构成违规。

**兼容视图**：`victoriametrics.components` 改为 Jinja 表达式，从四个独立字典重建旧结构，
使 `verify/tasks/acceptance.yml`、scale 操作、`victoriametrics` role 中遍历所有组件的逻辑继续工作。

## 3. 关键实现与踩坑

- **字典自引用陷阱再现（ADR-53 已记录过）**：兼容视图里 `victoriametrics` 条目最初写
  `'enabled': victoriametrics.enabled` / `args: ...victoriametrics.data_dir...`——
  字典引用自身，Jinja 报 `maximum recursion depth`。修法：兼容视图里 victoriametrics 条目
  用字面值；四个组件的 `args` 里的共享路径也改字面值（`/etc/winter/monitoring/...` 等）。
- **example 补齐**：发现 `all.yml.example` 缺 `victoriametrics_health_retries/delay`
  （ADR-50 改名时遗留），用户照 example 复制会缺配置；已补齐，`all.yml` 与 example 键集合一致。
- 4 个新 role 的 deploy.yml / precheck.yml / 模板注释里的 `victoriametrics.components.<name>`
  全部改为 `<name>.*`。

## 4. 已知取舍

- 共享路径用字面值写在组件 args 里，意味着改 `victoriametrics.data_dir` 不会自动同步到
  组件 args——组件配置改由自己的 `<name>_overrides.args` 管理。这是切断自引用的代价。
- `victoriametrics.components` 兼容视图是过渡层，供遍历型逻辑使用；新代码应直接读
  `<name>.*`。未来可在 verify/scale 改造后移除该视图。

## 5. 验证（2026-11-27）

- YAML 解析：`all.yml` 与 `all.yml.example` 均为 74 键，键集合一致。
- 变量解析：`vmalert.port=8880`、`victorialogs.port=9428`、`alertmanager.port=9093`、
  `grafana.port=3000`、`grafana.install_dir=/opt/winter/grafana`；
  兼容视图 `components.vmalert.port=8880`、`components.grafana.port=3000` 一致。
- `ansible-playbook --syntax-check` 通过。
- 5 个监控 role 依次 `deploy -t <name>`：首次因 args 字面值变更各 `changed=2`（一次性收敛），
  二次重跑 `changed=0 failed=0`，服务 active。
