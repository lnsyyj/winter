# ADR-58: podman 独立 role

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-26 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-11 技术栈基线](./ADR-11-技术栈基线.md)、[ADR-50 配置项命名统一为软件名](./ADR-50-配置项命名统一为软件名.md)、[ADR-56 预检不做组 gating](./ADR-56-预检不做组校验与组gating.md) |

## 1. 问题

用户指出 `roles/system-checks/tasks/os.yml` 里的 podman 检查不该放在系统检查 role：
"podman 也是单独的软件，需要有单独的 role"。

现状违反"一个软件一个 role"原则：podman 的**检查**在 system-checks 的 os.yml，
**安装**却混在 os_init.packages 的列表里（和 python3/curl/chrony 一起），podman 自己没有 role。

## 2. 决策：新建 roles/podman，安装与检查都归位

| 原位置 | 迁移到 |
| --- | --- |
| `system-checks/tasks/os.yml` 的 podman version/presence 检查 | `roles/podman/tasks/precheck.yml` |
| `os_init_defaults.packages` 中的 `podman` | `roles/podman/tasks/deploy.yml`（dnf install）|

新增 `roles/podman/`：

```
roles/podman/tasks/
├── main.yml        # ADR-42 dispatcher
├── deploy.yml      # dnf 安装 + 版本验证
├── precheck.yml    # 只读检查（二进制存在性 + 版本输出）
└── prechecks.yml   # 兼容旧调用的薄封装
```

接入点：

- `site.yml`：新增 `Container runtime (podman)` play（`hosts: podman` / `tags: [podman]`），
  位于 OS initialization 之后、bootstrap 之前（容器运行时是基础层）。
- `inventory/hosts.ini`：新增 `[podman]` 组，列三台 IP，位置与 site.yml play 顺序一致。
- `deploy.sh`：COMPONENT_TAGS 与帮助文本加入 `podman`。
- `group_vars/all.yml`：新增 `podman_defaults` / `podman` 字典（`enabled`/`package`/`disablerepo`），
  归属索引加 `podman*, podman.* → podman role`。

## 3. 关键实现

- **离线安装**：先 `rpm -qa` 收集已装包，只装缺失项——避免 third-party repo 不可达时
  `dnf` 盲等（与 os-init 相同策略）。`podman.disablerepo` 可排除故障源。
- **幂等**：已安装时 `changed=0`，只报告版本。
- **enabled 门禁**：`deploy.yml` 先 assert `podman.enabled`，与其他 role 一致。
- **只读 precheck**：所有 command 任务 `changed_when: false`，不安装、不写配置。

## 4. 已知取舍

- ADR-11 的"运行时固定 podman，不用 docker"决策不变——本 ADR 只调整职责归属，
  不引入运行时可插拔（那需要单独 ADR）。
- `os_init.packages` 移除 podman 后，若某主机只在 `[os-init]` 不在 `[podman]` 组，
  将不再自动获得 podman——这是有意的显式化（要 podman 就加入 `[podman]` 组）。
- `prechecks.yml` 是对 `precheck.yml` 的薄封装，兼容历史上按 `prechecks.yml` 调用的约定；
  main dispatcher 按 `winter_ops_action=precheck` 走 precheck.yml。

## 5. 验证（2026-11-26）

- 语法检查通过；YAML 解析 66 个顶层键（含 podman）。
- `deploy -t podman`：三台 `failed=0`、`changed=0`（已装 podman-5.8.5，幂等），报告版本正确。
- `prechecks -t podman`：三台 `failed=0`。
- 全量 `prechecks -t prechecks`：三台 `failed=0`，OS 基线白名单检查仍正常。
- system-checks 的 os.yml 中 `prechecks_podman` 引用归零。
