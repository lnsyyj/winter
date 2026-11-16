# role: harbor

Harbor HA replica (ADR-16). The offline installer is fetched from the internal Nginx artifact
repository, configured against external PostgreSQL / Redis / NFS, and started with docker-compose.
Multiple replicas share the same database, Redis and object data, so Nginx can load-balance them.

## Hard prerequisites

The role asserts these and fails with an actionable message instead of pretending success:

- Docker and `docker-compose` are installed on every Harbor node.
- An external PostgreSQL database (`harbor.database.host`) with the configured database and user.
- An external Redis instance (`harbor.redis.host`).
- An NFS share (`harbor.storage.nfs_server` / `nfs_path`) reachable from every Harbor node; the
  role mounts it and adds an fstab entry. It does **not** deploy an NFS or Redis server.
- The offline installer `harbor.tarball` is present under `deploy/artifacts/repo/tarball/`. It is
  not fetched from the internet by this role because of its size and distribution terms.

## Configuration

`harbor.*` in `deploy/group_vars/all.yml` is the single entry point. Credentials come only from
`deploy/passwords.yml`: `vault_harbor_db_password`, `vault_harbor_redis_password` and
`vault_harbor_admin_password`. `harbor.yml` is rendered with mode 0640.

Set `harbor.enabled: true` to opt in; the `ha-setup` stage skips the role while it is false.

## Data safety

`data_volume` points at the shared NFS mount, so all replicas see the same registry data. No
cleanup task is provided: removing a replica never deletes registry data. Decommissioning storage
is a deliberate, out-of-band operation.

## Common commands

```bash
./deploy.sh ha-setup -i inventory/multinode
docker-compose -f /opt/winter/harbor/docker-compose.yml ps
curl -fsS http://127.0.0.1:80/api/v2.0/health
```
