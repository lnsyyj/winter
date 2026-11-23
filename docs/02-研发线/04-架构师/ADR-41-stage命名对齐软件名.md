# ADR-41 · 部署 stage 命名对齐软件名

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-20 |
| 决策人 | 项目负责人 |
| 关闭 | stage 名与实际部署的软件/role 对不上、看不出部署了哪些组件的问题 |
| 关联 | [ADR-17 部署框架](ADR-17-部署框架.md) · [ADR-16 镜像仓库](ADR-16-镜像仓库.md) |

---

## 1. 问题

`deploy.sh` 的 stage 名沿用了早期抽象命名，与实际部署的软件对不上，运维看 help 无法判断某个 stage 部署了什么：

- `ha-setup` —— 实际部署 winter-server + harbor 多副本、nginx upstream、keepalived VIP/LVS，名字完全看不出软件。
- `monitoring` —— 实际是 observability role（VM/VL/vmalert/Alertmanager/Grafana 五件套），名字与 role 不一致。
- `monitor-scale-out/in` —— 同上，且 "monitor" 与 role 名 "observability" 冲突。
- `scale-out/in` —— 实际只装 winter-agent，看不出是 Agent。
- `server` —— 实际是 winter-server role。
- `repo` —— 实际是 local-repo role（Nginx 制品源）。

## 2. 决策

stage 名统一按**软件名/role 名**命名，并在 help 每一行标注所部署的软件与涉及的 role。完整映射：

| 旧 stage | 新 stage | 对应软件 / role |
| --- | --- | --- |
| `repo` | `local-repo` | local-repo（Nginx 制品源） |
| `server` | `winter-server` | winter-server |
| `monitoring` | `observability` | observability（VM/VL/vmalert/AM/Grafana） |
| `monitor-scale-out` | `observability-scale-out` | observability + nginx |
| `monitor-scale-in` | `observability-scale-in` | observability + nginx |
| `ha-setup` | `application-ha` | winter-server + harbor + nginx + keepalived + realserver |
| `scale-out` | `agent-scale-out` | agent（winter-agent） |
| `scale-in` | `agent-scale-in` | agent（winter-agent） |

未改动的 stage（本就对齐软件/语义）：`postgresql`、`nginx`、`harbor`、`keepalived`、`dns`、`postgresql-ha`、`postgresql-scale-out/in`、`postgresql-migrate`、`deploy`、`prechecks`、`bootstrap-servers`、`system-init`、`post-deploy`、`reconfigure`、`upgrade`、`destroy`。

### 2.1 兼容性

旧 stage 名保留为**别名**：调用时打印 `WARNING: stage 'X' was renamed to 'Y'` 并转发到新 playbook，不出现在 help 中。这样现有运维脚本不会静默失效，同时明确引导迁移到新名。

### 2.2 Ansible tag 名

role 内部的 tag（`server` / `monitoring` / `repo` 等）**未改**——它们是 role 内部收敛粒度，不是 stage 名；改动会破坏大量 `--tags` 调用且无收益。help 与文档里的 stage 引用已全部更新，措辞统一为 "observability stack"。

## 3. 影响

- `deploy/deploy.sh`：help 全量重写（每行标注软件/role）+ case 新名 + 旧名别名分支。
- 8 个 playbook 文件重命名（`repo.yml`→`local-repo.yml` 等），git mv 保留历史。
- `deploy/README.md`、各 role README、相关 playbook 头部注释、ADR-40 引用同步更新。
- AGENTS.md 的变更历史表是历史记录，**不改写**（记录当时使用的名字）。

## 4. 验证

- `sh -n deploy.sh` 通过；新 stage 名全部映射到存在的 playbook。
- `verify-ansible.sh`：94 个 YAML 解析通过、全英文检查通过。
- 真机：`application-ha` / `keepalived` / `dns`（控制面 HA）与 `harbor`（builtin）两条路径按新名执行验证。

## 5. 风险

| 风险 | 缓解 |
| --- | --- |
| 外部脚本硬编码旧 stage 名 | 旧名保留为带告警的别名，不静默失败 |
| 文档遗漏引用 | 全仓 grep 旧名（排除历史 AGENTS.md 与别名分支）应为零 |
| tag 名与 stage 名不一致引起困惑 | help 明确区分 stage 与 role/tag；本 ADR 记录不改名理由 |

## 6. 什么情况下推翻

若后续 stage 数量继续增长导致「软件名=stage 名」粒度不够（例如一个软件有多个编排 stage），可改为 `<software>-<action>` 命名（如 `harbor-scale-out`），届时新开 ADR 并保留别名。
