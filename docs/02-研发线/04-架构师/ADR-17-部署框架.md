# ADR-17 · 部署框架：采用 kolla-ansible 风格的 Ansible 编排

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-15 |
| 决策人 | 项目负责人 |
| 影响范围 | 安装手册阶段 C、架构设计 §1.2 / §4.1 / §6.1、ADR-14 阶段 C、ADR-15 ansible 职责、TC-22 / TC-26、AGENTS.md I14 / I16、仓库目录约定 |
| 关联 | [ADR-14 离线交付](ADR-14-离线交付模型.md) · [ADR-15 安装通道](ADR-15-安装通道.md) · [ADR-16 镜像仓库](ADR-16-镜像仓库.md) · [kolla-ansible](https://github.com/openstack/kolla-ansible)（Apache-2.0） |

---

## 1. 背景

部署前检查与管理平台部署都要由 ansible 承载。摆在面前的是两条路：

- **写一堆零散 playbook**：快，但很快会退化成「一堆有 if-else 的脚本」，不可重跑、不可审计、没人敢改；
- **采用成熟框架的约定**：慢一点起步，但结构、命令语义、失败处理都有先例可循。

kolla-ansible 是后者的成熟样本：它把「部署一套多组件容器化平台」拆成固定阶段，用 inventory 表达拓扑、用单一 globals 表达配置、用 role 表达组件。这套结构与我们要做的事高度同构——它自己也管 RabbitMQ、HAProxy、MariaDB、Prometheus、Grafana、OpenSearch。

## 2. 决策

**自建部署框架，结构、命令模型与阶段语义对齐 kolla-ansible；不 fork、不 vendor、不依赖其 Python 包。**

### 2.1 目录结构（对齐 kolla 的约定）

```
deploy/
├── ansible.cfg
├── inventory/
│   ├── all-in-one            # 单机（M0 用）
│   └── multinode             # 分组：management / ceph_nodes / monitor
├── group_vars/
│   └── all.yml               # ≈ globals.yml：唯一配置入口
├── globals.d/                # 可选：按组件拆分覆盖（对齐 kolla 的 globals.d 机制）
├── roles/
│   ├── prechecks/            # 只读校验，不改系统
│   ├── podman/               # 运行时基线
│   ├── registry_harbor/
│   ├── postgresql/
│   ├── rabbitmq/
│   ├── observability/        # VM + vmalert + Alertmanager + Grafana（log 待 D4）
│   ├── platform_server/
│   ├── platform_web/
│   └── platform_agent/
├── playbooks/
│   ├── bootstrap-servers.yml
│   ├── prechecks.yml
│   ├── pull.yml
│   ├── deploy.yml
│   ├── reconfigure.yml
│   ├── post-deploy.yml
│   ├── upgrade.yml
│   └── destroy.yml
└── tools/
    ├── genpwd                 # 对齐 kolla-genpwd：生成 secrets，不落仓库
    └── genconfig
```

### 2.2 命令模型（对齐 kolla-ansible CLI）

```bash
./deploy.sh bootstrap-servers -i inventory/multinode   # 装基础依赖（podman、python、ansible 依赖）
./deploy.sh prechecks          -i inventory/multinode  # 只读校验：磁盘/时间/网络/源可达/端口
./deploy.sh pull               -i inventory/multinode  # 从 Harbor 拉镜像（对应 ADR-16）
./deploy.sh deploy             -i inventory/multinode  # 收敛到目标状态
./deploy.sh post-deploy        -i inventory/multinode  # 初始化：管理员、凭据、Region/AZ
./deploy.sh reconfigure        -i inventory/multinode  # 改配置后重跑
./deploy.sh upgrade            -i inventory/multinode  # 版本升级（N → N+1）
./deploy.sh destroy            -i inventory/multinode  # 销毁（数据卷默认保留，需显式 --volumes 才删）
```

安装手册的「阶段 C」从手工步骤改为**执行这些命令 + 核对预期输出**。

## 3. 明确借用什么

| 借用的约定 | 为什么值得借 |
| --- | --- |
| **分阶段语义**：bootstrap → prechecks → pull → deploy → post-deploy | 每阶段职责单一、可单独重跑；失败时能定位到阶段而不是「装到一半」 |
| **prechecks 只读** | 「部署前检查」必须是只读的，否则检查本身就成了变更，排障时无法信任 |
| **单一配置入口**（`group_vars/all.yml` + `globals.d/` 覆盖） | 避免配置散落在多个 `.env` 与命令行参数里 |
| **secrets 独立文件 + 生成工具** | `passwords.yml` 由 `genpwd` 生成，仓库零明文（对应 I5） |
| **inventory 分组表达拓扑** | 用 `management` / `ceph_nodes` 分组，而不是在 playbook 里写 `when: inventory_hostname == ...` |
| **role 粒度 = 组件** | 一个组件一个 role，升级与替换边界清晰 |
| **`destroy` 显式区分是否删数据卷** | 防止「重装一次数据没了」，也防止「以为删干净了其实没有」 |

## 4. 明确不借用什么

| 不借用 | 原因 |
| --- | --- |
| **不 fork / 不 vendor 其代码** | 它是 OpenStack 专用：24 个服务定义、镜像命名、版本矩阵全绑定 OpenStack；fork 会把 20+ 个用不到的服务与升级包袱一起继承 |
| **不依赖 `kolla-ansible` Python 包与 CLI** | 我们只需要 `ansible-core` + 少量 collections；少一层依赖就少一处离线包体积与版本冲突 |
| **不用 Docker 作为默认运行时** | 已定 podman（ADR-11）；role 内使用 `containers.podman` 系列模块 |
| **不做「Ansible 当编排器」** | Ansible 是配置管理，不是运行时编排器。运行期靠 podman + systemd + 平台自身 Reconciler，Ansible 只负责部署、变更、升级 |
| **不复制其代码片段** | 若日后确需复用，按 Apache-2.0 保留署名与许可声明，并单独提交 |

## 5. 验收标准（可测，写进 TC）

| 标准 | 判定方式 |
| --- | --- |
| prechecks 只读 | 在未初始化主机上执行，应失败并给出明确原因；执行前后系统状态 diff 为空 |
| deploy 幂等 | 连续执行两次，第二次 `changed=0` |
| destroy 可重建 | destroy（默认保留卷）→ deploy，数据仍在；`destroy --volumes` 后重建为干净状态 |
| 配置单一入口 | 仓库内不存在第二份「同名但有差异」的配置来源；`grep` 可验证 |
| 零明文 | 仓库中无密码、密钥、token；全部来自 `passwords.yml`（不入库） |
| 离线可跑 | 断网环境下完整跑通 bootstrap → deploy → post-deploy（TC-22） |

## 6. 风险

| 风险 | 缓解 |
| --- | --- |
| 自建框架退化成「一堆脚本」 | 硬性要求：一个组件一个 role、配置只在 `group_vars`、playbook 里不写 `when: hostname` |
| Ansible 在 500 节点规模下变慢 | 开启 fact 缓存、`serial` 分批、按需 `strategy: free`、用 `--limit` 灰度 |
| 团队不会读 role，排障只能问人 | 每个 role 的 README 写清「失败时看哪里」；手册里每阶段给「预期输出 + 常见失败」 |
| ansible-core / collections 版本漂移 | 版本锁进 BOM，离线包内固化；升级走独立提交 |
| 误以为 Ansible 能兜住运行期一致性 | 运行期一致性靠 Reconciler（ADR-06/08），Ansible 只管「部署时收敛」 |
| 照搬 kolla 的阶段名但语义走样 | 以本 ADR §3 的七条约定为验收项，评审时逐条对照 |

## 7. 什么情况下推翻

- podman 的 quadlet / `kube play` 成熟到可以「声明式 + systemd」自愈，Ansible 只需做 bootstrap → 可缩减为薄封装。
- 引入更专业的部署编排工具（如 GitOps 式控制器）且被证明更适合离线场景。
- 团队无人能维护 Ansible 代码 → 退回「脚本 + 手册」，但必须保留幂等与 prechecks 两条底线。

推翻流程同前：新开 ADR → 本 ADR 标 `Superseded` → 同步安装手册阶段 C、ADR-14/15、架构设计 §1.2/§4.1、TC-22/TC-26、`AGENTS.md` I16。
