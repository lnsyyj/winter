# postgresql · Database (convergent)

Corresponds to deployment phase `deploy`. The goal is not "install a PostgreSQL", but **have a database and account usable by the platform on this machine**.

## Two paths

| Machine state | Which path | Behavior |
| --- | --- | --- |
| An instance is already running | Converge | Only confirm the service, create the database/role, verify; **no package installation, no restart, no parameter changes** |
| Nothing installed | Install | Install `postgresql17-server` (when a PGDG repo is available) or `postgresql-server`, run initdb, start the service |

## Key constraints

- **Never delete any existing database or role**; only add the `winter` database and `winter` role.
- **Do not change memory parameters by default**: `ALTER SYSTEM` requires a restart, which would interrupt other services on the same machine (e.g. argus). To tune, explicitly set `postgresql.tune: true`.
- The password is read only from `vault_pg_password` in `deploy/passwords.yml` (excluded by `.gitignore`), **never written into group_vars**.
- If the role already exists, its password is not changed by default (`force_password: false`), to avoid overwriting a password someone changed manually.

## Variables (`group_vars/all.yml`)

| Variable | Default | Notes |
| --- | --- | --- |
| `postgresql.major` | empty (auto-detect) | An already-running instance takes priority |
| `postgresql.db_name` / `db_user` | `winter` / `winter` | Platform database and account |
| `postgresql.tune` | `false` | Whether to tune memory and connection parameters |
| `postgresql.shared_buffers` etc. | see defaults | Only effective when `tune: true` |

## Verification

```bash
# Database and role exist
sudo -u postgres psql -tAc "select datname from pg_database where datname='winter'"
sudo -u postgres psql -tAc "select rolname from pg_roles where rolname='winter'"

# Connect once with the application account (password from passwords.yml)
PGPASSWORD=... psql -h 127.0.0.1 -U winter -d winter -c 'select 1'
```

## Known trade-offs

- The SQL that creates the role/database appears in process arguments (visible in `ps` for a few seconds). Accepted for M0; for production delivery, switch to ansible-vault + `psql -f` reading from stdin to avoid long-lived command lines.
- `listen_addresses` defaults to `127.0.0.1`: sufficient when management plane components are on the same machine; when the Server moves to a different machine, open it up to the internal address.
