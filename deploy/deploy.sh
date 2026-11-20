#!/usr/bin/env sh
# Platform deployment stage dispatcher (ADR-17: aligned with kolla-ansible's CLI shape)
# Usage: ./deploy.sh <stage> [-i inventory] [ansible-playbook arguments...]
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
STAGE=${1:-}

usage() {
    cat <<'EOF'
Usage: ./deploy.sh <stage> [-i inventory] [--tags tag,...] [ansible arguments...]

Stages:
  bootstrap-servers        Install base dependencies and OS settings (system-init + bootstrap roles)
  system-init              Install base OS tools and enable chrony only (system-init role, all hosts)
  prechecks                Pre-deployment checks, read-only (prechecks role)
  local-repo               Internal artifact repository on Nginx (local_repo role)
  postgresql               PostgreSQL single instance (postgresql role; use pg-ha for HA)
  nginx                    Nginx access layer (nginx role)
  observability            VictoriaMetrics + VictoriaLogs + vmalert + Alertmanager + Grafana (observability role)
  winter-server            Management Server Go binary (winter-server role)
  harbor                   Harbor registry (harbor role; builtin or external mode, ADR-16)
  keepalived               Keepalived Director + LVS real servers (keepalived + realserver roles)
  dns                      BIND 9 DNS, multi-VIP round-robin (dns role)
  pull                     Pull images from Harbor (not implemented)
  deploy                   Converge all of: local-repo, postgresql, nginx, observability, winter-server
  pg-ha                    PostgreSQL HA cluster: etcd + Patroni + HAProxy (postgresql/etcd/patroni/haproxy roles)
  pg-scale-out             Add PG replica nodes; final etcd count must be odd (same four PG roles)
  pg-scale-in              Remove PG replica nodes; data preserved by default (same four PG roles)
  pg-migrate               Migrate single-node PostgreSQL data into the HA cluster (stop-the-world, ADR-40)
  observability-scale-out  Add monitor nodes: Alertmanager HA + Grafana replicas + vmalert notifiers (observability + nginx)
  observability-scale-in   Remove monitor nodes from all references, then tear down (data preserved)
  app-ha                   App-layer HA: winter-server + harbor replicas behind nginx upstream + keepalived VIP/LVS
  agent-scale-out          Pre-install the winter-agent on managed nodes (agent role, use --limit)
  agent-scale-in           Remove the winter-agent from managed nodes (agent role, use --limit)
  post-deploy              Acceptance checks and deployment manifest
  reconfigure              Validate configuration, then re-converge
  upgrade                  Verify artifacts, converge to pinned versions, re-run acceptance
  destroy                  Uninstall the platform (data preserved by default)

Examples:
  ./deploy.sh prechecks -i inventory/multinode
  ./deploy.sh prechecks -i inventory/multinode --tags prechecks.time
  ./deploy.sh deploy --tags observability,winter-server   # converge only the observability stack and Server (includes local-repo)
  ./deploy.sh winter-server -i inventory/multinode        # deploy a single component (prechecks run first)
  ./deploy.sh dns -i inventory/multinode --tags precheck.dns   # run only the DNS prechecks
EOF
}

if [ -z "$STAGE" ]; then
    usage
    exit 2
fi
shift

case "$STAGE" in
    -h|--help|help)
        usage
        exit 0
        ;;
esac

