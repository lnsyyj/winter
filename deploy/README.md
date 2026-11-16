# deploy · Platform Deployment Framework

Established per [ADR-17](../docs/02-研发线/04-架构师/ADR-17-部署框架.md): the structure is aligned with kolla-ansible, but the code is independent — no fork and no dependency on its Python packages.

## Current Status

| Stage | Command | Status |
| --- | --- | --- |
| Bootstrap servers | `./deploy.sh bootstrap-servers -i inventory/multinode` | ✅ **Implemented** (base packages + optional kernel/data-disk settings) |
| Prechecks | `./deploy.sh prechecks -i inventory/multinode` | ✅ **Implemented** (read-only, for the provisioning phase) |
| Converge deployment | `./deploy.sh deploy -i inventory/multinode` | ✅ **Implemented** (internal repo + PostgreSQL + Nginx + observability stack + Server) |
| Converge monitoring stack/Server only | `./deploy.sh deploy --tags observability,server` | ✅ **Implemented** (automatically includes the internal repo) |
| PG high availability | `./deploy.sh pg-ha -i inventory/pgha` | ✅ **Implemented** (etcd + Patroni + HAProxy, three nodes) |
| PG HA scale out | `./deploy.sh pg-scale-out -i inventory/pgha -e '{"pg_scale":{"nodes":["pg-d","pg-e"]}}'` | ✅ **Implemented** (etcd member add + replica join + HAProxy update) |
| PG HA scale in | `./deploy.sh pg-scale-in -i inventory/pgha -e '{"pg_scale":{"removed_nodes":["pg-e"]}}'` | ✅ **Implemented** (replica only, data preserved by default) |
| Monitoring scale out | `./deploy.sh monitor-scale-out -i inventory/multinode -e '{"monitoring_scale":{"nodes":["mon02"]}}'` | ✅ **Implemented** (Alertmanager gossip HA + Grafana replicas + vmalert notifiers; targets must be in `[monitor]`) |
| Monitoring scale in | `./deploy.sh monitor-scale-in -i inventory/multinode -e '{"monitoring_scale":{"removed_nodes":["mon02"]}}'` | ✅ **Implemented** (references rewired first, then teardown; data preserved by default) |
| HA converged deployment | `./deploy.sh ha-setup -i inventory/multinode` | ✅ **Implemented** (Server/Harbor replicas + Nginx upstreams + Keepalived VIP; prerequisites asserted) |
| Scale out (install Agent) | `./deploy.sh scale-out -i inventory/multinode --limit node04` | ✅ **Implemented** (requires the Agent binary and CA; see below) |
| Scale in (remove Agent) | `./deploy.sh scale-in -i inventory/multinode --limit node03` | ✅ **Implemented** (data preserved by default) |
| Destroy (uninstall) | `./deploy.sh destroy -i inventory/multinode` | ✅ **Implemented** (data preserved by default; explicit purge required to delete) |
| Post-deploy | `./deploy.sh post-deploy -i inventory/multinode` | ✅ **Implemented** (entry-point checks + release manifest) |
| Reconfigure | `./deploy.sh reconfigure -i inventory/multinode` | ✅ **Implemented** (validates config, then re-converges) |
| Upgrade | `./deploy.sh upgrade -i inventory/multinode -e '{"upgrade":{"target_version":"v0.3.0"}}'` | ✅ **Implemented** (SHA256 verification + converge + acceptance) |
| pull | — | ⬜ Not implemented — intentionally obsolete: management components are static artifacts, Ceph images are pulled by cephadm (ADR-30) |

Unimplemented stages exit with an explicit error (exit code 2); they **do not pretend to succeed**.

## Offline Artifact Repository (ADR-14 / ADR-15)

