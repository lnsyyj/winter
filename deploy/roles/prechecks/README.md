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
| `tasks/resources.yml` | Management plane 15C/24G/350G; managed node ≥3 data disks | Fail and provide actual values and the source of this project's estimates |
| `tasks/time.yml` | chronyd running, offset ≤1s | Fail; clock drift causes cephadm to fail immediately |
| `tasks/kernel.yml` | overcommit_memory / max_map_count | Report only by default; fail when `enforce_kernel: true` |
| `tasks/network.yml` | Default route, DNS resolution, SELinux status | Fail if no route (outbound connectivity impossible) |
| `tasks/ports.yml` | Ports 80/443 free on management node | Fail and provide a command to find the listener |
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

## Known trade-offs

- **Data disk check defaults to report-only mode**: M0 VMs often have only one disk; strict validation would block the first successful run. For production delivery, set `strict_data_disks: true`.
- **Kernel parameters are report-only by default**: They should be written by the `bootstrap-servers` phase, which is not implemented yet.
- **Artifact reachability is disabled by default**: Before phase C1 brings up the repo, checking it would inevitably fail as a false positive.