case "$STAGE" in
    bootstrap-servers)
        PLAYBOOK="$ROOT/playbooks/bootstrap-servers.yml"
        ;;
    system-init)
        PLAYBOOK="$ROOT/playbooks/system-init.yml"
        ;;
    prechecks)
        PLAYBOOK="$ROOT/playbooks/prechecks.yml"
        ;;
    local-repo)
        PLAYBOOK="$ROOT/playbooks/local-repo.yml"
        ;;
    postgresql)
        PLAYBOOK="$ROOT/playbooks/postgresql.yml"
        ;;
    nginx)
        PLAYBOOK="$ROOT/playbooks/nginx.yml"
        ;;
    observability)
        PLAYBOOK="$ROOT/playbooks/observability.yml"
        ;;
    winter-server)
        PLAYBOOK="$ROOT/playbooks/winter-server.yml"
        ;;
    harbor)
        PLAYBOOK="$ROOT/playbooks/harbor.yml"
        ;;
    keepalived)
        PLAYBOOK="$ROOT/playbooks/keepalived.yml"
        ;;
    dns)
        PLAYBOOK="$ROOT/playbooks/dns.yml"
        ;;
    deploy)
        PLAYBOOK="$ROOT/playbooks/deploy.yml"
        ;;
    pg-ha)
        PLAYBOOK="$ROOT/playbooks/pg-ha.yml"
        ;;
    pg-scale-out)
        PLAYBOOK="$ROOT/playbooks/pg-scale-out.yml"
        ;;
    pg-scale-in)
        PLAYBOOK="$ROOT/playbooks/pg-scale-in.yml"
        ;;
    pg-migrate)
        PLAYBOOK="$ROOT/playbooks/pg-migrate.yml"
        ;;
    observability-scale-out)
        PLAYBOOK="$ROOT/playbooks/observability-scale-out.yml"
        ;;
    observability-scale-in)
        PLAYBOOK="$ROOT/playbooks/observability-scale-in.yml"
        ;;
    app-ha)
        PLAYBOOK="$ROOT/playbooks/app-ha.yml"
        ;;
    agent-scale-out)
        PLAYBOOK="$ROOT/playbooks/agent-scale-out.yml"
        ;;
    agent-scale-in)
        PLAYBOOK="$ROOT/playbooks/agent-scale-in.yml"
        ;;
    destroy)
        PLAYBOOK="$ROOT/playbooks/destroy.yml"
        ;;
    post-deploy)
        PLAYBOOK="$ROOT/playbooks/post-deploy.yml"
        ;;
    reconfigure)
        PLAYBOOK="$ROOT/playbooks/reconfigure.yml"
        ;;
    upgrade)
        PLAYBOOK="$ROOT/playbooks/upgrade.yml"
        ;;
    # Legacy stage names (renamed to match software/role names). Kept as aliases with a warning so
    # existing operator scripts keep working during migration; not shown in usage.
    repo|monitoring|server|monitor-scale-out|monitor-scale-in|ha-setup|scale-out|scale-in)
        case "$STAGE" in
            repo) NEW=local-repo ;;
            monitoring) NEW=observability ;;
            server) NEW=winter-server ;;
            monitor-scale-out) NEW=observability-scale-out ;;
            monitor-scale-in) NEW=observability-scale-in ;;
            ha-setup) NEW=app-ha ;;
            scale-out) NEW=agent-scale-out ;;
            scale-in) NEW=agent-scale-in ;;
        esac
        echo "WARNING: stage '$STAGE' was renamed to '$NEW'; update your scripts (alias still works)." >&2
        PLAYBOOK="$ROOT/playbooks/$NEW.yml"
        ;;
    pull)
        echo "Stage '$STAGE' is not implemented yet (see the plan in docs/02-研发线/04-架构师/ADR-17-部署框架.md)." >&2
        echo "Currently available: prechecks. For manual operations, temporarily refer to stage C of the installation and deployment manual." >&2
        exit 2
        ;;
    *)
        echo "Unknown stage: $STAGE" >&2
        usage >&2
        exit 2
        ;;
esac

if ! command -v ansible-playbook >/dev/null 2>&1; then
    echo "ansible-playbook not found. Install ansible-core on the control machine (version pinned in the BOM), or use a virtual environment." >&2
    exit 3
fi

# Default to all-in-one when -i / --inventory is not explicitly passed
case " $* " in
    *" -i "*|*" -i"*|*" --inventory "*) ;;
    *) set -- "$@" -i "$ROOT/inventory/all-in-one" ;;
esac

# Run from the deploy directory so ansible.cfg (roles_path, default inventory) is honoured
# even when the dispatcher is invoked from the repository root.
cd "$ROOT"
exec ansible-playbook "$PLAYBOOK" "$@"