All three on-site sources are unreliable (verified: the official PGDG site times out, EPEL has no PG17/etcd, and pip's file site times out), so all packages are **committed with the code**, published by Nginx on the management node, and pulled by nodes over the internal network.

```
deploy/artifacts/repo/
├── rpm/pgdg17/     4 RPMs for PG 17 (no contrib, see below)
├── rpm/base/       haproxy, perl-libs and other base dependencies
├── tarball/        etcd + 5 static packages for the observability stack
├── bin/            winter-server (Go single binary, cross-compiled artifact)
├── pip/            patroni and its dependency wheels
└── SHA256SUMS.txt  full transfer checksum manifest (rpm/pip/tarball/bin): cd deploy/artifacts/repo && sha256sum -c SHA256SUMS.txt
```

| Step | Approach |
| --- | --- |
| Build packages with internet access | `sh deploy/scripts/fetch-artifacts.sh` (reproducible; `GHPROXY=` for a direct connection, `GHPROXY=https://ghproxy.net` via mirror) |
| Transfer verification | Artifacts are transferred with git; on site, verify each item with `sha256sum -c` (ADR-14, second section) |
| Publish | `./deploy.sh deploy --tags repo`: copy to `/var/lib/winter/www/repo`; Nginx serves plain HTTP on **8082**, and the firewall allows only the internal subnet |
| Pull | Roles use `get_url {{ platform.repo_base_url }}/...`; RPMs are installed locally with `dnf --disablerepo='*'`, **without refreshing any remote repository metadata** |

### Observability Stack Artifacts (versions pinned, no latest)

| Component | Version | Artifact |
| --- | --- | --- |
| VictoriaMetrics | v1.156.0 | `victoria-metrics-linux-amd64-v1.156.0.tar.gz` |
| vmalert | v1.156.0 | `vmalert-linux-amd64-v1.156.0.tar.gz` |
| VictoriaLogs | v1.54.0 | `victoria-logs-linux-amd64-v1.54.0.tar.gz` |
| Alertmanager | 0.35.0 | `alertmanager-0.35.0.linux-amd64.tar.gz` |
| Grafana | 9.5.20 | `grafana-9.5.20.linux-amd64.tar.gz` |
| Server | `17ba4fb` | `bin/winter-server` |

Two deliberate trade-offs:

- **vmalert is not released separately upstream**; it ships only with `vmutils` (~140MB). The packaging script extracts `vmalert-prod` and repackages it into a ~14MB tarball, avoiding bringing the full tool suite into the repo.
- **Grafana 9.5.20 (83.6MB) was chosen** because it is the last stable release whose tar.gz is under GitHub's 100MB single-file limit; 11.x and later are all >170MB, so upgrades must switch to splitting or physical media transfer.

> Verified: locally via ghproxy ~18KB/s (unusable); on real machines with direct GitHub access ~1.7MB/s (15MB/9s).
> The packaging script uses the proxy by default; on machines with direct access use `GHPROXY= sh deploy/scripts/fetch-artifacts.sh`.

**Verified on real machines** (2026-11-15): on `.197` and `.198`, two **minimal systems without a PGDG repo**, PG 17 was installed entirely from the internal repo (`ok=23 failed=0`), and the application account `winter` connects normally.

**Two pitfalls worth recording**:

1. `postgresql17-contrib` depends on `libperl` → `perl-libs` → `perl(Carp)/perl(Encode)…`, a whole perl chain, requiring a dozen-plus extra packages on minimal systems. **HA does not use contrib, so it is excluded**; add it later if needed.
2. `dnf download --resolve` skips dependencies on **machines where they are already installed** (this run missed `perl-libs`). The fix is a final `rpm -qa` comparison, or generating the closure in a clean container.

## Observability Stack and Server (ADR-18 / ADR-09 / ADR-33)

`./deploy.sh deploy` converges everything at once; you can also run only `--tags observability,server`.
The component list, ports, versions and command-line arguments all live in `group_vars/all.yml`; roles do not hardcode ports.

| Component | Port (default) | systemd | Health endpoint |
| --- | --- | --- | --- |
| VictoriaMetrics | 8428 | `winter-victoriametrics` | `/health` |
| vmalert | 8880 | `winter-vmalert` | `/health` |
| VictoriaLogs | 9428 | `winter-victorialogs` | `/health` |
| Alertmanager | 9093 | `winter-alertmanager` | `/-/healthy` |
| Grafana | 3000 | `winter-grafana` | `/api/health` |
| Server | 8081 | `winter-server` | `/healthz` |

- All listen only on `127.0.0.1` and run as non-root users; Grafana is exposed externally only through the `https://<domain>/grafana/` reverse proxy (ADR-09), and ships with a default datasource pointing at local VictoriaMetrics.
- Single alerting path: `vmalert → Alertmanager → webhook` (ADR-18); M0 uses placeholder rules, the Server's HMAC webhook endpoint is not implemented yet, and dropped alerts do not affect tasks.
- Metrics/logs are retained for 30 days; **audits live in PG, not in the logging system** (ADR-06 / ADR-32).
- `observability.enabled: false` or a single component's `enabled: false` trims the stack (ADR-18 packaging layers).
- The Server's `DATABASE_URL` / `JWT_SECRET` / admin password all come from `deploy/passwords.yml` (see `passwords.yml.example` for the template); roles assert explicitly and do not generate default weak passwords.
- Overwriting a running binary triggers `ETXTBSY`, so the Server role does `cp` in the target directory + `mv` within the same directory to atomically replace the inode, keeping the downloaded source file for idempotency; no restart if content is unchanged.

## Agent Pre-install, Scale-out and Scale-in (ADR-35 / ADR-19 / ADR-28)

Managed nodes get the Agent during the provisioning phase; at runtime there is no SSH. Scaling the platform means installing or removing the Agent on nodes, not opening inbound ports.

```bash
# Add new hosts to [ceph_nodes] first, then target only them
./deploy.sh scale-out -i inventory/multinode --limit node04

# Remove the Agent; platform data is preserved unless explicitly purged
./deploy.sh scale-in -i inventory/multinode --limit node03
```

Prerequisites for `scale-out` (the role fails with an actionable message when missing, never pretends success):

| Prerequisite | Configuration |
| --- | --- |
| `winter-agent` binary in the internal repo | `platform.repo_base_url` + `agent_overrides.artifact` |
| CA certificate on the control machine | `agent_overrides.cert.ca_source` |
| Nginx `grpc_pass` for the Agent service on outbound 443 | infrastructure outside this role |

Identity: `node_id` defaults to the inventory hostname and must match the certificate `CN` and `SPIFFE URI:spiffe://winter/node/<node_id>`. In `local_csr` mode the private key is generated on the node and never leaves it; a CSR is staged, and the service starts only once a certificate is issued. In `preissued` mode a key/cert pair is copied from the control machine.

Safety: the service account is non-root with an explicit sudoers whitelist (absolute paths, no `ALL`, no interpreters). `scale-in` preserves data by default; `purge_data: true` requires an explicit non-empty `data_dirs` list, and `remove_user` is opt-in.

## PostgreSQL High Availability (etcd + Patroni + HAProxy)

```bash
./deploy.sh pg-ha -i inventory/pgha                  # full deployment
./deploy.sh pg-ha -i inventory/pgha --tags etcd      # DCS only
./deploy.sh pg-ha -i inventory/pgha --tags patroni   # PG cluster only
./deploy.sh pg-ha -i inventory/pgha --tags haproxy   # entry point only
```

| Component | Role | Port (`group_vars.pg_cluster`) |
| --- | --- | --- |
| etcd ×3 | Stores cluster state, Raft leader election (must be odd) | 2379 / 2380 |
| Patroni ×3 | Manages the PG process, replication, switchover/failover, rewind | REST 8008 |
| PostgreSQL ×3 | One primary, two replicas | **5433** |
| HAProxy ×3 | Single application entry point, follows Patroni `/primary` for leader election | **5434** |

> 5433/5434 instead of 5432: on `.194`, port 5432 is already used by an existing service (argus); change ports only in
> `pg_cluster` under `group_vars`; roles do not hardcode them.

### Verified Results (2026-11-15, three nodes)

| Scenario | Result |
| --- | --- |
| bootstrap | pg-b becomes Leader, the other two `streaming`, Lag 0 |
| Planned switchover | `patronictl switchover` → pg-a takes over, TL+1, entry point follows automatically |
| **Node loss** (stop patroni + `kill -9 postgres`) | **Automatic switchover within 10 seconds** to pg-c, HAProxy seamlessly points to the new primary |
| Failed node recovery | After restart, automatically `rewind`s and rejoins as a streaming replica, Lag 0 |
| Write verification | Created a table and inserted via 5434 successfully; replicas synced |

### Three Pitfalls You Must Know (all found on real machines)

1. **`listen_addresses` / `port` must not be set in `bootstrap.dcs.postgresql.parameters`**: Patroni fails validation and ignores them, which shows up as the primary listening only on localhost and replicas hanging on `pg_basebackup`. The listen address and port are determined by `postgresql.listen/port`.
2. **Execution order of `bootstrap.users` and the `post_bootstrap` script is not guaranteed**: the script must idempotently create roles (including `replicator`) and the application database.
3. **On machines with SELinux Enforcing, two steps are needed**: `semanage port -a -t http_port_t -p tcp 5434` (to bind the port) + `setsebool -P haproxy_connect_any on` (to connect to backends 5433/8008). Missing either shows up as `cannot bind socket` and `server closed the connection unexpectedly`, respectively.

### Scaling the PG HA Cluster

Add the new hosts to `[management]` in `inventory/pgha` first; topology is always expressed by inventory groups.

```bash
# 3 -> 5: add two nodes at once so the etcd member count stays odd
./deploy.sh pg-scale-out -i inventory/pgha -e '{"pg_scale":{"nodes":["pg-d","pg-e"]}}'

# 5 -> 3: remove a replica (never the leader); PG and etcd data is preserved
./deploy.sh pg-scale-in -i inventory/pgha -e '{"pg_scale":{"removed_nodes":["pg-e"]}}'
```

Safety rules enforced by the playbooks:

- etcd requires an **odd final member count >= 3**; the stage refuses an even or too-small result.
- Scale-in verifies the target answers `/replica` (200) before touching it — the leader can never be removed.
- New etcd members are registered with `etcdctl member add` and join with `initial-cluster-state=existing`; the templates are membership-aware, so a plain `pg-ha` rerun still renders the static bootstrap cluster.
- `serial: 1` during scale-in prevents a simultaneous DCS restart.
- Data is never deleted implicitly: `pg_scale.purge_data: true` requires an explicit `pg_scale.data_dirs` list.

After a successful scale-in, remove the hosts from `[management]` in the inventory so future runs match the real topology.

### Scaling the Monitoring Stack

Add the new hosts to `[monitor]` in the inventory first; topology is always expressed by inventory groups. Every name in
`monitoring_scale.nodes` / `removed_nodes` must already be a member of that group.

```bash
# Add a monitor node: gossip peer + Grafana replica + vmalert notifier
./deploy.sh monitor-scale-out -i inventory/multinode -e '{"monitoring_scale":{"nodes":["mon02"]}}'

# Remove a monitor node: rewire references first, then tear down services
./deploy.sh monitor-scale-in -i inventory/multinode -e '{"monitoring_scale":{"removed_nodes":["mon02"]}}'
```

Scope and boundaries:

- **Stateless / HA layers scale horizontally**: Alertmanager forms a gossip cluster (`monitoring_scale.cluster_port`,
  default 9094), vmalert notifies every Alertmanager replica, and Nginx load-balances Grafana across all monitor nodes.
- **VictoriaMetrics / VictoriaLogs stay single-node**: they use the single-node binaries with local storage, so adding a
  monitor node does **not** create a distributed datastore. Point `monitoring_scale.metrics_query_url` at a central
  VictoriaMetrics endpoint when replicas must be aggregated; the playbooks never configure `remote_write` implicitly.
- Multi-node runs bind components to `0.0.0.0` and advertise each node's `ansible_host`; scaling back to one node
  restores the localhost-only binding.
- At least one monitor node must remain; scale-in refuses to remove the last one.
- Data is preserved by default: `monitoring_scale.purge_data: true` requires an explicit non-empty
  `monitoring_scale.data_dirs` list.

### High Availability (Server / Harbor)

Server is stateless and Harbor is shared-storage based, so both are deployed as multiple replicas
behind Nginx, with Keepalived providing a floating VIP on the `[lb]` nodes.

**VRRP alone is failover, not load balancing**: the VIP lives on one node at a time, so every
connection reaches the same Nginx. Enable `ha.lvs.enabled: true` to use Keepalived's native LVS in
DR mode. The VIP still moves with VRRP, but connections are scheduled across all `[nginx_nodes]`
backends and real servers answer directly with the VIP as source, bypassing the Director.

```bash
./deploy.sh ha-setup -i inventory/multinode
```

Inventory groups: `[lb]` (LVS Director + Keepalived), `[nginx_nodes]` (real Nginx backends),
`[server_nodes]` (Server replicas) and `[harbor_nodes]` (Harbor replicas). All default to
`management` in the shipped inventories, so single-node runs are unchanged. With LVS enabled,
`[lb]` and `[nginx_nodes]` must be disjoint and share an L2 network — the stage refuses an overlap.

Configuration in `group_vars/all.yml`:

- `ha.vip`, `ha.interface`, `ha.priority` — VRRP address and election; the shared
  `vault_keepalived_auth_pass` comes from `passwords.yml`. Priority descends across `[lb]`.
- `ha.lvs.enabled: true` and `ha.lvs.scheduler` (`rr` / `wrr` / `lc` / `wlc`) — four-layer load
  balancing; requires at least two `[nginx_nodes]`. The `realserver` role puts the VIP on `lo`
  with ARP suppression on each backend, and persists it with a oneshot systemd unit.
- `server_ha.database_host` / `audit_database_host` — must point at the PG HA access point once
  Server replicas span nodes; the stage refuses `127.0.0.1` in that case.
- `harbor.enabled: true` plus `harbor.database.*`, `harbor.redis.*`, `harbor.storage.*` — all
  required for Harbor replicas; the offline installer must be present in the internal repo.

Harbor hard prerequisites: Docker and `docker-compose` on every Harbor node, an external
PostgreSQL, an external Redis, and an NFS share mounted by the role. The role fails with an
actionable message when any is missing; it never deploys an NFS or Redis server itself.

Nginx gains `upstream winter_api` and `upstream winter_harbor`: `/api/v1/` stays with the Server,
while `/api/v2.0/`, `/v2/`, `/service/` and `/harbor/` are routed to Harbor.

### Common Operations Commands

```bash
export PATH=/usr/pgsql-17/bin:$PATH
patronictl -c /etc/patroni/patroni.yml list            # view topology
patronictl -c /etc/patroni/patroni.yml switchover      # planned switchover
patronictl -c /etc/patroni/patroni.yml failover        # forced failover
etcdctl --endpoints=http://127.0.0.1:2379 endpoint health --cluster
```

## Real-Machine Verification Record (2026-11-15 · CentOS Stream 9)

Base layer: `./deploy.sh deploy` ran through with `ok=25 changed=2 failed=0`; the second run gave `changed=0` (idempotent).

| Verification Item | Result |
| --- | --- |
| PostgreSQL | Reused the existing 17.11 instance, added the `winter` database and role; the application account connects, a wrong password is rejected |
| Existing databases | `appdb`/`postgres`/`template*` left untouched |
| Nginx overlay | `winter.local` site returns 200, `/healthz` returns ok; **the existing site on the same host (argus) still returns 200 on 80/443** |
| Password source | `deploy/passwords.yml` (gitignored), not committed to the repo |

### Monitoring Stack and Server (added the same day)

| Verification Item | Result |
| --- | --- |
| Full converge | `./deploy.sh deploy` → `ok=65 changed=22 failed=0` |
| Idempotency (TC-26) | After fixes, two consecutive reruns both gave `changed=0` |
| 6 services | `winter-{victoriametrics,vmalert,victorialogs,alertmanager,grafana,server}` all active running + enabled at boot |
| Listening interfaces | 8428/8880/9093/9428/3000/8081 all bound only to `127.0.0.1`; only the internal repo on 8082 is exposed externally |
| Local health endpoints | All 6 endpoints return 200 |
| Internal repo | Grafana package and Server binary on 8082 return HEAD 200 |
| Reverse proxy | `/grafana/api/health` and `/api/healthz` both return 200; **the existing site still returns 200 when accessed by IP** |
| Login | Logged in successfully via the systemd instance, `/v1/me` returns admin |

Three issues exposed and fixed on real machines:

1. `unarchive` requires the `dest` directory to **already exist**: Grafana had only created the parent `/opt/winter` → added creation of the install directory;
2. `local_repo` treated the `success` output of every `firewall-cmd --reload` as a change → switched to `--query-rich-rule`, reporting changed only when a rule is actually added;
3. `winter-server`'s `mv` consumed the downloaded source file, causing a fresh `get_url` on every rerun → changed to copying into the target directory + `mv` within the same directory for atomic replacement, preserving the source file.

### Five base-layer issues exposed and fixed on real machines (all invisible on paper, only blowing up at runtime):

1. `deploy/passwords.yml` is not on Ansible's automatic load path → switched to an explicit `include_vars` in pre_tasks;
2. `deploy.sh` had no execute bit after clone → `git update-index --chmod=+x`;
3. PGDG's `pg_isready` is not in the postgres user's PATH → switched to `psql -tAc "select 1"` for liveness checks;
4. nginx being already installed still triggered `dnf`, which hung on an unreachable `pgdg-common` repo → stat first, then decide whether to install;
5. **`group_vars/all.yml` was never loaded** (it is not next to the playbook/inventory directory) → added `vars_files` to both playbooks; and role default dicts were replaced wholesale by group_vars → switched to `*_overrides` + `combine` merging.

> **Provisioning phase vs runtime** (ADR-35): this directory is a **provisioning-phase** tool, using ansible to install Agents, register certificates and distribute configuration.
> The platform holds **no SSH credentials at runtime**; all node operations go through the Agent. Therefore `prechecks` is only for confirmation before installation,
> and prechecks during onboarding and operations run locally via the Agent (implemented in Go, with checks kept consistent with this role).

## Directory Layout

```
deploy/
├── deploy.sh                  # stage dispatcher (aligned with kolla-ansible's CLI shape)
├── ansible.cfg
├── inventory/                 # all-in-one / multinode / pgha
├── group_vars/all.yml          # single configuration entry point (corresponds to kolla's globals.yml)
├── passwords.yml.example      # template for sensitive values; the real passwords.yml is not committed
├── scripts/fetch-artifacts.sh # build packages with internet access (reproducible, pinned versions)
├── artifacts/repo/            # offline artifact repository (committed with the code)
├── playbooks/                 # bootstrap-servers / prechecks / deploy / pg-ha / pg-scale-out / pg-scale-in / monitor-scale-out / monitor-scale-in / ha-setup / scale-out / scale-in / destroy / post-deploy / reconfigure / upgrade
└── roles/
    ├── bootstrap/             # base packages + optional kernel/data-disk settings
    ├── agent/                 # Agent pre-install and removal (scale-out / scale-in / destroy)
    ├── uninstall/             # platform removal with data preserved by default
    ├── prechecks/             # read-only checks
    ├── local_repo/            # publishes the internal artifact repo (8082)
    ├── postgresql/ nginx/     # database and access layer
    ├── etcd/ patroni/ haproxy/# PG high availability
    ├── observability/         # VM/vmalert/VictoriaLogs/Alertmanager/Grafana
    ├── winter-server/         # Go single binary + systemd (multi-replica via ha-setup)
    ├── harbor/                # Harbor replicas (external DB/Redis/NFS + docker-compose)
    ├── keepalived/            # VRRP VIP + LVS/DR virtual_server (Director)
    └── realserver/            # LVS/DR real servers (loopback VIP + ARP suppression)
```

## Quick Start

```bash
# 1. The control machine needs ansible-core (version per the docs BOM convention)
ansible-playbook --version

# 2. Adapt inventory and group_vars/all.yml to the site, and prepare passwords.yml
cp passwords.yml.example passwords.yml && vi passwords.yml

# 3. Base dependencies (run once on every managed host)
./deploy.sh bootstrap-servers -i inventory/multinode

# 4. Read-only prechecks
./deploy.sh prechecks -i inventory/multinode

# 5. Full converge (database + access layer + monitoring stack + Server)
./deploy.sh deploy -i inventory/multinode

# 6. Run again to confirm idempotency (should be changed=0)
./deploy.sh deploy -i inventory/multinode

# 7. Uninstall when needed (data volumes are preserved)
./deploy.sh destroy -i inventory/multinode
```

Static checks before a commit (no ansible-core required):

```bash
sh deploy/scripts/verify-ansible.sh
```

It parses every YAML file, checks handler/notify consistency, enforces the English-only rule, validates Jinja expressions and links, and runs `ansible-playbook --syntax-check` automatically when ansible-core is installed on the control machine.

Available tags: `bootstrap` / `agent` / `scale-out` / `scale-in` / `destroy` / `repo` / `postgresql` / `nginx` / `observability` / `server` / `etcd` / `patroni` / `haproxy` / `db`,
as well as `prechecks.os` / `prechecks.resources` / `prechecks.time` / `prechecks.kernel` / `prechecks.network` / `prechecks.ports` / `prechecks.artifacts` / `prechecks.groups`.

## Design Constraints (Read Before Making Changes)

1. **prechecks must be read-only**: every `command` needs `changed_when: false`; no package installs, no sysctl writes, no firewall changes.
2. **Configuration only in `group_vars/all.yml`**: do not hardcode IPs, ports or paths in playbooks or roles.
3. **No hostname conditionals**: express topology with inventory groups (`management` / `ceph_nodes`); do not use `when: inventory_hostname == ...`.
4. **Failure messages must be actionable**: `fail_msg` must include expected value / actual value / how to fix.
5. **No secrets in the repo**: passwords and secrets go only in `passwords.yml` (excluded by `.gitignore`).
6. **No compilation on target machines**: all Go components are cross-compiled into single binaries under `artifacts/repo/bin/` (ADR-14).

## Relationship to Documentation

- The commands in manual stage C match this document: `docs/07-新兴与稀缺线/20-技术文档工程师/安装部署手册.md`
- Checks and acceptance criteria: `docs/03-测试线/06-测试工程师/测试策略与验收用例.md` (TC-26 requires `prechecks` to leave the system unchanged and `deploy`'s second run to be `changed=0`)
- Source of prerequisites: `docs/04-运维线/09-SRE-DevOps/部署与监控告警方案.md` §1 / §2
