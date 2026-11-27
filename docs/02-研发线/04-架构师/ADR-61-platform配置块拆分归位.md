# ADR-61: `platform` 配置块拆分归位

- 状态：已确认
- 日期：2026-11-27
- 决策者：架构师 ④

## 1. 背景

用户指出 `deploy/group_vars/all.yml` 的 `platform:` 块没有对应的 role，问"为什么不按每个 role 命名"。

核查后发现 `platform` 是唯一没有对应 role 的配置块，且混了三类性质不同的配置：

| 子项 | 实际使用者 | 问题 |
|---|---|---|
| `repo_base_url` | 16 个 role 下载制品 | 提供者是 repo-nginx role，却挂在 platform 下 |
| `registry_host` / `registry_ca_path` | harbor、system-checks | 属 Harbor，且 `harbor.hostname` 本来就存在却反向引用 platform |
| `auth`（session/ldap 全套） | **零引用** | 死配置；Server 实际用 `winter_server.*` + `vault_*` |
| `retention` | 只有 reconfigure 校验 | 属 winter-server（审计保留策略） |
| `domain` / `https_port` / `data_dir` / `version` | nginx/grafana/keepalived/verify 等 12+ 处 | 真正的跨 role 共享配置 |

## 2. 决策

按 ADR-50/53/59 的"配置归位到所属软件"原则，把 `platform` 彻底拆掉：

1. **能归 role 的归位**：
   - `platform.repo_base_url` → `repo_nginx.base_url`（repo-nginx role 发布，全仓下载源）
   - `platform.registry_host` → `harbor.hostname`（Harbor 已有该字段，删除反向引用）
   - `platform.registry_ca_path` → `harbor.registry_ca_path`（新增）
   - `platform.retention.*` → `winter_server.retention.*`（审计保留属 Server）
2. **删除死配置**：`platform.auth`（session/ldap 全套，零引用）。LDAP 若真要实现，按 ADR-22 重新设计并归 `winter_server.auth`。
3. **真全局项建 `site.*` 顶层块**（无 role 归属，显式命名）：
   ```yaml
   site:
     domain: winter.local
     https_port: 443
     data_dir: /var/lib/winter
     version: unspecified
   ```
   这四项被多数 role 共享，强行塞进任一 role 会造成新的"寄生"（同 ADR-52 的 etcd 问题）。`site.*` 明确表示"站点级全局配置"。

## 3. 实施

- `all.yml` / `all.yml.example`：删除 `platform` 块（约 57 行）与 `auth` 死配置，新增 `site` 块；在 `repo_nginx` / `harbor` / `winter_server` 字典注入归位项；归属索引 `platform.*` → `site.*`。
- 全仓 **87 处**引用同步替换：34 个文件（role tasks/templates/README、site.yml、deploy.sh、配置内部自引用）。
- 映射：`platform.domain` → `site.domain`、`platform.https_port` → `site.https_port`、`platform.data_dir` → `site.data_dir`、`platform.version` → `site.version`、`platform.repo_base_url` → `repo_nginx.base_url`、`platform.registry_host` → `harbor.hostname`、`platform.registry_ca_path` → `harbor.registry_ca_path`、`platform.retention` → `winter_server.retention`。
- 注意：`rules/platform.yml` 是**文件名**，不是配置引用，保留原样。

## 4. 取舍

- `site.*` 仍是无 role 的顶层块，但它的四项确实是跨 role 共享，且名字明确表达"站点级"，不再是"看起来像 role 却没有 role"的 `platform`。
- Harbor 的 `hostname` 默认值从 `{{ platform.registry_host }}`（空）改为字面 `""`，行为不变（precheck 仍要求非空）。
- 零引用的 `auth` 直接删除而非迁移——不为死配置制造新的归属块。

## 5. 验证（2026-11-27 真机）

- YAML 解析：`all.yml` / `example` 各 74 键，`platform` 已消失，`site` 四项正确。
- 配置解析：`site=winter.local/443//var/lib/winter/unspecified`、`repo=http://192.168.101.189:8082`、`harbor=/`、`ret.audit_online_days=180`。
- syntax-check 通过；prechecks 三台 `failed=0`；vmalert/grafana 幂等重跑 `changed=0 failed=0`；15 个监控服务全 active。
