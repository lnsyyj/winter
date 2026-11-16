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
  bootstrap-servers    Install base dependencies and OS settings (podman, chrony, sysctl, data disk)
  prechecks            Pre-deployment checks (read-only, implemented)
  pull                 Pull images from Harbor (not implemented)
  deploy               Converge to the target state (internal repo + PostgreSQL + Nginx + observability stack + Server)
  pg-ha                PostgreSQL high availability cluster (etcd + Patroni + HAProxy)
  pg-scale-out         Add replica nodes to the PG HA cluster (odd final etcd count)
  pg-scale-in          Remove replica nodes from the PG HA cluster (data preserved by default)
  monitor-scale-out    Add monitor nodes (Alertmanager HA, Grafana replicas, vmalert notifiers)
  monitor-scale-in     Remove monitor nodes from all references, then tear them down (data preserved)
  scale-out            Pre-install the Agent on new managed nodes (use --limit)
  scale-in             Remove the Agent from managed nodes (use --limit)
  post-deploy          Acceptance checks and deployment manifest
  reconfigure          Validate configuration, then re-converge
  upgrade              Verify artifacts, converge to pinned versions, re-run acceptance
  destroy              Uninstall the platform (data preserved by default)

Examples:
  ./deploy.sh prechecks -i inventory/multinode
  ./deploy.sh prechecks -i inventory/multinode --tags prechecks.time
  ./deploy.sh deploy --tags observability,server   # converge only the monitoring stack and Server (automatically includes the internal repo)
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
    prechecks)
        PLAYBOOK="$ROOT/playbooks/prechecks.yml"
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
    monitor-scale-out)
        PLAYBOOK="$ROOT/playbooks/monitor-scale-out.yml"
        ;;
    monitor-scale-in)
        PLAYBOOK="$ROOT/playbooks/monitor-scale-in.yml"
        ;;
    scale-out)
        PLAYBOOK="$ROOT/playbooks/scale-out.yml"
        ;;
    scale-in)
        PLAYBOOK="$ROOT/playbooks/scale-in.yml"
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

exec ansible-playbook "$PLAYBOOK" "$@"
