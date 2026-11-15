# role: winter-server

把 `server/` 交叉编译出的 Go 单二进制部署成 systemd 服务，替代手工 `nohup`。

- 制品：`deploy/artifacts/repo/bin/winter-server`（由 `server/bin/winter-server` 复制而来，
  现场**不编译**，ADR-14）。
- 安装：`/usr/local/bin/winter-server`，专用账号 `winter-server`（非 root）。
- 配置：`/etc/winter/server.env`，权限 0640，模板 `templates/server.env.j2`。
  `DATABASE_URL` 由 `passwords.yml` 的 `vault_pg_password` 与 `pg_cluster.haproxy.listen_port`
  拼出；`JWT_SECRET` / `ADMIN_PASSWORD` 同样只来自 `passwords.yml`。
- 服务：`winter-server.service`，`Type=exec`、`Restart=always`、开机自启。
- 端口：`winter_server.port`（默认 8081），必须与 `nginx.api_upstream` 一致。

## 幂等与不停机覆盖

`install` 直接覆盖正在执行的二进制会触发 `ETXTBSY`，所以先把新包下载到
`/var/tmp/winter-server/winter-server.new`，`cmp` 比对内容后复制到目标目录、
再用**同目录 `mv` 原子替换 inode**（保留下载源文件，让 `get_url` 保持幂等）：
内容不变则完全不改动、不重启；内容变化才 notify 重启。

## 前置

1. 先跑过 `local_repo`（制品源可用）与 `postgresql`（库与账号存在）；
2. `deploy/passwords.yml` 必须有 `vault_pg_password`、`vault_jwt_secret`（≥32 字符）、
   `vault_admin_password`，role 会 assert 拦截缺失，绝不生成默认弱口令；
3. 迁移与 break-glass 管理员由进程启动时自动完成，role 不重复执行 SQL。

## 常用命令

```bash
./deploy.sh deploy --tags server
systemctl status winter-server
curl -s http://127.0.0.1:8081/healthz
curl -sk --resolve winter.local:443:127.0.0.1 https://winter.local/api/v1/me
```
