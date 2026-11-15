# ADR-30 · Ceph 镜像由 cephadm 直拉，平台不预拉

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-15 |
| 决策人 | 项目负责人 |
| 影响范围 | 架构设计 §5.9 / §8、ADR-16、任务模板（镜像拉取错误码）、安装手册、TC-36 |
| 关联 | [ADR-16 镜像仓库](ADR-16-镜像仓库.md) · [ADR-17 部署框架](ADR-17-部署框架.md) · [ADR-25 任务状态机](ADR-25-任务状态机.md) |

---

## 1. 决策

| 对象 | 谁拉 | 说明 |
| --- | --- | --- |
| **Ceph 容器镜像** | **cephadm 自己拉** | 平台只配 registry 地址、凭据与 CA，不做预拉、不做本地缓存管理 |
| 管理面组件镜像（Server/PG/Harbor/…） | 由 `deploy` 的 `pull` 阶段拉 | 与 ADR-17 一致；这条不受本 ADR 影响 |

**范围很重要**：本 ADR 只针对「被管 Ceph 集群的容器镜像」。管理面自己那套镜像仍走显式 `pull` 阶段——否则部署失败时无法区分「我们没拉」和「cephadm 没拉」。

## 2. 理由

1. **cephadm 本来就要拉**：bootstrap 与 `orch apply` 都会自行 pull，平台再预拉一遍等于同一份镜像走两条路径。
2. **少一层状态**：不预拉就不需要维护「节点镜像就绪表」，也不用处理本地镜像与仓库不一致。
3. **凭据面更小**：registry 凭据只通过 cephadm 的配置注入，不在节点上额外落一份登录态。

## 3. 必须补上的三件事（否则这个选择会变成坑）

| 问题 | 处理 |
| --- | --- |
| **失败信息埋在 cephadm 日志里** | 任务模板把「镜像拉取」拆成可识别错误：Agent 执行后扫描 cephadm 输出与 `podman events`，命中拉取类错误 → 步骤失败码 `image_pull_failed`，而不是笼统的 `bootstrap_failed` |
| **tag 漂移** | 镜像引用**必须用 digest**（`image@sha256:...`），禁止裸 tag；`clusters.image_digest` 记录实际使用的 digest |
| **拉取很慢被判超时** | `image_pull` 步骤单独设超时（默认 30 分钟），不套用普通步骤的 5 分钟 |

## 4. 前置条件（交付期 ansible / Agent 初始化时完成）

| 条件 | 落点 |
| --- | --- |
| 节点信任 Harbor CA | 写 `/etc/containers/certs.d/<registry>/ca.crt`（ADR-16） |
| cephadm 能登录 registry | 通过 cephadm 的 `mgr/cephadm/registry_*` 配置注入，凭据来自平台加密存储，**不写节点明文文件** |
| 节点可出站到 Harbor 443 | prechecks 的 `check_artifacts` 覆盖管理面侧；节点侧在初始化步骤校验 |
| 禁用不安全选项 | 禁止 `insecure-registry` 与 `--tls-verify=false`（I15） |

## 5. 数据模型

| 对象 | 变更 |
| --- | --- |
| `image_inventory`（新表） | id, image, digest(uk), ceph_version, source(harbor), added_at —— 记录「用过哪些镜像」，用于审计与版本追溯（对应 PRD FR-10） |
| `clusters.image_digest` | 记录该集群当前使用的镜像 digest；升级时变更并写审计 |

## 6. 代价

| 代价 | 说明 |
| --- | --- |
| 无预热能力 | 首次部署受网络与仓库性能影响；大镜像（>1GB）在慢盘上可能数分钟 |
| 重试粒度较粗 | `image_pull_failed` 只能整步重试，不能只重试单个镜像 |
| 版本追溯依赖登记 | 不预拉就没有「本地清单」，必须靠 `image_inventory` + digest 记录来回答「这套集群用的哪个镜像」 |

## 7. 风险

| 风险 | 缓解 |
| --- | --- |
| 错误码识别不全，仍退回 `bootstrap_failed` | 维护一份错误特征表（超时/401/x509/no space），新增特征需回归 |
| 节点磁盘被镜像占满 | prechecks 校验根分区；部署后监控磁盘水位 |
| cephadm 版本行为差异导致识别失效 | 按 `ceph_version` 维护特征表，升级 Ceph 时回归一次 |
| 有人图省事用 `--tls-verify=false` | 违反 I15，评审拒绝；现场问题必须查 CA，不许关校验 |

## 8. 验收（TC-36）

| 场景 | 通过标准 |
| --- | --- |
| 正常拉取 | 部署成功，`clusters.image_digest` 与实际运行镜像一致 |
| Harbor 停机 | 任务失败且错误码为 `image_pull_failed`，不是泛化的 `bootstrap_failed`，日志指向明确 |
| 凭据错误 | 错误码可区分（401）并可指导修 registry 凭据 |
| 超时设置 | 大镜像拉取不被 5 分钟普通超时误杀 |
| digest 固定 | 同一 digest 重复部署结果一致；tag 变更不导致隐式升级 |

## 9. 什么情况下推翻

- 现场网络到 Harbor 极慢、或需要「到货即预热」→ 增加预拉步骤（回到显式 pull 模型）。
- 引入 OCI 镜像加速/缓存节点 → 采集与拉取路径随之调整。

推翻流程同前：新开 ADR → 本 ADR 标 `Superseded` → 同步架构设计 §5.9/§8、ADR-16、任务模板错误码、TC-36。
