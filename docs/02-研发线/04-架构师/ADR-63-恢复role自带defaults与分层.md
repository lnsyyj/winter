# ADR-63: 恢复 role 自带 defaults 与配置分层

- 状态：已确认
- 日期：2026-11-27
- 决策者：架构师 ④
- 取代：[ADR-45 配置与密码统一到不入库的 all.yml](./ADR-45-配置与密码统一到不入库的all.yml.md) 的"role 无 defaults"部分（ADR-45 其余部分仍有效）
- 回滚锚点：`v0.15-pre-role-defaults`

## 1. 背景

用户指出"每个 role 中也有配置吧？all.yml 是每个 role 中配置的汇总"。核查发现：

- ADR-45（2026-11-24）把 17 个 role 的 `defaults/main.yml` 全删了，配置集中在 `group_vars/all.yml`，理由是"配置真相源单点"。
- 代价（ADR-45 自己写明）：**role 失去自包含性**——单独拿一个 role 目录无法理解它有哪些配置。
- 用户现在要求恢复 role 自带配置。

## 2. 关键技术验证（决定方案）

恢复 role defaults 前实测两个 Ansible 行为：

1. **同一 play 内多 role，只跑一个 tag**：另一个 role 的 defaults 仍可见（role defaults 在 play 加载时注入，不受 tag 过滤）。
2. **role defaults 里的合并表达式** `x: "{{ x_defaults | combine(x_overrides | default({}, true)) }}"` 能正确引用 `group_vars/all.yml` 的 `x_overrides`。

但真机跑 site.yml 单 tag 时暴露了第三个约束：

3. **site.yml 每个 role 是独立 play**，`-t vmalert` 时其他 play 的 role defaults 不加载 → `victoriametrics_ports`、`repo_nginx` 等 undefined。这正是 ADR-45 集中配置的根本理由。

## 3. 决策

采用"role defaults 为真相源 + 生成聚合文件"的分层，兼顾两者：

### 3.1 分层

| 层 | 位置 | 内容 | 入库 |
|---|---|---|---|
| role 默认值 | `roles/<role>/defaults/main.yml`（22 个 role） | 该 role 的配置默认值 + 合并表达式 | 是 |
| 聚合视图 | `group_vars/all.yml` 的 `>>> generated role defaults` 标记块 | 由脚本从上述 defaults 生成，让所有 play 可见 | all.yml 不入库，但模板入库 |
| 站点覆盖 | `group_vars/all.yml` 标记块之前 | `site.*`、`prechecks.*`、`vault_*`、`*_overrides` | 不入库（模板入库） |

### 3.2 生成脚本

`deploy/scripts/build-role-defaults.py` 读取所有 `roles/*/defaults/main.yml`，写入 `group_vars/all.yml` 的标记块之间；只重写标记块，保留站点配置与密码。修改任一 role defaults 后必须重跑：

```bash
python3 scripts/build-role-defaults.py
```

### 3.3 为什么不用 group_vars/all/ 目录

试过 `group_vars/all/role_defaults.yml`（目录形式），但 Ansible 中 `group_vars/all.yml` 文件与 `group_vars/all/` 目录**不能共存**——目录优先，all.yml 里的 `site`/`vault_*` 全被忽略，导致 `site is undefined`。因此改为"单文件 + 标记块"方案。

### 3.4 内联 site 字面值

role defaults 中原本有 12 处引用 `site.domain`/`site.https_port`/`site.data_dir`。由于聚合块与站点配置在同一文件内、加载顺序按文本顺序，这些引用可能读到未定义值。统一改为字面值（winter.local/443//var/lib/winter），站点差异通过 `site_overrides` 或 role overrides 处理——与 ADR-60/62 的"字典内部用字面值"一致。

## 4. 配置分布

- 迁移前：`group_vars/all.yml` 73 个顶层键。
- 迁移后：`all.yml` 站点层 19 键（site/prechecks/vault/6 个 overrides）+ 聚合块 54 键 = 73 键，守恒。
- 22 个 role 有 `defaults/main.yml`；`system-checks` 的配置（prechecks）留在站点层，无 defaults。

## 5. 验证（2026-11-27 真机）

- YAML：各 role defaults 与 all.yml 均解析成功；73 键守恒，无丢失无多余。
- syntax-check 通过；prechecks 三台 `failed=0`。
- 单 tag 部署：`-t vmalert` / `-t grafana` / `-t etcd` / `-t postgresql` 全部 RC=0（vmalert 幂等 `changed=0`），证明跨 role 配置在单 tag 下可见。
- 服务：15 个监控服务 + etcd + postgresql-17 全 active。

## 6. 代价与约定

- **必须重跑生成脚本**：改了 role defaults 忘了重跑，部署仍用旧的聚合值。 mitigated by 脚本注释与本文档。
- **all.yml 变大**（853 行），但站点配置集中在前 60 行，聚合块有明确标记可折叠。
- **ADR-45 的配置单点汇总意图仍成立**：只是从"手写 all.yml"变成"脚本生成聚合块 + 手写站点层"。
