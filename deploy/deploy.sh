#!/usr/bin/env sh
# 平台部署阶段分发器（ADR-17：对齐 kolla-ansible 的命令形态）
# 用法：./deploy.sh <stage> [-i inventory] [ansible-playbook 参数...]
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
STAGE=${1:-}

usage() {
    cat <<'EOF'
用法：./deploy.sh <stage> [-i inventory] [--tags tag,...] [ansible 参数...]

阶段：
  prechecks            部署前检查（只读，已实现）
  bootstrap-servers    装基础依赖（未实现）
  pull                 从 Harbor 拉镜像（未实现）
  deploy               收敛到目标状态（未实现）
  post-deploy          初始化（未实现）
  reconfigure          改配置后重跑（未实现）
  upgrade              版本升级（未实现）
  destroy              销毁（未实现）

示例：
  ./deploy.sh prechecks -i inventory/multinode
  ./deploy.sh prechecks -i inventory/multinode --tags prechecks.time
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
    prechecks)
        PLAYBOOK="$ROOT/playbooks/prechecks.yml"
        ;;
    bootstrap-servers|pull|deploy|post-deploy|reconfigure|upgrade|destroy)
        echo "阶段 '$STAGE' 尚未实现（规划见 docs/02-研发线/04-架构师/ADR-17-部署框架.md）。" >&2
        echo "当前可用：prechecks。手工操作临时参考安装部署手册阶段 C。" >&2
        exit 2
        ;;
    *)
        echo "未知阶段：$STAGE" >&2
        usage >&2
        exit 2
        ;;
esac

if ! command -v ansible-playbook >/dev/null 2>&1; then
    echo "未找到 ansible-playbook。请在控制机安装 ansible-core（版本锁进 BOM），或用虚拟环境。" >&2
    exit 3
fi

# 未显式传 -i / --inventory 时，默认 all-in-one
case " $* " in
    *" -i "*|*" -i"*|*" --inventory "*) ;;
    *) set -- "$@" -i "$ROOT/inventory/all-in-one" ;;
esac

exec ansible-playbook "$PLAYBOOK" "$@"
