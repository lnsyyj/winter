# nginx · 接入层（叠加式）

对应部署阶段 `deploy`。

## 为什么是「叠加」而不是「替换」

交付现场的管理节点上**可能已经有 Nginx 在跑别的站点**（本机现状就是：`argus` 占了 80/443 的默认虚拟主机，反代到 8080）。直接覆盖 `nginx.conf` 会把别人的站点弄坏。

因此本 role 的规则是：

| 规则 | 原因 |
| --- | --- |
| 只写 `conf.d/winter.conf` 一个文件 | 不动 `nginx.conf`、不动别人的 conf |
| **不声明 `default_server`** | 同端口的默认站点仍归已有应用 |
| 靠 `server_name` 分流 | 只有访问本平台域名时才进我们这段 |
| 用 `reload` 而非 `restart` | 不断已有连接，别人的服务无感 |
| 证书与静态目录独立（`/var/lib/winter/...`） | 不与 `/var/www` 混用 |

## 变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `nginx.server_name` | `{{ platform.domain }}` | 证书 CN 与站点域名，默认 `winter.local` |
| `nginx.https_port` | `{{ platform.https_port }}` | 默认 443 |
| `nginx.doc_root` | `/var/lib/winter/www` | 静态资源 |
| `nginx.tls_dir` | `/var/lib/winter/tls` | 自签证书（首次生成，已有则不动） |
| `nginx.api_upstream` | `http://127.0.0.1:8081` | Server 地址；**8080 可能被同机服务占用** |

## 验证

```bash
nginx -t
curl -k -H "Host: winter.local" https://127.0.0.1/healthz      # → ok
curl -k -H "Host: winter.local" https://127.0.0.1/             # → 占位页
```

## 已知取舍

- 自签证书只适合内网/测试；正式交付换成企业 CA 或 ACME，改 `nginx.tls_dir` 下的两个文件后 `reload` 即可。
- `/api/` 在 Server 未部署前返回 502，属预期；这不是配置错误。
- 若客户已有 LB，本 role 的 80/443 可整体关闭（`--skip-tags nginx`），只用 LB 指向 Server。
