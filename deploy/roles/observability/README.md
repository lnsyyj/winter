# role: observability

部署 ADR-18 的可观测性栈。全部是**静态二进制**，从内网 Nginx 制品源（`local_repo`）取包，
目标机不编译、不连外网（ADR-14 / ADR-15）。

## 组件与默认端口

| 组件 | 二进制 | 端口 | 健康端点 | 说明 |
| --- | --- | --- | --- | --- |
| VictoriaMetrics | `victoria-metrics-prod` | 8428 | `/health` | 指标，保留 `monitoring_retention_days` 天 |
| vmalert | `vmalert-prod` | 8880 | `/health` | **唯一**告警规则链路，规则目录 `config_dir/rules/*.yml` |
| VictoriaLogs | `victoria-logs-prod` | 9428 | `/health` | 日志，保留 30 天；审计在 PG，不进这里 |
| Alertmanager | `alertmanager` | 9093 | `/-/healthy` | webhook 回流平台（ADR-33） |
| Grafana | 目录式安装 | 3000 | `/api/health` | 只经 Nginx `/grafana/` 反代（ADR-09） |

端口、版本、保留期、命令行参数全部在 `deploy/group_vars/all.yml` 的
`monitoring_ports` / `observability.components` 里改；本 role 不硬编码端口。
所有进程只监听 `monitoring_listen_addr`（默认 `127.0.0.1`），以 `winter-monitor`
非 root 账号运行，systemd unit 名为 `winter-<组件>`。

## 配置与裁剪

- `observability.enabled: false` 可整体跳过（playbook 级 `when`）。
- 单个组件 `enabled: false` 可裁剪，对应 ADR-18 的「打包分层」。
- `monitoring_retention_days` 同时作用于指标与日志；审计保留期是另一套（ADR-32）。
- `vault_grafana_admin_password` 必须来自 `deploy/passwords.yml`，role 会显式 assert，
  不会生成默认弱口令。
- vmalert 的 `rules/platform.yml` 是 M0 占位规则（永不为真）；接入采集后按 ADR-18
  补「VictoriaLogs 磁盘水位」与 ADR-36 的「队列深度 / 最老任务年龄」告警。
- Alertmanager 的 webhook 指向 Server `/v1/alerts/webhook`，M0 端点尚未实现；
  告警可丢、审计不可丢，端点不可用不阻塞任务。

## 幂等设计

1. 解压标记文件带 tarball 名：同版本重复跑不解压，换版本自动重新解压；
2. 安装二进制前 `cmp` 比对，内容不变不覆盖；
3. 配置/unit 变更才通过 handler 重启；
4. 二次运行 `changed=0`（`uri` 健康检查为只读）。

## 制品

`deploy/artifacts/repo/tarball/` 下的 5 个包由 `deploy/scripts/fetch-artifacts.sh` 生成，
`SHA256SUMS.txt` 供搬运校验。注意两点：

- `vmalert` 官方不单独发布，脚本从 `vmutils` 全量工具包中提取 `vmalert-prod` 后**二次打包**，
  避免把 140MB 工具包带进仓库；BOM 中记录原始版本。
- Grafana 选 9.5.20，因为它是最后一个 tar.gz 小于 GitHub 单文件 100MB 上限的稳定版；
  升级到 11.x+ 需要改走分片或搬运介质。

## 常用命令

```bash
./deploy.sh deploy --tags observability      # 只收敛监控栈（自动带上制品源）
systemctl status 'winter-*'                 # 在目标机看全部组件
journalctl -u winter-grafana -f
curl -s http://127.0.0.1:8428/health
```
