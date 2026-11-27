# ADR-62: 剩余 role 变量归位到软件字典

- 状态：已确认
- 日期：2026-11-27
- 决策者：架构师 ④

## 1. 背景

用户要求"其他 role 的变量也都按照 ADR-61 的标准修改"。对 `all.yml` 全部 74 个顶层配置逐项分析引用方后，发现四类残留的命名/归属问题：

| 类别 | 问题 | 规模 |
|---|---|---|
| `ha.*` | VIP/VRRP/LVS 配置叫概念名 `ha`，实际属 keepalived role；且 `keepalived.*` 字典已存在（4 个路径字段），两者混用 | ~110 处引用 |
| `pg_cluster` / `pg_migrate` / `pg_scale` | 缩写 `pg_` 不是软件名 `postgresql` | ~123 处引用 |
| `victoriametrics_*` 散落项 | 7 项（ports/listen_addr/bind_addr/advertise_addr/health_*/scale/vmalert_notifiers）前缀合规但没进字典，且 `victoriametrics.listen_addr` 与之重复 | 7 项 |
| `repo_nginx_listen_port` | 扁平变量，没进 repo_nginx 字典 | 1 项 |

## 2. 决策

按 ADR-50/53/61 的"配置归位"原则，分四类处理：

### 2.1 `ha.*` → `keepalived.*`（合并字典）
- `ha_defaults` 的 18 个字段（VIP/VRRP/LVS）合并进 `keepalived_defaults`，与原有 4 个路径字段同处一个字典
- 合并表达式改为 `keepalived: "{{ keepalived_defaults | combine(keepalived_overrides | default({}, true), recursive=True) }}"`
- `ha_overrides` → `keepalived_overrides`（CLI `-e` 传参名同步）
- 全仓 `ha.` → `keepalived.`（排除 `winter_server_ha.*` 与历史文件名 `ha.yml`）
- `lvs_realserver.vip` 的 `{{ ha.vip }}` → `{{ keepalived.vip }}`

### 2.2 `pg_*` → `postgresql_*`（软件名）
保持独立合并字典与 overrides 机制（ADR-42 要求），仅改前缀：
- `pg_cluster*` → `postgresql_cluster*`
- `pg_migrate*` → `postgresql_migrate*`
- `pg_scale*` → `postgresql_scale*`

### 2.3 `victoriametrics_*` 散落项 → `victoriametrics_common` 字典
- 新建 `victoriametrics_common` 存放 7 项共享配置（被 5 个监控 role 共用）
- 原散落变量名保留为**兼容别名**（`victoriametrics_ports: "{{ victoriametrics_common.ports }}"` 等），5 个 role 的引用不用大改

### 2.4 `repo_nginx_listen_port` → 字典内
- `repo_nginx.listen_port` 改为字面值 8082（权威源）
- `repo_nginx.base_url` 里的端口也用字面值 8082
- reconfigure 的 `repo_port` 改读 `repo_nginx.listen_port`

## 3. 取舍与踩坑

**ADR-60 自引用陷阱再现两次**：
1. `victoriametrics_common` 字典内部的 `bind_addr`/`advertise_addr`/`vmalert_notifiers` 原本引用 `victoriametrics_common.listen_addr`，字典引用自身 → Jinja `maximum recursion`。改为字面值 127.0.0.1。
2. `repo_nginx.base_url` 引用 `repo_nginx.listen_port`，同样自引用 → 改用字面值 8082。

这与 ADR-60 的 `victoriametrics.components` 自引用是同一类问题：**字典尚未构造完成时不能引用自身**。规避方式统一为"字典内部用字面值，外部 role 读字典"。

**兼容别名模式**：`victoriametrics_*` 散落项没有直接删除，而是改成从 `victoriametrics_common` 取值的别名——与 ADR-60 的 `components` 兼容视图一致，降低改动风险。未来可逐步让 5 个 role 直接读 `victoriametrics_common.*` 后删除别名。

**`pg_*` 不改结构**：迁移/扩容是带 overrides 的操作配置（ADR-42），保持独立合并字典比塞进 `postgresql` 主字典更安全，只改前缀即可满足"软件名命名"。

## 4. 验证（2026-11-27 真机）

- YAML 解析：`all.yml` / `example` 各 73 键，键集合一致；`ha` / `pg_cluster` / `pg_migrate` / `pg_scale` / `repo_nginx_listen_port` 已消失
- 配置解析：`keepalived.vip=""`、`keepalived.lvs.enabled=False`、`postgresql_cluster.scope=winter-pg`、`postgresql_migrate.source_port=5432`、`victoriametrics_common.ports.victoriametrics=8428`、`repo_nginx.base_url=http://192.168.101.189:8082`、`lvs_realserver.vip=""`（正确跟随 keepalived.vip）
- syntax-check 通过；prechecks 三台 `failed=0`
- victoriametrics/vmalert/grafana 幂等重跑 `changed=0 failed=0`；15 个监控服务全 active
- 全仓残留扫描：`ha.*` / `pg_*` / `repo_nginx_listen_port` / `platform.*`（除文件名）归零
