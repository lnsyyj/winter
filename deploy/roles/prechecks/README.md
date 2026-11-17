# prechecks · Pre-deployment checks (read-only)

Corresponds to installation/deployment manual phase C3 and test case TC-26. **This role must not modify the system**: no package installation, no sysctl writes, no firewall changes.

> **Scope (ADR-35)**: This role serves only the **provisioning phase** — run by implementation personnel with ansible to confirm the environment before installation.
> **Runtime** prechecks (managed clusters, routine operations) are executed locally by the Agent (implemented in Go), not via ansible or SSH.
> The **checklist must be identical** on both sides, with contract tests preventing drift: the same test cases are fed to both this role and the Agent's precheck implementation.

## Checks

| File | What it checks | On failure |
| --- | --- | --- |
| `tasks/main.yml` | Group validity (cannot be in both management and ceph_nodes) | Fail immediately, indicating which group the host should belong to |
| `tasks/os.yml` | CentOS Stream 9, podman present | Fail and indicate the baseline is locked (ADR-11) |
| `tasks/resources.yml` | Management plane min vCPU / RAM / free root GB (all configurable); managed node ≥N data disks | Fail in strict mode, warn only in report-only mode; both thresholds and strictness are configurable |
| `tasks/time.yml` | chronyd running, offset ≤1s | Fail; clock drift causes cephadm to fail immediately |
| `tasks/kernel.yml` | overcommit_memory / max_map_count | Report only by default; fail when `enforce_kernel: true` |
| `tasks/network.yml` | Default route, DNS resolution, SELinux status | Fail if no route (outbound connectivity impossible) |
| `tasks/ports.yml` | Required ports free on management node | Fail in strict mode; warn only when `strict_ports: false` (idempotent re-runs) |
| `tasks/artifacts.yml` | Internal repo / Harbor reachable (disabled by default) | Fail and advise completing phase C1 first |

## What to check on failure

1. Read `fail_msg` first — it should contain "expected value / actual value / how to fix". If it only says "check failed", that task needs rewriting.
2. Rerun a single category to avoid being buried by other failures:
   ```bash
   ./deploy.sh prechecks -i inventory/multinode --tags prechecks.time
   ```
3. With multiple nodes, use `--limit` to narrow the scope:
   ```bash
   ./deploy.sh prechecks -i inventory/multinode --limit node02
   ```
4. If still unclear, verify the inventory groups are correct — many "variable undefined" errors actually mean a host landed in the wrong group.

## Variables

All are under `prechecks:` in `deploy/group_vars/all.yml`, not hardcoded in this role. The role's `defaults/main.yml` only provides fallback values.

Resource thresholds are fully tunable:

| Variable | Default | Meaning |
| --- | --- | --- |
| `management_min_vcpus` | 15 | Minimum CPU cores on a management node |
| `management_min_memory_mb` | 24576 | Minimum RAM in MB (24G) |
| `management_min_root_free_gb` | 350 | Minimum free root space in GB |
| `strict_resources` | true | `true` fails below thresholds; `false` warns and continues |
| `strict_ports` | true | `true` fails if a required port is busy; `false` warns (idempotent re-runs) |
| `ceph_node_min_data_disks` | 3 | Minimum OSD data disks per Ceph node |
| `strict_data_disks` | false | `true` fails on too few disks; `false` warns and continues |

For a small lab or test machine, either lower the `management_min_*` values or set `strict_resources: false` to downgrade the resource check to a warning.

The strictness switch can also be overridden on the command line without editing group_vars:

```bash
./deploy.sh prechecks -i inventory/all-in-one -e prechecks_strict_resources=false
```

This works because the check reads `prechecks_strict_resources` (a top-level override) and falls back to `prechecks.strict_resources`.

## Known trade-offs

- **Resource check defaults to strict mode**: A production host must meet the configured management-plane minimums. For a small lab VM, lower the `management_min_*` thresholds or set `strict_resources: false` to warn only.
- **Data disk check defaults to report-only mode**: M0 VMs often have only one disk; strict validation would block the first successful run. For production delivery, set `strict_data_disks: true`.
- **Kernel parameters are report-only by default**: They should be written by the `bootstrap-servers` phase, which is not implemented yet.
- **Artifact reachability is disabled by default**: Before phase C1 brings up the repo, checking it would inevitably fail as a false positive.
