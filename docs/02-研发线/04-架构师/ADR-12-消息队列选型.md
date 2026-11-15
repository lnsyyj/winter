# ADR-12 · 消息队列选 RabbitMQ

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-15 |
| 决策人 | 项目负责人 |
| 取代 | [ADR-07](架构设计-Ceph管理平台.md)（NATS JetStream，`Proposed`，未采纳） |
| 影响范围 | 架构设计 §3 / §4 / §5 / §6 / §13、权衡分析 §2 R7 / §8 D1、部署资源估算、API 分片并发约定 |
| 关联 | [ADR-11 技术栈基线](ADR-11-技术栈基线.md) · [权衡分析 B6](架构决策与权衡分析.md) |

---

## 1. 背景

D1 是最后一个卡住任务层实现的基础组件。两个真实候选：

- **NATS JetStream**：单二进制、资源极轻、按 subject 分片天然契合「按集群分流」。
- **RabbitMQ**：团队已有运维经验、语义完备、管理 UI 与 exporter 成熟，但需 Erlang 运行时、资源更重。

之前的推荐值是 JetStream，理由是「少组件」；本次改判的核心是一个更朴素的事实：**消息队列是本平台唯一「不丢任务」的承诺载体，故障成本远高于资源成本。**

## 2. 决策

采用 **RabbitMQ**（3.x 稳定线，部署时锁定具体版本与镜像 digest）。

## 3. 理由

1. **团队能修，比「更轻」更值钱**：队列出问题是深夜事故，能不能定位 flow control、unacked 堆积、磁盘告警，取决于有没有人真的运维过。
2. **语义刚好覆盖需求**：durable queue、persistent message、publisher confirm、manual ack、prefetch、DLX 死信、TTL —— 「不丢 + 可重试 + 死信告警」全部原生支持，不需要自己拼。
3. **排障路径短**：管理 UI 能直接看到队列深度、unacked、消费者、连接；Prometheus exporter 成熟，接进现有监控栈成本低。
4. **离线交付友好**：镜像与依赖关系明确，`podman load` 即可，不引入未知运行时依赖。

## 4. 放弃了什么

- **JetStream 的单二进制与低占用**：RabbitMQ 多一个 Erlang 运行时，内存基线约 2G、镜像更大 —— 当时被视为对「少组件」（G6）目标的让步；**同日 G6 已放宽为「交付可控」（[ADR-13](ADR-13-放宽部署单元约束.md)），故该让步现仅计为交付体积与运维成本**。
- **按 subject 天然的集群分片**：RabbitMQ 侧改用「每集群独立队列 + routing key」模拟，队列数量会随集群数增长，需要命名规范与清理策略。
- **少学一个中间件的机会**：团队若本就不熟 RabbitMQ，本决策的主要收益归零（见 §7 风险）。

## 5. 实现约束（选了 RabbitMQ ≠ 不丢任务）

这些不是建议，是**上线前必须满足的条件**：

| 约束 | 为什么 |
| --- | --- |
| 开启 **publisher confirm** 并等待 ack | 否则 publish 返回成功不代表 broker 已持久化，宕机即丢 |
| **durable queue + persistent message** 同时成立 | 只设其一，重启后队列或消息仍会消失 |
| **manual ack**，禁止 auto-ack | auto-ack 在消费中途崩溃即丢任务 |
| **prefetch 限制**（按集群分片时取 1） | 否则单消费者抢走多集群任务，破坏「同集群串行」的互斥前提 |
| 死信走 **DLX + 独立死信队列 + 告警**，禁止无限 requeue | 无限 requeue 会把 CPU 打满并掩盖真实失败原因 |
| 消息体只带 `task_id`，任务参数存 PG | 大消息体会拖垮队列内存与流控，且不利于审计 |
| 队列命名 `task.{cluster_fsid}.{task_type}`，带 TTL 与清理策略 | 集群下线后队列必须回收，否则长期累积 |
| 幂等仍由 Agent 侧 `idempotency_key` 保证 | MQ 只保证「至少一次」，exactly-once 是幻觉（B6） |

## 6. HA 形态

- **MVP**：单节点（符合 ADR-10），durable queue + 持久化卷。**单节点不承诺不丢**——节点故障时依赖卷恢复，恢复期间无法接收任务。
- **V1**：3 节点集群 + **quorum queue**（`x-queue-type: quorum`）。classic mirrored queue 已废弃，不要使用。

## 7. 风险

| 风险 | 缓解 |
| --- | --- |
| Erlang 运行时安全补丁频繁，升级窗口难安排 | 锁定版本，升级走灰度 + 回归用例；镜像 digest 固定 |
| 内存到达水位（默认 40%）触发 **flow control**，publisher 被阻塞 | 监控 `rabbitmq_alarms_memory_used_watermark`；任务提交超时要有明确错误码，不能静默挂住 |
| 队列数量随集群数线性增长（每集群每任务类型一个队列） | 命名规范 + 生命周期管理，集群删除时级联清理 |
| 「团队熟 RabbitMQ」若只是听过，本决策主要理由失效 | 在 M0 阶段用真实一次故障演练验证（停节点、灌积压、看恢复），不通过则重新评估 |

## 8. 什么情况下推翻

- 团队实际无人能承担 RabbitMQ 运维（含 Erlang 升级与流控排障）。
- 吞吐需求超出单集群 RabbitMQ 合理范围（万级 msg/s 以上）。
- 需要跨地域消息复制或事件溯源能力（那是 Kafka 的场景，届时应重评而非硬扛）。

## 9. 影响

| 项 | 变化 |
| --- | --- |
| D1 | **关闭**，转为本 ADR |
| ADR-07 | `Proposed` → **`Rejected`**（未采纳，改选 RabbitMQ） |
| 部署资源 | 消息队列组件从 1C/1G 调整为 2C/2G/20G，管理面合计 ≈ 11C / 16G / 220G |
| 任务分片 | 由「JetStream subject 分片」改为「每集群独立队列 + routing key」 |
| D2 / D3 / D4 / D5 / D7~D11 | 不变 |

## 10. 推翻流程

同 ADR-11：新开 ADR 说明原因与迁移代价 → 本 ADR 标 `Superseded` → 同步架构设计 §4、§5、§6，权衡分析 §2 R7 与 §8，部署方案资源表，以及 `AGENTS.md` §1。**不直接改写本 ADR 结论。**
