#!/usr/bin/env sh
# winter · single-entry deployment dispatcher (ADR-42).
#
# Every command enters through site.yml. The first argument is the ACTION; what actually
# runs is decided by tags (component/operation) and the inventory groups enabled. This mirrors the
# reference ops framework: one playbook, actions passed as a variable, targets selected by inventory.
#
# Usage: ./deploy.sh <action> [-i inventory] [-t tag,...] [-e @vars.yml] [ansible arguments...]
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ACTION=${1:-}

usage() {
    cat <<'EOF'
Usage: ./deploy.sh <action> [-i inventory] [-t tag,...] [-e @vars.yml] [ansible arguments...]

Actions (first argument, becomes winter_ops_action):
  deploy        Converge the selected components/operation to their target state
  prechecks     Read-only pre-deployment checks for the selected components
  destroy       Uninstall the platform (data preserved by default)
  verify        Acceptance checks and deployment manifest
  reconfigure   Validate configuration then re-converge
  upgrade       Verify artifacts, converge to pinned versions, re-run acceptance

Targets are selected by -t tags and inventory groups. All available tags:
  Components:  prechecks system-init bootstrap local-repo postgresql nginx observability
               winter-server harbor keepalived named agent
  Operations:  postgresql-ha postgresql-scale-out postgresql-scale-in postgresql-migrate
               observability-scale-out observability-scale-in application-ha destroy

Examples:
  ./deploy.sh deploy -i inventory/all-in-one
  ./deploy.sh deploy -i inventory/multinode -t local-repo
  ./deploy.sh prechecks -i inventory/pgha -t postgresql
  ./deploy.sh deploy -i inventory/operations/scale-out-postgresql -t postgresql-scale-out \
      -e @group_vars/operations/postgresql-scale-out.yml
  ./deploy.sh deploy -i inventory/multinode -t application-ha -e @group_vars/operations/application-ha.yml

Notes:
  - Omit -t to run every play selected by the inventory (careful: operations are included).
  - -e @group_vars/operations/<op>.yml supplies per-operation variables; see those files.
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

# Default to the all-in-one topology when no inventory was passed.
case " $* " in
    *" -i "*|*" -i"*|*" --inventory "*) ;;
    *) set -- "$@" -i "$ROOT/inventory/all-in-one" ;;
esac

# Prechecks and destroy must not trigger the transactional operation playbooks. If the caller did
# not pass an explicit -t, restrict to the component tags for those actions.
COMPONENT_TAGS="prechecks,system-init,bootstrap,local-repo,postgresql,nginx,observability,winter-server,harbor,keepalived,named,agent"
ACTION_TAGS=""
case "$ACTION" in
    deploy|prechecks)
        # Without explicit -t, converge/check components only. Transactional operations
        # (postgresql-ha, postgresql-scale-*, postgresql-migrate, application-ha, observability-scale-*, destroy) MUST be
        # opted into with -t — running them on a plain 'deploy' would be destructive.
        ACTION_TAGS="$COMPONENT_TAGS"
        ;;
    destroy)
        ACTION_TAGS="destroy"
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
