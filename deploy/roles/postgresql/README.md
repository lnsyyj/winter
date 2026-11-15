# postgresql · 数据库（收敛式）

对应部署阶段 `deploy`。目标不是「装一个 PostgreSQL」，而是**让本机有一个可供平台使用的库与账号**。

## 两条路径

| 机器现状 | 走哪条 | 行为 |
| --- | --- | --- |
| 已有实例在跑 | 收敛 | 只确认服务、建库建号、验证；**不装包、不重启、不改参数** |
| 完全没有 | 安装 | 装 `postgresql17-server`（有 PGDG 源时）或 `postgresql-server`，initdb，起服务 |

## 关键约束

- **不删任何已有库或角色**；只增 `winter` 库与 `winter` 角色。
- **默认不改内存参数**：`ALTER SYSTEM` 需要重启，同机若有其他业务（例如 argus）会被打断。要调参显式开 `postgresql.tune: true`。
- 密码只从 `deploy/passwords.yml` 的 `vault_pg_password` 读（该文件已被 `.gitignore` 排除），**绝不写进 group_vars**。
- 已存在角色时默认不改其密码（`force_password: false`），避免覆盖别人手工改过的口令。

## 变量（`group_vars/all.yml`）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `postgresql.major` | 空（自动探测） | 已在跑的实例优先 |
| `postgresql.db_name` / `db_user` | `winter` / `winter` | 平台库与账号 |
| `postgresql.tune` | `false` | 是否调整内存与连接参数 |
| `postgresql.shared_buffers` 等 | 见 defaults | 仅在 `tune: true` 时生效 |

## 验证

```bash
# 库与角色存在
sudo -u postgres psql -tAc "select datname from pg_database where datname='winter'"
sudo -u postgres psql -tAc "select rolname from pg_roles where rolname='winter'"

# 用应用账号连一次（密码来自 passwords.yml）
PGPASSWORD=... psql -h 127.0.0.1 -U winter -d winter -c 'select 1'
```

## 已知取舍

- 建角色/库的 SQL 会出现在进程参数里（`ps` 可见数秒）。M0 接受；正式交付改用 ansible-vault + `psql -f` 从 stdin 读，避免命令行长驻。
- `listen_addresses` 默认 `127.0.0.1`：管理面组件同机时够用；将来 Server 跨机再放开到内网地址。
