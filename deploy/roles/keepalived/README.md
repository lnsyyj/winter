# role: keepalived

VRRP high availability for the Nginx access layer (ADR-16). Two or more `[keepalived]` nodes share a
floating VIP; the node holding the VIP serves `https://<site.domain>/`. If the local Nginx
health check fails, the VIP moves to a peer.

## Configuration

`ha.*` in `deploy/group_vars/all.yml` is the single entry point:

| Key | Meaning |
| --- | --- |
| `keepalived.enabled` | Enables the role; `false` keeps single-node behaviour |
| `keepalived.vip` | Single floating address (active-standby) |
| `keepalived.vips` | Multiple addresses for active-active Directors; one per `[keepalived]` node |
| `keepalived.interface` | NIC that carries the VIP |
| `keepalived.virtual_router_id` | VRRP router ID, must be unique on the LAN |
| `keepalived.priority` | Higher value becomes master; override per host for master/backup |
| `keepalived.peer_addresses` | Optional unicast peers; the HA playbook computes them from `[keepalived]` |
| `keepalived.nginx_check_interval` | Health-check interval in seconds |

`vault_keepalived_auth_pass` comes from `deploy/group_vars/all.yml` (never from `group_vars`). The
VRRP state is `BACKUP` on every node by default; election is decided by priority, which avoids
a split-brain at startup.

## Health check

`/etc/keepalived/check_nginx.sh` exits 0 only when the `nginx` process is running **and** the
HTTPS `/healthz` endpoint answers with the platform `Host` header. On SELinux Enforcing systems,
allow the script to run (for example a local policy for `keepalived_t` executing shell scripts)
before relying on failover.

## Active-active Directors (multi-VIP)

A single VIP with VRRP is failover only: the standby Director stays idle. Set `keepalived.vips` to a list
with one address per `[keepalived]` node (for example `[10.0.0.100, 10.0.0.101]`) to run every Director at
once. The role generates one `vrrp_instance` per VIP; each Director is MASTER for one instance and
BACKUP for the rest. If a node fails, its VIPs move to the surviving node.

Publish **every** VIP as an A record for the platform domain so DNS resolution spreads clients
across Directors. Multi-A DNS is a site prerequisite — this framework does not manage external
DNS. With a single `keepalived.vip` the original active-standby behaviour is unchanged.

## LVS/DR load balancing

VRRP alone is failover only: the VIP lives on one node at a time, so every connection reaches the
same Nginx. Enable `keepalived.lvs.enabled: true` to use Keepalived's native LVS in DR mode. The Director
still holds the VIP (with VRRP failover), but `virtual_server` schedules connections across every
`[nginx]` backend using `keepalived.lvs.scheduler` (`rr` / `wrr` / `lc` / `wlc`).

Real servers need the VIP on `lo` plus ARP suppression; that is the job of `roles/lvs-realserver`,
applied by `application-ha` to `[nginx]`. Return traffic goes straight from the real server to the
client with the VIP as source, so the Director only handles inbound packets.

Requirements: at least two `[nginx]`, `keepalived.vip`, and the `ip_vs` kernel module (loaded by
this role). `keepalived.lvs.persistence_timeout: 0` disables session affinity; raise it only when an
upstream needs sticky connections.

## Common commands

```bash
systemctl status keepalived
ip addr show dev <keepalived.interface>          # where the VIP currently lives
journalctl -u keepalived -f
```
