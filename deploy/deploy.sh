#!/usr/bin/env sh
# winter · single-entry deployment dispatcher (ADR-42).
#
# Every command enters through playbooks/site.yml. The first argument is the ACTION; what actually
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
  Operations:  pg-ha pg-scale-out pg-scale-in pg-migrate
               observability-scale-out observability-scale-in app-ha destroy

Examples:
  ./deploy.sh deploy -i inventory/all-in-one
  ./deploy.sh deploy -i inventory/multinode -t local-repo
  ./deploy.sh prechecks -i inventory/pgha -t postgresql
  ./deploy.sh deploy -i inventory/operations/scale-out-pg -t pg-scale-out \
      -e @group_vars/operations/pg-scale-out.yml
  ./deploy.sh deploy -i inventory/multinode -t app-ha -e @group_vars/operations/app-ha.yml

Notes:
  - Omit -t to run every play selected by the inventory (careful: operations are included).
  - -e @group_vars/operations/<op>.yml supplies per-operation variables; see those files.
  - Old stage names (pg-ha, app-ha as actions, etc.) are gone — use 'deploy -t <tag>'.
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
        # (pg-ha, pg-scale-*, pg-migrate, app-ha, observability-scale-*, destroy) MUST be
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

# No skip-tags needed: every transactional playbook now tags all its tasks with its single
# operation tag (pg-ha, pg-migrate, ...), so -t <component> can never trigger an operation and
# vice versa. The previous skip-tags hack also cancelled -t pg-migrate when extra-vars sat
# between the flag and its value — removed.

# Lifecycle actions keep their own playbooks: their internal task tags overlap with component
# names, so importing them into site.yml would leak into `-t <component>` runs (e.g. upgrade tasks
# tagged 'postgresql' firing on a routine deploy). They enter through deploy.sh but not via site.yml.
case "$ACTION" in
    destroy)    PLAYBOOK="playbooks/destroy.yml" ;;
    verify)     PLAYBOOK="playbooks/post-deploy.yml" ;;
    reconfigure) PLAYBOOK="playbooks/reconfigure.yml" ;;
    upgrade)    PLAYBOOK="playbooks/upgrade.yml" ;;
    *)          PLAYBOOK="playbooks/site.yml" ;;
esac

# Map the CLI action to the role task-file name. 'prechecks' is the user-facing command but
# role task files use singular 'precheck', matching the reference framework's convention.
TASK_ACTION=$ACTION
[ "$ACTION" = "prechecks" ] && TASK_ACTION="precheck"
set -- "$@" -e "winter_ops_action=$TASK_ACTION"

cd "$ROOT"
echo "Running: ansible-playbook $PLAYBOOK -e winter_ops_action=$TASK_ACTION $*"
exec ansible-playbook "$PLAYBOOK" "$@"
