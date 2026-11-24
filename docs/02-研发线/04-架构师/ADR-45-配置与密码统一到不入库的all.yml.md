# ADR-45: 配置与密码统一到不入库的 all.yml

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-24 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-17 部署框架](./ADR-17-部署框架.md) · [ADR-42 单入口部署架构](./ADR-42-单入口部署架构.md) · [ADR-44 编排逻辑归入软件 role](./ADR-44-编排逻辑归入软件role.md) |
| 回滚锚点 | `v0.5-pre-config-consolidation` |

## 1. 问题

ADR-17 建立的双文件模型（配置在 `group_vars/all.yml`、密码在 `passwords.yml`）在实践中暴露两点问题：

1. **配置分散**：部分 role 仍自带 `defaults/main.yml`（如 `postgresql_defaults`、`agent_defaults`、
   `nginx_defaults`），配置真相源不在一处，改配置要翻 role 目录。
2. **密码加载重复**：ADR-44 拆散编排 role 后，密码 `include_vars` 散落在 site.yml 的 32 处
   pre_tasks 和 15 个 role action 文件中（共 47 处），全是为了把 `vault_*` 注入每个 play 作用域。

用户提出：哪个进程需要密码就放哪个 role、每个 role 的配置都在 `all.yml` 汇总。经评估，
"密码拆进各 role"不成立（一个密码被多个 role 共用、且 role 文件会入库导致明文进 git），
改为"配置与密码都进 all.yml、all.yml 本身不入库"。

## 2. 决策

1. **`group_vars/all.yml` 成为唯一配置 + 密码文件**，且**不入库**（加入 `.gitignore`）。
   部署者在控制机 `cp group_vars/all.yml.example group_vars/all.yml` 后填入真实 `vault_*`。
2. **入库模板 `group_vars/all.yml.example`**：全部配置 + 密码占位符（`CHANGE_ME_*`），
   替代原 `passwords.yml.example`（已删除）。
3. **所有 role 的 `defaults/main.yml` 删除**（17 个），配置迁入 `all.yml`：
   - 带 `*_defaults` 的 6 个（agent/bootstrap/system-init/uninstall/nginx/postgresql）：
     `X_defaults` 块与 `X = X_defaults | combine(X_overrides, recursive=True)` 合并行
     都搬进 `all.yml`，保留 ADR-42 的操作变量注入能力。
   - 独立变量（etcd_role/harbor_compose_command/patroni/keepalived_* 等）：原样搬入。
   - `prechecks`/`named` 的 role defaults 是 all.yml 已有定义的冗余 fallback，直接删除。
4. **删除全部 47 处密码 `include_vars`**：site.yml 的 32 个 pre_tasks 块与 15 个 role action
   文件中的对应块。`group_vars/all.yml` 由 Ansible 自动加载，`vault_*` 天然在所有 play 可用。
5. **删除空 `pre_tasks:` 键**：site.yml 的 32 个 play 在去掉唯一 task 后不再保留空键。

## 3. 关键实现约束（真机验证）

- **lazy Jinja 跨变量求值**：`agent = combine(agent_defaults, agent_overrides)`、
  `patroni.app_db = '{{ postgresql.db_name }}'` 从 role defaults 搬到 group_vars/all.yml 后，
  仍在 play 运行时正确求值——在控制机用 debug playbook 实测：
  `patroni.app_db=winter`、`agent.enabled=true`、`nginx.server_name=winter.local`、
  `vault_pg_password` 读自 all.yml，全部正确（ansible-core 2.14.18）。
- **`ansible-inventory --host` 不展开 group_vars**：它只输出 inventory host vars；
  验证合并后的变量必须用 `ansible-playbook` 的 debug task，不能用 `--host | jq`。
- **测试 playbook 必须放在 deploy 目录**：放 /tmp 会导致 ansible.cfg 的 roles_path 与
  group_vars 相对路径失效，表现为 `'agent' is undefined`。

## 4. 放弃什么

- **role 失去自包含性**：单独拿一个 role 目录无法独立运行，必须先有 `group_vars/all.yml`。
  这是"配置单点汇总"的直接代价，参考框架同样把配置集中在入口处。
- **all.yml 无版本管理**：它不入库，配置变更无法从 git 历史追溯；入库的只有 example 模板。
  若日后需要配置版本管理，可改用 ansible-vault 加密后入库（当时放弃：需额外管理解密口令）。
- **ADR-17 的"secrets 独立文件 + 零明文"表述被本 ADR 部分推翻**：零明文仍成立（all.yml 不入库），
  但"独立文件"不再成立——密码与配置合并。旧 ADR 作为历史记录保留不改。

## 5. 验证

- 本地静态校验：120 个 YAML 解析通过、shell 语法通过。
- 控制机 `ansible-playbook --syntax-check -i inventory/multinode site.yml`：通过。
- 控制机 debug playbook 变量求值：agent/postgresql/patroni/nginx/ha/dns/etcd_role/vault_* 全部正确。
- **待补**：真机全量回归（deploy/component、app-ha、pg-ha、pg-migrate、pg-scale-out、destroy）
  尚未在本次变更上重跑；配置搬迁虽经变量求值验证，仍建议在真机环境完整回归后再上生产。
