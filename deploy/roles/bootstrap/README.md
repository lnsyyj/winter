# role · bootstrap

Base layer of the ADR-17 stage model. Installs OS packages that the platform needs and applies the settings that `roles/prechecks` only reports.

## What it does

- Installs missing base packages (podman, python3, chrony, curl, wget) from the OS installation media. It queries `rpm -qa` first: on sites with unreachable third-party repos a blind `dnf` call hangs even when everything is already installed.
- Enables `chronyd` (mTLS/certificate checks depend on clock sync).
- Optionally writes `/etc/sysctl.d/99-winter.conf` and loads `br_netfilter`.
- Optionally formats and mounts a data disk. Both steps are opt-in and off by default.

## Configuration

All values live in `group_vars/all.yml` as `bootstrap_overrides`:

```yaml
bootstrap_overrides:
  enforce_kernel: true            # write sysctl profile (keep in sync with prechecks.enforce_kernel)
  data_disk:
    device: /dev/sdb
    format_if_empty: true
    mount: true
```

## Safety

- `enforce_kernel: false` by default during M0, matching `prechecks.enforce_kernel: false`.
- `data_disk.format_if_empty` and `data_disk.mount` are false by default; formatting only runs when the device has no filesystem signature.
- Re-running is idempotent: packages are queried before install, `template` only writes on content change, and `mount` uses `state: mounted`.

## Failure messages

- Unsupported distribution: expected `RedHat` family, actual distribution printed with `ansible_facts.distribution`.
