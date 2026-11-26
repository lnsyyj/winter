# ADR-54: 禁止 hosts: all，所有 play 用显式主机组

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-26 |
| 决策人 | 项目负责人 |
| 前置 | [ADR-49 组名统一为软件名](./ADR-49-组名统一为软件名.md) |

## 1. 问题

用户指出：`site.yml` 里还有两处 `hosts: all`（System checks、Base dependencies/bootstrap）。
`hosts: all` 的问题是**无法从 inventory 看出会执行哪些节点**——inventory 里没写 `all` 组，
节点的纳入/排除不透明，违背"写哪个 IP 就执行哪个节点"的可控原则。

## 2. 决策：每个 play 指向明确列出 IP 的具名组

- `site.yml`：
  - `System checks` 的 `hosts: all` → `hosts: system-checks`
  - `Base dependencies and OS settings` 的 `hosts: all` → `hosts: bootstrap`
  - 全文件 `hosts: all` 数量归零。
- `inventory/hosts.ini`：新增两个组，与 role 同名，各列出三台 IP：
  ```
  [system-checks]   192.168.101.189 / .197 / .198
  [bootstrap]       192.168.101.189 / .197 / .198
  ```
  放在文件最前面，与 site.yml 前两个 play 对齐。
- 头部注释明确：**只有列入组的 IP 才会被该 play 执行；不列即排除**。

## 3. 关键实现

- `system-checks` / `bootstrap` role 内无 `groups['all']` 依赖，改动不影响逻辑。
- inventory 的 `[组]` 顺序、group_vars 的配置块顺序继续与 site.yml play 顺序一一对应
  （承接 2026-11-26 的顺序对齐工作）。

## 4. 已知取舍

- 新组与原 `all` 语义在当前三节点环境下等价，但**未来新增节点必须显式加进对应组**才会被
  system-checks/bootstrap 覆盖——这是有意的：纳入节点变成显式动作，不再隐式生效。

## 5. 验证（2026-11-26）

- `grep -c 'hosts: all' site.yml` = 0。
- 新组解析：`system-checks`、`bootstrap` 各解析到 3 台。
- `ansible-playbook --syntax-check` 通过。
- 真机 `prechecks -t prechecks`：三台 `ok=15 failed=0`；
  `deploy -t bootstrap`：三台 `ok=4 failed=0`。
