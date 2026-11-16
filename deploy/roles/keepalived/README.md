# role: keepalived

VRRP high availability for the Nginx access layer (ADR-16). Two or more `[lb]` nodes share a
floating VIP; the node holding the VIP serves `https://<platform.domain>/`. If the local Nginx
health check fails, the VIP moves to a peer.

## Configuration

`ha.*` in `deploy/group_vars/all.yml` is the single entry point:

| Key | Meaning |
| --- | --- |
| `ha.enabled` | Enables the role; `false` keeps single-node behaviour |
| `ha.vip` | Floating address (required when enabled) |
| `ha.interface` | NIC that carries the VIP |
| `ha.virtual_router_id` | VRRP router ID, must be unique on the LAN |
| `ha.priority` | Higher value becomes master; override per host for master/backup |
| `ha.peer_addresses` | Optional unicast peers; the HA playbook computes them from `[lb]` |
| `ha.nginx_check_interval` | Health-check interval in seconds |

`vault_keepalived_auth_pass` comes from `deploy/passwords.yml` (never from `group_vars`). The
VRRP state is `BACKUP` on every node by default; election is decided by priority, which avoids
a split-brain at startup.

## Health check

`/etc/keepalived/check_nginx.sh` exits 0 only when the `nginx` process is running **and** the
HTTPS `/healthz` endpoint answers with the platform `Host` header. On SELinux Enforcing systems,
allow the script to run (for example a local policy for `keepalived_t` executing shell scripts)
before relying on failover.

## Common commands

```bash
systemctl status keepalived
ip addr show dev <ha.interface>          # where the VIP currently lives
journalctl -u keepalived -f
```
