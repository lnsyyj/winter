# role: system-init

Single-purpose system initialization (ADR-17): installs the base OS tools and enables clock
synchronization with chrony. It was extracted from `roles/bootstrap` so it can be deployed on its
own; `bootstrap-servers` reuses this role and keeps its original behaviour.

## What it does

- Installs missing base packages (`podman`, `python3`, `python3-pip`, `curl`, `wget`, `chrony`)
  from the OS installation media. It queries `rpm -qa` first: on sites with unreachable third-party
  repositories a blind `dnf` call hangs even when every package is already present.
- Enables and starts `chronyd` (mTLS and certificate checks depend on clock sync).
- Optionally adds extra NTP sources via an include file. Existing `chrony.conf` content is never
  overwritten: one `include` line is appended idempotently and a drop-in is rendered under
  `/etc/chrony.d/`.
- Reports `chronyc tracking` after convergence; initial synchronization may take a few seconds.

## Configuration

All values live in `group_vars/all.yml` as `system_init_overrides` (write only differences):

```yaml
system_init_overrides:
  packages: [podman, python3, python3-pip, curl, wget, chrony]
  chrony:
    enabled: true
    servers: [time.corp.local, 10.0.0.1]
```

`servers: []` (the default) keeps the distribution's own pool configuration. Non-empty `servers`
adds `server <name> iburst` lines to the include file.

## Prechecks

Read-only and idempotent:

```bash
./deploy.sh system-init -i inventory/multinode --tags precheck.system-init
```

Checks: supported distribution, package and chrony configuration validity, NTP server token
format, chrony config parent directory and the chronyd unit status (reported, not required before
installation). Every command task uses `changed_when: false`.

## Standalone use

```bash
./deploy.sh system-init -i inventory/multinode
./deploy.sh bootstrap-servers -i inventory/multinode   # still runs system-init first
```

Target hosts come from `[system-init]`, which defaults to all management hosts in the
shipped inventories. Bootstrap-only concerns (kernel sysctl and data disk) remain in `roles/bootstrap`.
