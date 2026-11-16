# role · bootstrap

Base layer of the ADR-17 stage model. Applies the kernel and data-disk settings that `roles/prechecks` only reports; base packages and clock synchronization are handled by `roles/system-init`.

## What it does

- Base packages and clock synchronization were extracted into `roles/system-init`, a single-purpose
  role that can also run on its own. The `bootstrap-servers` stage runs it first; configure it with
  `system_init_overrides` in `group_vars/all.yml`.
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
- Re-running is idempotent: `template` only writes on content change, `modprobe` is a no-op when already loaded, and `mount` uses `state: mounted`.

## Failure messages

- Unsupported distribution: expected `RedHat` family, actual distribution printed with `ansible_facts.distribution`.
