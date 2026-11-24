# role: harbor

Harbor registry (ADR-16). The offline installer is fetched from the internal Nginx artifact
repository, configured via `harbor.yml`, and started with docker-compose. Two modes:

- **`builtin` (default, MVP single node)** — uses Harbor's bundled PostgreSQL and Redis, with a
  local data volume. No external PG / Redis / NFS required; only `vault_harbor_admin_password`
  is needed. This matches the ADR-16 MVP decision (Harbor's bundled PG or a standalone instance;
  multi-replica + external dependencies are V1). Recommended for the MVP and for lab/single-node
  deployments.
- **`external` (V1 HA)** — multiple replicas share an external PostgreSQL, Redis and an NFS
  share. All three are hard prerequisites; this role never deploys a Redis or NFS server.

## Hard prerequisites (both modes)

- Docker and `docker-compose` are installed on every Harbor node (asserted with an actionable
  failure). `bootstrap-servers` does not provide a container runtime.
- The offline installer `harbor.tarball` is present under `deploy/artifacts/repo/tarball/`. It is
  not fetched from the internet by this role because of its size and distribution terms.

Additional prerequisites in `external` mode only:

- An external PostgreSQL database (`harbor.database.host`) with the configured database/user.
- An external Redis instance (`harbor.redis.host`).
- An NFS share (`harbor.storage.nfs_server` / `nfs_path`) reachable from every Harbor node; the
  role mounts it and adds an fstab entry.

## Configuration

`harbor.*` in `deploy/group_vars/all.yml` is the single entry point.

- `harbor.mode: builtin` — set `harbor.local_volume` (default `/var/lib/winter/harbor/data`).
- `harbor.mode: external` — fill `harbor.database.*`, `harbor.redis.*`, `harbor.storage.nfs_*`.

Credentials come only from `deploy/group_vars/all.yml`: `vault_harbor_admin_password` (always),
plus `vault_harbor_db_password` and `vault_harbor_redis_password` in external mode.
`harbor.yml` is rendered with mode 0640.

Set `harbor.enabled: true` to opt in; standalone `harbor` and `application-ha` stages skip the role
while it is false.

## Data safety

- builtin mode: registry and bundled DB/Redis data live on the local volume; removing Harbor does
  not delete it by default.
- external mode: `data_volume` points at the shared NFS mount so all replicas see the same data.
  No cleanup task is provided: removing a replica never deletes registry data. Decommissioning
  storage is a deliberate, out-of-band operation.

## Common commands

```bash
./deploy.sh harbor -i inventory/all-in-one
./deploy.sh application-ha -i inventory/multinode       # multi-replica, requires harbor.mode=external
docker-compose -f /opt/winter/harbor/docker-compose.yml ps
curl -fsS http://127.0.0.1:80/api/v2.0/health
```
