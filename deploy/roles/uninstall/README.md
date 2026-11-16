# role · uninstall

Removes the platform from management nodes (ADR-17 `destroy`). Data volumes are **preserved by default** — a reinstall keeps the database, monitoring data and certificates unless purge is explicitly enabled.

## What is removed

- Platform systemd units and their enabled-at-boot state.
- Nginx site fragments (`winter.conf`, `winter-repo.conf`) without touching other sites' configuration.
- Server/observability binaries, config and staging directories.
- The internal repo firewall rich rule (scoped to the configured subnet and port).
- Service accounts, only when `remove_users: true`.

## Safety

- `purge_data: true` requires an explicit non-empty `data_dirs` list; otherwise the stage fails instead of silently doing nothing.
- `remove_users: false` by default: removing accounts can break UID-based ownership elsewhere.
- Nginx reload failure does not block uninstall — the site owner may already have stopped Nginx.

## Configuration

The canonical path/service lists live in `group_vars/all.yml` under `uninstall_overrides`. Review them before running against a real site; adjust if any path was customized during deployment.

## Usage

```bash
./deploy.sh destroy -i inventory/multinode
./deploy.sh destroy -i inventory/multinode -e '{"uninstall":{"purge_data":true,"data_dirs":["/var/lib/winter/monitoring"]}}'
```
