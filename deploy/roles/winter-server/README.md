# role: winter-server

Deploys the Go single binary cross-compiled from `server/` as a systemd service, replacing manual `nohup`.

- Artifact: `deploy/artifacts/repo/bin/winter-server` (copied from `server/bin/winter-server`,
  **no compilation on site**, ADR-14).
- Install: `/usr/local/bin/winter-server`, dedicated account `winter-server` (non-root).
- Configuration: `/etc/winter/server.env`, mode 0640, template `templates/server.env.j2`.
  `DATABASE_URL` is assembled from `vault_pg_password` in `passwords.yml` and `pg_cluster.haproxy.listen_port`;
  `JWT_SECRET` / `ADMIN_PASSWORD` also come only from `passwords.yml`.
- Service: `winter-server.service`, `Type=exec`, `Restart=always`, start on boot.
- Port: `winter_server.port` (default 8081), must match `nginx.api_upstream`.
- Multi-replica: `./deploy.sh application-ha` deploys this role to every `[winter-server]` host and overrides
  `winter_server.listen_addr` with that host's internal address. Set `server_ha.database_host` /
  `audit_database_host` to the PG HA access point, then Nginx load-balances the replicas through
  `upstream winter_api`. The binary is stateless — sessions and tokens live in PostgreSQL (ADR-34).

## Idempotency and zero-downtime overwrite

`install` directly overwriting a running binary triggers `ETXTBSY`, so first download the new package to
`/var/tmp/winter-server/winter-server.new`, compare content with `cmp`, copy it to the target directory,
then **atomically replace the inode with `mv` in the same directory** (keep the downloaded source file so `get_url` stays idempotent):
if content is unchanged, nothing changes and there is no restart; only a content change triggers a restart via notify.

## Prerequisites

1. Run `local-repo` (artifact repository available) and `postgresql` (database and account exist) first;
2. `deploy/passwords.yml` must have `vault_pg_password`, `vault_jwt_secret` (≥32 characters),
   `vault_admin_password`; the role asserts to block missing values, never generating a default weak password;
3. Migrations and the break-glass administrator are handled automatically at process startup; the role does not run SQL again.

## Common commands

```bash
./deploy.sh deploy --tags server
systemctl status winter-server
curl -s http://127.0.0.1:8081/healthz
curl -sk --resolve winter.local:443:127.0.0.1 https://winter.local/api/v1/me
```
