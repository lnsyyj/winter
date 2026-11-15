# ADR-22 · 认证源：本地账号 + LDAP 作为可配置项（取代 ADR-21）

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-15 |
| 决策人 | 项目负责人 |
| 取代 | [ADR-21](ADR-21-认证源.md)（原定「LDAP 仅 V1 预留、不实现」） |
| 影响范围 | 架构设计 §4.1 / §11 / §14、安全与审计设计 §3 / §8、PRD D7、安装手册 C6、`deploy/group_vars`、TC-28 |
| 关联 | [ADR-19 Agent 权限](ADR-19-Agent权限模型.md) · [安全与审计设计](../../07-新兴与稀缺线/19-安全工程师/安全与审计设计.md) |

---

## 1. 变更点

ADR-21 的结论是「MVP 只做本地账号，LDAP 只留字段与接口」。本次调整为：

> **LDAP 作为配置项提供，客户填配置即可对接；不是版本特性，也不需要改代码。**

| 项 | ADR-21 | 本 ADR |
| --- | --- | --- |
| 本地账号 | MVP 唯一认证方式 | 保留，且**始终可用**（break-glass） |
| LDAP | V1 预留，不实现 | **MVP 即支持，由配置开关控制** |
| OIDC | V1 预留 | 仍不做（不在本次范围） |
| 实现约束 | Argon2id、会话、限流、初始管理员 | **全部继承，不重复** |

其余实现约束（密码哈希、会话、登录限流、初始管理员强制改密、认证与 RBAC 分离）继续按 ADR-21 执行。

## 2. 为什么改

1. **很多客户不接受「只有本地账号」**：政企与金融场景，账号必须来自目录服务，否则过不了评审。
2. **做成配置项比做成版本更合适**：不需要新版本、不需要重新发版，现场改 `group_vars` 就能开。
3. **成本可控**：认证是「一次 bind + 一次 search」，不涉及数据同步，复杂度远低于「同步用户表」。
4. **风险可隔离**：默认关闭，只有显式配置才启用；本地管理员始终保留，目录服务故障不会把平台锁死。

## 3. 认证模型

```
登录请求（用户名 + 密码）
  ├─ 1. local：命中本地账号 → Argon2id 校验 → 通过
  ├─ 2. ldap（若启用）：
  │      bind 服务账号 → search user_filter → 取 user_dn
  │      → 用 user_dn + 用户密码 re-bind（验证密码，不落库）
  │      → search group_filter 取组 → 按 role_map 映射角色
  │      → 首次登录 JIT 建用户（idp=ldap, subject=user_dn）
  └─ 3. 都失败 → 统一错误「用户名或密码错误」，不泄露哪一步失败
```

**两条路径互相独立**：LDAP 超时或不可用时，本地账号仍能登录；本地账号不存在时才尝试 LDAP。

## 4. 配置项（放 `group_vars`，密码放 `passwords.yml`）

```yaml
platform:
  auth:
    providers: [local, ldap]          # 顺序即尝试顺序；local 不可移除
    ldap:
      enabled: false
      url: "ldaps://dc01.corp.local:636"
      starttls: false
      tls_verify: true
      bind_dn: "cn=svc-winter,ou=svc,dc=corp,dc=local"
      # bind 密码不进本文件：passwords.yml 里的 vault_ldap_bind_password
      base_dn: "ou=people,dc=corp,dc=local"
      # 占位符 {login} / {user_dn} 由平台在登录时替换，不是 Ansible 变量
      user_filter: "(sAMAccountName={login})"
      group_base_dn: "ou=groups,dc=corp,dc=local"
      group_filter: "(member={user_dn})"
      role_map:                        # LDAP 组 DN → 平台角色
        "CN=ceph-admins,OU=groups,DC=corp,DC=local": admin
        "CN=ceph-ops,OU=groups,DC=corp,DC=local": operator
      default_role: none               # 未命中映射 = 无权限（deny by default）
      timeout_seconds: 5
      group_cache_ttl_seconds: 300
```

## 5. 强制约束

| 约束 | 原因 |
| --- | --- |
| **必须 TLS**（`ldaps://` 或 `starttls: true`）且默认校验证书 | 明文 simple bind 等于把密码广播到网络上 |
| **bind 密码只存 `passwords.yml`**，静态加密 | 与 Ceph 凭据同等级别，见安全设计 §3 |
| **只用用户凭据做一次 re-bind 验证**，不缓存密码 | 平台不持有用户口令，降低泄露面 |
| **`default_role: none`**，未映射组一律无权限 | 组名写错时宁可登不进去，也不能给默认权限 |
| **本地管理员不可被 LDAP 用户覆盖或删除** | break-glass：目录服务挂了仍能进平台 |
| 登录失败统一提示，不区分「用户不存在 / 密码错 / 组未映射」 | 防用户名枚举 |
| 组映射缓存 TTL ≤300s，且可强制刷新 | 改角色后不必等太久，也不至于每次登录都打目录 |
| 登录审计记录 `provider`、`user_dn`、结果，**不记录密码** | 追溯「谁在什么时候从哪个源登录」 |
| 限流按「用户名 + 来源 IP」计数，**跨 provider 共用** | 否则换一个 provider 就绕过爆破限制 |

## 6. 代价

| 代价 | 说明 |
| --- | --- |
| 测试面变大 | 需要 LDAP 测试环境（可用 lldap / OpenLDAP 容器），覆盖 bind 失败、组未映射、目录超时 |
| 实施成本转移 | 现场要填 `user_filter` / `role_map`，填错就是「登录成功但没权限」，必须给排查指引 |
| 多一层故障源 | 目录服务不可用会阻塞 LDAP 用户登录（本地账号不受影响） |
| 组模型差异 | AD 的 `sAMAccountName`、OpenLDAP 的 `uid`、嵌套组都需要按客户调整 filter；本 ADR 只支持一层组，不做递归 |

## 7. 验收（可测）

| 场景 | 期望 |
| --- | --- |
| LDAP 正确凭据 | 登录成功，角色由 `role_map` 决定 |
| LDAP 错误密码 | 失败，提示统一，不泄露用户名是否存在 |
| 用户组未命中 `role_map` | 登录被拒或登录成功但无任何权限（按 `default_role: none`），且审计有记录 |
| 关闭 LDAP | 本地账号正常，LDAP 用户无法登录 |
| LDAP 不可达 | 本地账号仍可登录；LDAP 用户得到「目录服务不可用」而非「密码错误」 |
| 明文 ldap:// 且未开 starttls | 配置校验直接拒绝启动 |
| 连续失败 | 触发限流，且换 provider 不能绕过 |

对应测试用例：TC-28。

## 8. 不做的事

- **不做 OIDC**（保持 V1 之后评估）。
- **不做用户/组同步**：不做定时同步，只在登录时按需查询（避免引入同步任务与冲突处理）。
- **不做多 LDAP 并存**：一套配置只对接一个目录。
- **不做嵌套组递归**：只解析一层 `group_filter` 结果，需要递归请用目录侧扁平化。

## 9. 什么情况下推翻

- 客户要求 OIDC / SAML 且不接受 LDAP → 新增 provider 类型，本 ADR 扩展而非推翻。
- 需要「用户离职即自动禁用」这类强同步语义 → 必须引入同步任务，架构改动较大，另立 ADR。

推翻/扩展流程同前：新开 ADR → 本 ADR 视情况标 `Superseded` → 同步架构设计 §4.1/§11、PRD D7、安全设计、安装手册 C6、`group_vars` 与 TC-28。
