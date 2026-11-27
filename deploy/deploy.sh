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

The action (first argument) selects WHAT to do; -t selects WHICH software/operation.
-t is Ansible's --tags: it matches tags on plays/roles/tasks in site.yml. Two kinds:
  Component tags (26, same name as the role) — choose what to deploy/check:
    system-checks os-init podman bootstrap repo-nginx etcd patroni haproxy postgresql
    nginx victoriametrics vmalert victorialogs alertmanager grafana winter-server
    harbor keepalived lvs-director lvs-realserver named winter-agent
  Operation tags (7, no same-name role) — choose a transactional action:
    postgresql-ha postgresql-scale-out postgresql-scale-in postgresql-migrate
    victoriametrics-scale-out victoriametrics-scale-in application-ha
  Lifecycle tags (4, bound to operations plays): verify reconfigure upgrade uninstall
  Subtags for fine-grained prechecks: system-checks.os / .resources / .time /
    .kernel / .network / .ports / .artifacts
  Combine with commas: -t etcd,postgresql. Omit -t to run all component tags
  (operations are excluded unless explicitly named).

Inventory: one file inventory/hosts.ini. One group per SOFTWARE NAME, IPs listed directly:
  [etcd] [os-init] [postgresql] [nginx] [winter-server] [harbor] [keepalived]
  [named] [victoriametrics] [vmalert] [victorialogs] [alertmanager] [grafana] [repo-nginx]
  [winter-agent] [lvs-director] [lvs-realserver]
  Group name = software running there. Use -t to select components. See ADR-48/49.
  NOTE: hosts are raw IPs, so Patroni/etcd node names become IPs on HA rebuild.

Examples (all tags) — each line says what it checks / deploys:
  # --- prechecks: read-only checks (what is verified) ---
  ./deploy.sh prechecks -t system-checks       # OS baseline / resources+disks / time / kernel / network / ports / artifacts
  ./deploy.sh prechecks -t system-checks.os    # only the OS baseline check (subtags: .resources .time .kernel .network .ports .artifacts)
  ./deploy.sh prechecks -t os-init             # supported distro / config / NTP entries / chrony parent dir
  ./deploy.sh prechecks -t podman              # podman binary present + version
  ./deploy.sh prechecks -t repo-nginx          # artifact dir + SHA256SUMS on control machine / repo port free
  ./deploy.sh prechecks -t postgresql          # distro / password / repo URL / offline RPMs / 5432 free
  ./deploy.sh prechecks -t nginx               # 80/443 free / site parent dirs writable
  ./deploy.sh prechecks -t victoriametrics     # stack enabled / Grafana password / 5 tarballs / component ports
  ./deploy.sh prechecks -t vmalert             # vmalert health endpoint responds (must already be deployed)
  ./deploy.sh prechecks -t victorialogs        # victorialogs health endpoint responds
  ./deploy.sh prechecks -t alertmanager        # alertmanager /-/healthy responds
  ./deploy.sh prechecks -t grafana             # grafana /api/health responds
  ./deploy.sh prechecks -t winter-server       # secrets length / DB addr vs replica count / binary / 8081 free
  ./deploy.sh prechecks -t harbor              # mode / config / credentials / docker-compose / 80 free
  ./deploy.sh prechecks -t keepalived          # VIP / VRRP password / NIC / ip_vs / L2 reachability
  ./deploy.sh prechecks -t named               # DNS topology / master-slave / offline RPMs / 53 free
  ./deploy.sh prechecks -t lvs-director,lvs-realserver  # host is Director / RealServer (role assertion only)
  ./deploy.sh prechecks -t bootstrap,etcd,patroni,haproxy,winter-agent
                                               # no role-specific checks; global system-checks covers OS/ports/resources
  # --- deploy: converge components (listed in dependency order) ---
  ./deploy.sh deploy -t os-init                # base RPMs (python3/curl/wget/chrony) + chrony config
  ./deploy.sh deploy -t podman                 # install podman from OS repo (container runtime)
  ./deploy.sh deploy -t bootstrap              # sysctl profile + br_netfilter + data device
  ./deploy.sh deploy -t repo-nginx             # Nginx repo site on 8082 + SELinux port label
  ./deploy.sh deploy -t etcd                   # etcd/etcdctl binary + 3-node cluster + systemd unit
  ./deploy.sh deploy -t postgresql             # system PG 5432 from offline RPMs (single instance)
  ./deploy.sh deploy -t patroni                # Patroni binary/wheels + config + data dir (PG HA instance)
  ./deploy.sh deploy -t haproxy                # HAProxy RPM + config + 5434 primary route
  ./deploy.sh deploy -t nginx                  # access Nginx 80/443 + self-signed cert + site dirs
  ./deploy.sh deploy -t victoriametrics        # VictoriaMetrics binary + service account + dirs
  ./deploy.sh deploy -t vmalert                # vmalert binary + rules + systemd unit
  ./deploy.sh deploy -t victorialogs           # VictoriaLogs binary + storage + systemd unit
  ./deploy.sh deploy -t alertmanager           # Alertmanager binary + config + systemd unit
  ./deploy.sh deploy -t grafana                # Grafana directory + config/datasource + systemd unit
  ./deploy.sh deploy -t winter-server          # Go binary + server.env + systemd (DB endpoint auto-detected)
  ./deploy.sh deploy -t harbor                 # Harbor offline installer + docker-compose + builtin/external mode
  ./deploy.sh deploy -t keepalived             # Keepalived RPM + VRRP config + VIP failover
  ./deploy.sh deploy -t named                  # BIND 9 RPM + named.conf + zones + firewall
  ./deploy.sh deploy -t winter-agent           # agent binary + CA + config + systemd
  ./deploy.sh deploy -t lvs-director           # Director: ipvs rules + VRRP peers from [lvs-director]
  ./deploy.sh deploy -t lvs-realserver         # RealServer: lo VIP + ARP suppression + sysctl
  ./deploy.sh deploy -t etcd,postgresql,nginx  # multiple components in one run
  # --- operations: transactional, must be opted in with -t ---
  ./deploy.sh deploy -t postgresql-ha          # bootstrap 3-node etcd+Patroni+HAProxy HA cluster
  ./deploy.sh deploy -t postgresql-ha -e '{"postgresql_cluster_overrides":{"allow_single_node_bootstrap":true}}'
                                               # single-node HA bootstrap (1 -> 3 growth path)
  ./deploy.sh deploy -t postgresql-scale-out -e '{"postgresql_scale_overrides":{"nodes":["192.168.101.197","192.168.101.198"]}}'
                                               # add Patroni replicas via etcd learner/promote
  ./deploy.sh deploy -t postgresql-scale-in  -e '{"postgresql_scale_overrides":{"removed_nodes":["192.168.101.198"]}}'
                                               # remove a Patroni replica from the cluster
  ./deploy.sh deploy -t postgresql-migrate     # dump 5432 -> restore HA 5434 + checksum verify + URL switch
  ./deploy.sh deploy -t victoriametrics-scale-out -e '{"victoriametrics_scale_overrides":{"nodes":["<node>"]}}'
                                               # add monitoring replicas (vmalert/Alertmanager/Grafana)
  ./deploy.sh deploy -t victoriametrics-scale-in  -e '{"victoriametrics_scale_overrides":{"removed_nodes":["<node>"]}}'
                                               # remove monitoring replicas
  ./deploy.sh deploy -t application-ha \
      -e '{"keepalived_overrides":{"enabled":true,"vip":"192.168.101.250","interface":"ens34"},"winter_server_ha_overrides":{"database_host":"192.168.101.198","database_endpoint":"haproxy"}}'
                                               # Server replicas + Keepalived VIP failover
  # --- lifecycle tags ---
  ./deploy.sh verify                           # acceptance checks + write deployment manifest
  ./deploy.sh reconfigure                      # validate config (ports/retention) then re-converge
  ./deploy.sh upgrade                          # verify artifacts + converge pinned versions + re-verify
  ./deploy.sh destroy                          # uninstall (data preserved by default) = deploy -t uninstall

