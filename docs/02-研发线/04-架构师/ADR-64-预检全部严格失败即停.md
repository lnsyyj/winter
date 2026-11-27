# ADR-64: 预检全部严格检查，失败即停

- 状态：已确认
- 日期：2026-11-27
- 决策者：架构师 ④
- 回滚锚点：`v0.16-pre-strict-checks`

## 1. 背景

用户要求："所有 check 都要严格检验，不要有告警或跳过，失败立即暂停。"

核查发现 system-checks 里存在三类"非严格"逻辑：

1. **开关可关掉检查**：`strict_resources` / `strict_ports` / `strict_data_disks` / `enforce_kernel` / `check_artifacts`
2. **report-only 告警分支**：资源、端口、数据盘各有 strict/report-only 两条路径，false 时只打 `WARN` 不失败
3. **条件跳过**：`required_hosts: []` 时 DNS 检查整个跳过；artifacts 目标为空时也跳过

"失败立即暂停"方面：`any_errors_fatal: true` 已在全部 41 个 play 上，无需改动。

## 2. 决策（用户选择"严格 + 阈值留配置"）

### 2.1 删除所有开关与告警分支
- 删除 5 个开关：`strict_resources` / `strict_ports` / `strict_data_disks` / `enforce_kernel` / `check_artifacts`
- 删除 3 个 report-only debug 任务（资源/端口/数据盘）
- 所有 `assert` 无条件执行，失败即 `fatal` 并因 `any_errors_fatal` 中止整个 playbook

### 2.2 阈值保留在配置里（用户选项要求）
`group_vars/all.yml` 的 `prechecks` 块保留所有阈值，新增：
- `expected_vm_overcommit_memory: 1`、`expected_vm_max_map_count: 262144`（内核参数期望值，原为 role 内硬编码）
- `artifact_targets: {repo: <url>, registry: <url>}`（制品检查目标，原为 `check_artifacts` 布尔开关）
- `required_hosts` 默认 `["{{ site.domain }}"]`（原来为空会跳过 DNS 检查）

用户按环境调阈值（不是调开关）——测试机可调低 CPU/内存/磁盘/数据盘门槛，生产用 example 里的生产值。

### 2.3 各检查的最终形态

| 检查 | 之前 | 现在 |
|---|---|---|
| OS 基线 | 白名单可配，严格 | 不变，严格 |
| 资源（CPU/内存/磁盘） | strict 开关 + report-only | 无条件 assert |
| 时间偏移 | 严格 | 不变 |
| 内核参数 | `enforce_kernel: false` 默认关 | 无条件 assert，期望值可配 |
| 默认路由 | 严格 | 不变 |
| DNS 解析 | `required_hosts: []` 时跳过 | 断言非空 + 无条件解析，失败即停 |
| 端口占用 | strict 开关 + report-only | 无条件 assert |
| 制品可达性 | `check_artifacts: false` 默认关 | 无条件检查 artifact_targets |
| 数据盘数量 | `strict_data_disks: false` 默认关 | 无条件 assert，阈值可配 |

## 3. 验证（2026-11-27 真机）

逐步触发各类失败，确认 fail-fast 且消息含期望值/实际值：

1. 资源不足：`Host resources below minimum: CPU expected ≥15 cores, got 2; memory expected ≥24576 MB, got 3622 MB; root free expected ≥350 GB, got 45.0 GB`（RC=2，三台 failed=1）
2. 调低阈值后内核不达标：`vm.overcommit_memory: expected 1, got 0; vm.max_map_count: expected ≥262144, got 65530`（RC=2）
3. 调内核后 DNS 不通：`DNS lookup 'winter.local': expected rc=0, got rc=2`（RC=2）
4. 配 hosts 后端口占用：`Port 80: expected free, but it is in use. Listener: ...`（RC=2）
5. 停 nginx 后制品不可达：`Artifact 'repo' ... expected HTTP 200 or 401, got -1`（RC=2）

每个场景下 `skipped=0`、`rescued=0`、`ignored=0`——无跳过无告警。syntax-check 通过。

## 4. 代价

- **幂等重跑会失败**：已部署组件占用 80/443，严格端口检查会拒绝。这是严格模式的直接结果——幂等场景需先停服再跑 prechecks，或按阶段只对未部署主机检查。
- **默认阈值是生产值**（15 核/24G/350G），小机器必须调 `prechecks.management_min_*` 才能通过；example 模板保留生产值。

## 5. 相关

- fail_msg 统一格式见上一轮改动（expected/got + Fix）
- 全部 41 个 play 的 `any_errors_fatal: true` 保证任一主机失败即全局中止
