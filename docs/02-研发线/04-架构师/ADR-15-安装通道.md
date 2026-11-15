# ADR-15 · 安装通道：Nginx 内网源 + ansible 批量安装

| 项目 | 内容 |
| --- | --- |
| 状态 | **Accepted（已确认）** |
| 决策日期 | 2026-11-15 |
| 决策人 | 项目负责人 |
| 影响范围 | [ADR-14](ADR-14-离线交付模型.md) 阶段 C、安装部署手册 §5、PRD FR-10、D3 候选、D6、TC-22、AGENTS.md I14 |
| 关联 | [ADR-14 离线交付模型](ADR-14-离线交付模型.md) · [ADR-11 技术栈](ADR-11-技术栈基线.md) · [ADR-03 Agent 拉模式](架构设计-Ceph管理平台.md) |

---

## 1. 背景

ADR-14 定了三段式（制包 → 搬运 → 远程安装），但**没说「远程安装」具体怎么把包送到节点上**。三种做法：

| 做法 | 问题 |
| --- | --- |
| A. ansible `copy` / `scp` 逐台推大文件 | 10 节点 × 数百 MB 重复传输；断点重试差；ansible 连接成为瓶颈 |
| B. **Nginx 起内网源，节点用 dnf 从 HTTP 拉取** | 需要节点能访问源、需要签名与 ACL |
| C. 共享文件系统 / NFS | 依赖客户提供 NFS，跨网段与权限复杂 |

## 2. 决策

采用 **B：Nginx 提供内网软件源，ansible 只负责配置与触发，不负责搬字节**。

```
安装机 / 管理节点
  └─ Nginx（只读静态源，可复用管理面 Nginx 的另一个 server block）
       ├─ /repo/platform/<ver>/   ← 平台 RPM（agent、依赖）+ repodata（GPG 签名）
       ├─ /repo/baseos/           ← 可选：基础包镜像（客户已有则复用）
       ├─ /images/                ← 容器镜像 tar + digest 清单（供 podman load）
       └─ /ansible/               ← playbook、校验脚本、BOM.md
被管节点 ── dnf（baseurl=http://<源>/repo/platform/<ver>/）──> 安装
```

ansible 侧只做三件事：配 repo 文件（`yum_repository`）→ 触发安装（`dnf`）→ 校验版本与签名。

## 3. 为什么这样更好

1. **带宽只花一次**：N 台节点共享同一份源，而不是 N 份 scp。
2. **天然幂等与可重试**：dnf 解析依赖、跳过已装的包、断点续传；scp 中断要整份重来。
3. **可审计**：源侧 Nginx access log 就是「哪台机器装了什么、什么时候装的」。
4. **可演进**：装机完成后源不必拆，直接变成**内网更新源**，后续补丁与 Ceph 升级复用同一条链路。
5. **ansible 职责变轻**：从「传文件 + 装」变成「改配置 + 触发」，playbook 更简单、更不容易出错。

## 4. 两条必须先说清的边界

### 4.1 容器镜像不是 RPM，走不了 yum 源

平台组件（Server、RabbitMQ、PostgreSQL、VictoriaMetrics、Grafana…）都是容器镜像，`dnf` 装不了。两条路：

- **走 registry**：Harbor 或 `registry:2`，节点 `podman pull`；**已定为自建 Harbor（[ADR-16](ADR-16-镜像仓库.md)）**；
- **走静态 tar**：Nginx 提供 `images/*.tar` + `images.digest`，ansible 拉取后 `podman load`。**该路径已降级为应急方案**——仅在 Harbor 不可用时手工兜底，不作为常规分发方式。

### 4.2 HTTP 明文 + 不校验签名 = 新的投毒面

源一旦被替换，**所有节点会一起装上被篡改的包**——这和 B2「高权限 Agent 是 RCE 放大器」是同一类风险，只是入口从管理面换成了源。

因此强制：

| 要求 | 说明 |
| --- | --- |
| RPM 必须签名 | 发布流程内 `rpm --addsign`，私钥不进仓库 |
| repodata 必须签名 | `gpg --detach-sign` 生成 `repomd.xml.asc` |
| 节点侧 `gpgcheck=1` + `repo_gpgcheck=1` | 两个开关都要开，只开一个等于没开 |
| 源只读 | Nginx 仅允许 `GET` / `HEAD`，关闭 `autoindex` |
| 限来源网段 | `allow <管理网段>; deny all;` |
| 保留访问日志 | 装机与升级都要能追溯到节点 |

> 若客户坚持要加密传输，可在同一 Nginx 上加自签 TLS 并由 ansible 分发 CA——但这增加证书生命周期管理，**MVP 阶段用「签名 + 受控网段」更划算**。

## 5. 运维约束

- **源可复用**：管理面已有 Nginx，优先加一个 `server` 块，不新增组件（与 ADR-13 一致：不因「多一个」而拒绝，但也没必要重复造）。
- **源必须有备份与保留策略**：它现在是「装机与升级的唯一依赖」，删了就没法重装。至少保留当前版 + 上一版目录。
- **源单点**：Nginx 挂掉不影响已装节点运行，但新装/扩容/升级会被阻塞。修复优先级应等同于管理面。
- **SELinux / 防火墙**：源目录需要正确的上下文（`httpd_sys_content_t`），80/443 需放行管理网段；这两项是现场最常见的失败原因。
- **代理**：节点若配了全局 proxy，dnf 会走代理导致源不可达，playbook 需显式 `proxy=_none_` 或清空。

## 6. ansible 侧要求（不给具体命令）

- 用 `yum_repository` 生成本地 repo 文件，`gpgcheck=1`、`repo_gpgcheck=1`、`enabled=1`、`baseurl` 指向内网源；
- 用 `dnf` 安装，开启 `disable_gpg_check: false`；禁止 `--nogpgcheck`；
- `serial` 分批（建议 20% 或 5 台一档），配合 `any_errors_fatal: true`，避免同时打满源；
- 首次执行一律先 `--check`，把「磁盘不足 / 源不可达 / 版本不符」暴露在不改系统的阶段；
- 装完用 `rpm -q` + BOM 比对版本，结果回写到平台的「主机」页。

## 7. 风险

| 风险 | 缓解 |
| --- | --- |
| 源被篡改（明文 HTTP） | 包与 repodata 双重签名 + 限网段 + 只读 |
| 源单点导致扩容/升级阻塞 | 源可用性纳入监控；保留本地缓存（`dnf` 的 `/var/cache`）作为短时兜底 |
| 多网段 ACL 配错，部分节点装不上 | 前置检查里加「源可达性」探测，先报错再动手 |
| 源目录无限膨胀（每版都留） | 版本目录 + 保留策略（当前版 + 上一版），旧版归档到冷存储 |
| 镜像走 tar 后难以管理 | 若镜像数量增长，回归 Harbor；这条是 D3 的判断依据 |

## 8. 什么情况下推翻

- 客户已有 Nexus / Artifactory / 内部 yum 源 → 改为**对接客户源**，我们只提供 repodata 与签名。
- 节点不允许主动访问安装机（单向网络）→ 退回 ansible `copy` 或逐台介质安装。
- 镜像规模增长到无法用 tar 管理 → 强制引入 registry（D3 收敛为 Harbor）。

推翻流程同前：新开 ADR → 本 ADR 标 `Superseded` → 同步 ADR-14 阶段 C、安装手册 §5、PRD FR-10、TC-22、`AGENTS.md` I14。
