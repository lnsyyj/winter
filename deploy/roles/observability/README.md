# role: observability

Deploys the observability stack of ADR-18. All components are **static binaries**, fetched from the internal Nginx artifact repository (`local-repo`);
the target machine does not compile or connect to the external network (ADR-14 / ADR-15).

## Components and default ports

| Component | Binary | Port | Health endpoint | Description |
| --- | --- | --- | --- | --- |
| VictoriaMetrics | `victoria-metrics-prod` | 8428 | `/health` | Metrics, retained for `monitoring_retention_days` days |
| vmalert | `vmalert-prod` | 8880 | `/health` | The **only** alert rule pipeline; rules directory `config_dir/rules/*.yml` |
| VictoriaLogs | `victoria-logs-prod` | 9428 | `/health` | Logs, retained for 30 days; audit is in PG, not here |
| Alertmanager | `alertmanager` | 9093 | `/-/healthy` | webhook feeds back to the platform (ADR-33) |
| Grafana | Directory-based install | 3000 | `/api/health` | Only via the Nginx `/grafana/` reverse proxy (ADR-09) |

Ports, versions, retention and command-line arguments are all changed in `deploy/group_vars/all.yml` under
`monitoring_ports` / `observability.components`; this role does not hardcode ports.
All processes listen only on `monitoring_listen_addr` (default `127.0.0.1`) in single-node mode, run as the
non-root account `winter-monitor`, and their systemd units are named `winter-<component>`. The monitoring scale
stages override the bind address with `0.0.0.0` and advertise each node's internal address; see below.

## Configuration and tailoring

- `observability.enabled: false` skips the whole stack (playbook-level `when`).
- Setting a single component's `enabled: false` trims it, corresponding to ADR-18's "packaging layers".
- `monitoring_retention_days` applies to both metrics and logs; audit retention is a separate setting (ADR-32).
- `vault_grafana_admin_password` must come from `deploy/passwords.yml`; the role asserts this explicitly
  and will not generate a default weak password.
- vmalert's `rules/platform.yml` is an M0 placeholder rule (never true); after collection is connected, add
  the "VictoriaLogs disk watermark" alert per ADR-18 and the "queue depth / oldest task age" alert per ADR-36.
- Alertmanager's webhook points to the Server's `/v1/alerts/webhook`; the M0 endpoint is not yet implemented;
  alerts may be dropped, audit must not be; an unavailable endpoint does not block jobs.

## Idempotent design

1. The extraction marker file includes the tarball name: rerunning the same version does not re-extract, changing version automatically re-extracts;
2. Compare with `cmp` before installing binaries; do not overwrite if content is unchanged;
3. Only config/unit changes trigger a restart via handler;
4. The second run gives `changed=0` (the `uri` health check is read-only).

## Artifacts

The 5 packages under `deploy/artifacts/repo/tarball/` are produced by `deploy/scripts/fetch-artifacts.sh`,
with `SHA256SUMS.txt` for transfer verification. Note two points:

- `vmalert` is not officially released on its own; the script extracts `vmalert-prod` from the full `vmutils`
  toolkit and **repackages** it, avoiding bringing the 140MB toolkit into the repository; the original version is recorded in the BOM.
- Grafana 9.5.20 is chosen because it is the last stable release whose tar.gz is under GitHub's 100MB single-file limit;
  upgrading to 11.x+ requires switching to sharding or transport media.

## Scale-out / scale-in (ADR-18 / ADR-33)

This role is reused by the two monitoring scale stages. Both stages are idempotent and keep single-node
behaviour unchanged.

- `./deploy.sh observability-scale-out`: converge the stack on all `[observability]` nodes, form an Alertmanager gossip
  cluster (`monitoring_scale.cluster_port`, default 9094), point every vmalert instance at all Alertmanager
  replicas, and publish all Grafana replicas to the Nginx upstream.
- `./deploy.sh observability-scale-in`: rewire the remaining nodes first, then run `tasks/remove.yml` on the
  removed nodes, and finally drop their Grafana upstream entries.

Multi-node runs bind components to `0.0.0.0` and advertise each node's `ansible_host`. Scaling back to a
single node restores the localhost-only binding. At least one monitor node must remain.

**Boundary**: VictoriaMetrics and VictoriaLogs use the single-node binaries with local storage. Adding a monitor
node does **not** create a distributed datastore. Set `monitoring_scale.metrics_query_url` to a central
VictoriaMetrics endpoint for aggregation; `remote_write` is never configured implicitly. Data is preserved by
default; `monitoring_scale.purge_data: true` requires a non-empty `monitoring_scale.data_dirs` list.

## Common commands

```bash
./deploy.sh deploy --tags observability      # Converge only the observability stack (automatically includes the artifact repository)
systemctl status 'winter-*'                 # View all components on the target machine
journalctl -u winter-grafana -f
curl -s http://127.0.0.1:8428/health
```
