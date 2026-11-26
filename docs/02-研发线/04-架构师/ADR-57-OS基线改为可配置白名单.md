# ADR-57: OS 基线改为可配置白名单

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-26 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-11 技术栈基线](./ADR-11-技术栈基线.md)、[ADR-56 预检删除组 gating](./ADR-56-预检不做组校验与组gating.md) |

## 1. 问题

用户指出 `roles/system-checks/tasks/os.yml` 的 OS 基线断言写死了 CentOS Stream 9：

```yaml
- ansible_facts.distribution == 'CentOS'
- ansible_facts.distribution_major_version == '9'
- "'CentOS Stream 9' in prechecks_os_release_text"
```

"后续我可能要支持不同的系统，但是这里写死了"。ADR-11 确实把 CentOS Stream 9 锁定为
OS 基线，但"未来支持 Rocky/Alma/RHEL/国产化系统"也在其推翻条件里——代码不该把扩展点写死。

## 2. 决策：白名单配置化

在 `group_vars/all.yml` 新增顶层变量 `prechecks_supported_os`（默认值在
`prechecks_supported_os_defaults`）：

```yaml
prechecks_supported_os_defaults:
  - distribution: CentOS
    major_version: "9"
    pretty_name_contains: "CentOS Stream 9"
prechecks_supported_os: "{{ prechecks_supported_os_defaults }}"
```

匹配规则：

| 字段 | 必填 | 含义 |
| --- | --- | --- |
| `distribution` | 是 | `ansible_facts.distribution`（如 CentOS / Rocky / Ubuntu）|
| `major_version` | 是 | `ansible_facts.distribution_major_version`（字符串比较）|
| `pretty_name_contains` | 否 | 进一步从 `/etc/os-release` 的 PRETTY_NAME  narrowing（区分 CentOS Stream 与 CentOS Linux）|

主机匹配白名单中**任一条目**即通过；白名单为空则跳过检查。

新增基线只需两种方式之一，**不改 role 代码**：

1. 改文件：编辑 `prechecks_supported_os_defaults` 加条目。
2. 运行时覆盖：`-e '{"prechecks_supported_os":[...]}'`。

## 3. 关键实现

- 用**顶层变量**而非 `prechecks.supported_os`：`-e '{"prechecks":{...}}'` 会整体替换
  `prechecks` 字典导致其他阈值丢失；顶层变量可被 `-e` 单独覆盖，不影响其他预检配置。
- 分两个 `set_fact`：先匹配无 `pretty_name_contains` 的条目，再匹配有该字段的条目，
  合并后 assert 判断长度。避免 Jinja `or/and` 串联过长难维护。
- `fail_msg` 打印实际 distribution/version/PRETTY_NAME，并提示去哪里加白名单条目——
  actionable，不是裸 assert 失败。

## 4. 已知取舍

- ADR-11 的"锁定 CentOS Stream 9"**决策本身不变**：默认白名单仍只有 CentOS Stream 9。
  本文只提供扩展点；新增基线仍需走 ADR 修订（ADR-11 §7 推翻流程）后把条目加进白名单。
- podman 固定不变（仍按 ADR-11），本文只放开 OS 识别。
- 白名单为空 = 跳过 OS 检查，是显式逃生口（如全新 OS 尚未纳入白名单时先跑其他检查）。

## 5. 验证（2026-11-26）

| 场景 | 命令 | 结果 |
| --- | --- | --- |
| 默认白名单 | `prechecks -t prechecks` | RC=0，三台 `failed=0` |
| `-e` 加 Rocky 9 | `prechecks ... -e '{"prechecks_supported_os":[CentOS9,Rocky9]}'` | RC=0，通过 |
| 白名单不匹配 | `prechecks ... -e '{"prechecks_supported_os":[Rocky9]}'` | RC=2，报错含实际 OS 与配置指引 |
| 语法检查 | `ansible-playbook --syntax-check` | 通过 |
