#!/usr/bin/env sh
# winter · single-entry deployment dispatcher (ADR-42).
#
# Every command enters through site.yml. The first argument is the ACTION; what actually
# runs is decided by tags (component/operation) and the inventory groups enabled. This mirrors the
# reference ops framework: one playbook, actions passed as a variable, targets selected by inventory.
#
# Usage: ./deploy.sh <action> [-i inventory] [-t tag,...] [-e '{"key":value}'] [ansible arguments...]
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ACTION=${1:-}

usage() {
    cat <<'EOF'
Usage: ./deploy.sh <action> [-i inventory] [-t tag,...] [-e '{"key":value}'] [ansible arguments...]

Actions (first argument, becomes winter_ops_action):
  deploy        Converge the selected components/operation to their target state
  prechecks     Read-only pre-deployment checks for the selected components
  destroy       Uninstall the platform (data preserved by default)
  verify        Acceptance checks and deployment manifest
  reconfigure   Validate configuration then re-converge
  upgrade       Verify artifacts, converge to pinned versions, re-run acceptance

Targets are selected by -t tags and inventory groups. All available tags:
  Components:  prechecks os-init podman bootstrap repo-nginx etcd postgresql nginx victoriametrics
               winter-server harbor keepalived named winter-agent
  Operations:  postgresql-ha postgresql-scale-out postgresql-scale-in postgresql-migrate
               victoriametrics-scale-out victoriametrics-scale-in application-ha uninstall

Inventory: one file inventory/hosts.ini. One group per SOFTWARE NAME, IPs listed directly:
  [etcd] [os-init] [postgresql] [nginx] [winter-server] [harbor] [keepalived]
  [named] [victoriametrics] [vmalert] [victorialogs] [alertmanager] [grafana] [repo-nginx]
  [winter-agent] [lvs-director] [lvs-realserver]
  Group name = software running there. Use -t to select components. See ADR-48/49.
  NOTE: hosts are raw IPs, so Patroni/etcd node names become IPs on HA rebuild.

Examples:
  ./deploy.sh deploy                                               # default inventory
  ./deploy.sh deploy -t repo-nginx
  ./deploy.sh prechecks -t postgresql
  ./deploy.sh deploy -t postgresql-scale-out \
      -e '{"pg_scale_overrides":{"nodes":["192.168.101.197","192.168.101.198"]}}'
  ./deploy.sh deploy -t lvs-director,lvs-realserver \
      -e '{"ha_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34"},"winter_server_ha_overrides":{"database_host":"192.168.101.189","database_endpoint":"haproxy"}}'
  ./deploy.sh deploy -t application-ha \
      -e '{"ha_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34"},"winter_server_ha_overrides":{"database_host":"192.168.101.189","database_endpoint":"haproxy"}}'

Per-operation variables (pass via -e; all.yml holds only defaults):
  named              -e '{"named_overrides":{"enabled":true},"ha_overrides":{"vip":"<VIP>","interface":"<NIC>"}}'
  postgresql-ha      (single-node bootstrap) -e '{"pg_cluster_overrides":{"allow_single_node_bootstrap":true}}'
  postgresql-scale-out  -e '{"pg_scale_overrides":{"nodes":["<new-node>",...]}}'
  postgresql-scale-in  -e '{"pg_scale_overrides":{"removed_nodes":["<node>",...]}}'
  postgresql-migrate (no extra vars; stop-the-world)
  victoriametrics-scale-out -e '{"victoriametrics_scale_overrides":{"nodes":["<node>",...]}}'
  victoriametrics-scale-in  -e '{"victoriametrics_scale_overrides":{"removed_nodes":["<node>",...]}}'
  application-ha     -e '{"ha_overrides":{"enabled":true,"vip":"<VIP>","interface":"<NIC>"},"winter_server_ha_overrides":{"database_host":"<PG-HA-entry>","database_endpoint":"haproxy"}}'
  LVS/DR (app-ha)    add "lvs":{"enabled":true,"scheduler":"rr"} inside ha_overrides

Notes:
  - Omit -t to run every play selected by the inventory (careful: operations are included).
  - Override keys are <section>_overrides; all.yml merges them onto <section>_defaults.
  - Old stage names (postgresql-ha, application-ha as actions, etc.) are gone — use 'deploy -t <tag>'.
EOF
}

case "$ACTION" in
    -h|--help|help|"")
        usage
        [ -n "$ACTION" ] && exit 0 || exit 2
        ;;
    deploy|prechecks|destroy|verify|reconfigure|upgrade)
        ;;
    *)
        echo "Unknown action: $ACTION (expected deploy|prechecks|destroy|verify|reconfigure|upgrade)" >&2
        usage >&2
        exit 2
        ;;
esac

# Drop the action argument — everything after it is ansible-playbook options.
shift

if ! command -v ansible-playbook >/dev/null 2>&1; then
    echo "ansible-playbook not found. Install ansible-core on the control machine." >&2
    exit 3
fi

# Default to the single inventory file when no -i was passed (ADR-48: flat groups, no topologies).
case " $* " in
    *" -i "*|*" -i"*|*" --inventory "*) ;;
    *) set -- "$@" -i "$ROOT/inventory/hosts.ini" ;;
esac

# Prechecks and destroy must not trigger the transactional operation playbooks. If the caller did
# not pass an explicit -t, restrict to the component tags for those actions.
COMPONENT_TAGS="system-checks,os-init,podman,bootstrap,repo-nginx,etcd,patroni,haproxy,postgresql,nginx,victoriametrics,vmalert,victorialogs,alertmanager,grafana,winter-server,harbor,keepalived,named,winter-agent"
ACTION_TAGS=""
case "$ACTION" in
    deploy|prechecks)
        # Without explicit -t, converge/check components only. Transactional operations
        # (postgresql-ha, postgresql-scale-*, postgresql-migrate, application-ha, victoriametrics-scale-*, destroy) MUST be
        # opted into with -t — running them on a plain 'deploy' would be destructive.
        ACTION_TAGS="$COMPONENT_TAGS"
        ;;
    destroy)
        ACTION_TAGS="uninstall"
        ;;
    verify|reconfigure|upgrade)
        ACTION_TAGS="$ACTION"
        ;;
esac
if [ -n "$ACTION_TAGS" ]; then
    case " $* " in
        *" -t "*|*" --tags "*) ;;   # caller supplied tags: respect them
        *) set -- "$@" -t "$ACTION_TAGS" ;;
    esac
fi

# ADR-43: ALL actions — including lifecycle (destroy/verify/reconfigure/upgrade) — now enter
# through site.yml. Every operation's tasks live in roles/<name>/tasks/*.yml with their own
# dedicated tag, so -t <component> can never trigger an operation and vice versa. No skip-tags
# hack and no separate lifecycle playbooks are needed anymore.

# Map the CLI action to the role task-file name. 'prechecks' is the user-facing command but
# role task files use singular 'precheck', matching the reference framework's convention.
TASK_ACTION=$ACTION
[ "$ACTION" = "prechecks" ] && TASK_ACTION="precheck"
set -- "$@" -e "winter_ops_action=$TASK_ACTION"

cd "$ROOT"
echo "Running: ansible-playbook site.yml -e winter_ops_action=$TASK_ACTION $*"
exec ansible-playbook site.yml "$@"