Per-operation variables (pass via -e; role defaults hold the baseline):
  named                 -e '{"named_overrides":{"enabled":true},"keepalived_overrides":{"vip":"<VIP>","interface":"<NIC>"}}'
  postgresql-ha          (single-node bootstrap) -e '{"postgresql_cluster_overrides":{"allow_single_node_bootstrap":true}}'
  postgresql-scale-out  -e '{"postgresql_scale_overrides":{"nodes":["<new-node>",...]}}'
  postgresql-scale-in   -e '{"postgresql_scale_overrides":{"removed_nodes":["<node>",...]}}'
  postgresql-migrate    (no extra vars; stop-the-world)
  victoriametrics-scale-out -e '{"victoriametrics_scale_overrides":{"nodes":["<node>",...]}}'
  victoriametrics-scale-in  -e '{"victoriametrics_scale_overrides":{"removed_nodes":["<node>",...]}}'
  application-ha        -e '{"keepalived_overrides":{"enabled":true,"vip":"<VIP>","interface":"<NIC>"},"winter_server_ha_overrides":{"database_host":"<PG-HA-entry>","database_endpoint":"haproxy"}}'
  LVS/DR (app-ha)       add "lvs":{"enabled":true,"scheduler":"rr"} inside keepalived_overrides
  After HA is up        re-run 'deploy -t winter-server' so Server reconnects to 5434

Notes:
  - Omit -t: runs ALL component tags (not operations) on hosts selected by inventory groups.
  - The action maps to winter_ops_action: deploy/converge, prechecks/precheck (read-only), etc.
    -t and the action are independent: 'prechecks -t etcd' runs etcd's precheck tasks only.
  - Override keys are <section>_overrides; role defaults hold the baseline (ADR-63).
  - Old stage names as ACTIONS are gone — use 'deploy -t <tag>' (stage names live on as tags).
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
