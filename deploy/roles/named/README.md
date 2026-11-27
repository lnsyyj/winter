# role: named

BIND 9 authoritative DNS for the platform domain (ADR-16 / ADR-17). It publishes one A record
per active-active Director VIP, so DNS round-robin spreads clients across every VIP. An optional
slave keeps a TSIG-synced copy of the zone. BIND 9 is chosen because it is the de-facto
enterprise standard on RHEL, supports standard round-robin A records and master/slave transfer.

## Configuration

`named.*` in `deploy/group_vars/all.yml` is the single entry point:

| Key | Meaning |
| --- | --- |
| `named.enabled` | Enables the role; `false` keeps DNS out of the deployment |
| `named.domain` | Zone name, defaults to `site.domain` |
| `named.vips` | A records for the domain; empty falls back to `keepalived.vips`, then `keepalived.vip` |
| `named.master_addresses` / `named.slave_addresses` | Explicit overrides; empty derives addresses from inventory groups |
| `named.forwarders` | Upstream resolvers for external names; empty means authoritative-only |
| `named.recursion` / `named.dnssec` | Resolver behaviour; off by default for an authoritative internal zone |
| `named.allow_query_cidrs` | Query ACL; empty means `any` |
| `named.allow_transfer_cidrs` | Transfer ACL on the master; empty means TSIG key only |
| `named.install_method` | `package` (dnf from host repos) or `repo` (offline RPMs from the internal repo) |
| `named.rpm_files` / `named.rpm_subdir` | BIND RPMs when `install_method=repo` |
| `named.manage_firewall` | Open `named.listen_port`/tcp and /udp with firewalld; set `false` for an external firewall |
| `named.manage_systemd_resolved` | Disable the systemd-resolved stub listener to free `named.listen_port`; `false` to manage the conflict yourself |
| `named.check_interval` | Local resolution retry window (seconds) |

`vault_dns_tsig_secret` comes from `deploy/group_vars/all.yml`. Generate one with
`openssl rand -base64 32` on a trusted machine; the secret never goes into `group_vars`.

### Port 53 and systemd-resolved

On stock RHEL/Rocky hosts `systemd-resolved` owns `127.0.0.53:53` with its stub listener, so BIND
cannot bind `listen_addr: any`. With `named.manage_systemd_resolved: true` (the default) the role
writes `/etc/systemd/resolved.conf.d/winter-dns.conf` containing `DNSStubListener=no` and restarts
the resolver before BIND starts. The resolver service and `/etc/resolv.conf` stay intact, so host
name resolution is unaffected. Set the flag to `false` only when port 53 is already free or the
stub listener is managed outside this role.

## Topology via inventory groups

- `[named]` — all DNS servers (the playbook targets this group).
- `[dns_masters]` — masters; defaults to `[named]` minus `[dns_slaves]`.
- `[dns_slaves]` — slaves; a slave pulls the zone from the masters with TSIG.

Hostnames are never tested in the playbook; roles derive their role from group membership. With
no `[dns_slaves]` group the single DNS node is the master — the default single-node behaviour.

## Active-active VIP round-robin

`dns_vips` is resolved in this order: `named.vips` → `keepalived.vips` → `[keepalived.vip]`. Every address becomes
an `@ IN A` record in the zone, so a client resolving the platform domain receives all VIPs and
BIND rotates the answer order. This completes the multi-VIP Director design: DNS spreads clients
across Directors, LVS/DR spreads them across Nginx backends. When a VIP fails over, its address
is unchanged and the A record stays valid.

## Zone transfer

The master renders a full zone file with a serial derived from a hash of the domain and VIP set,
so editing VIPs changes the serial deterministically and triggers a notify. Slaves keep a
transferred copy under `{{ named.zone_dir }}/slaves/` and do not render the zone themselves. A TSIG
key is required when any slave exists; `named-checkconf` validates the rendered configuration
before the service is reloaded.

## Prechecks

Read-only and idempotent:

```bash
./deploy.sh named -i inventory/multinode --tags precheck.named
```

Checks: configuration validity, DNS topology, TSIG secret presence, package source availability,
offline RPM staging (control machine), port availability and the firewalld tool. Every command task
uses `changed_when: false`, so a check never modifies the host.

## Common commands

```bash
systemctl status named
named-checkconf /etc/named.conf
dig +short @127.0.0.1 winter.local A     # lists all published VIPs
journalctl -u named -f
```
