# winter-server（Go + Gin）

管理面唯一的 HTTP 入口。M0 目标是「能启动、能鉴权、能落任务、审计不丢」，
不碰 Ceph、不建 SSH 通道、不接消息队列——这三条分别由 ADR-35 / ADR-29 / ADR-36 约束。

## 1. 目录

```
main.go                        启动与优雅关闭（配置 → 连接池 → 迁移 → 管理员 → 监听）
internal/config/               只从环境变量读配置（ADR-17：配置由编排层注入）
internal/db/                   业务库 + 审计库双连接池、embed 迁移、break-glass 管理员
  migrations/*.sql             业务库迁移
  migrations/audit/*.sql       审计库迁移（独立库，ADR-29）
internal/auth/                 bcrypt 口令哈希、JWT 签发/校验、refresh 令牌哈希（ADR-34）
internal/audit/                两段式审计 intent → result + 链式哈希（ADR-29）
internal/task/                 任务表即队列：SKIP LOCKED / 租约 / 退避 / 死信（ADR-36）
internal/api/                  Gin 路由、鉴权中间件、REST handler
```

迁移 SQL 用 `go:embed` 编进二进制，部署时不需要额外分发 SQL 文件。

## 2. 配置（全部环境变量）

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `DATABASE_URL` | 是 | — | 业务库 DSN，例 `postgres://winter:pwd@127.0.0.1:5434/winter?sslmode=disable` |
| `AUDIT_DATABASE_URL` | 否 | 同 `DATABASE_URL` | 审计库；M0 允许同实例，正式环境必须独立库 |
| `JWT_SECRET` | 是 | — | 至少 32 字符；短了拒绝启动 |
| `LISTEN_ADDR` | 否 | `127.0.0.1:8081` | 与 `deploy/group_vars/all.yml` 的 `nginx.api_upstream` 保持一致 |
| `APP_ENV` | 否 | `dev` | 设为 `prod` 时 Gin 进 ReleaseMode |
| `ACCESS_TTL` | 否 | `15m` | 支持 `15m` 或纯秒数 `900` |
| `REFRESH_TTL` | 否 | `168h` | 同上 |
| `ADMIN_USER` | 否 | `admin` | break-glass 本地管理员 |
| `ADMIN_PASSWORD` | 否 | — | 未设置则跳过初始化（不会生成默认口令） |

## 3. 运行

```bash
# 本地开发
cd server
go run . # 需先设置 DATABASE_URL / JWT_SECRET

# 交付：单二进制交叉编译，目标机不编译（ADR-14）
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o winter-server .
```

启动顺序固定，任一步失败都不进入监听：配置 → 连接池 Ping → 迁移 → 本地管理员 → 监听。
收到 SIGTERM/SIGINT 后给在途请求 10s 退出。

Nginx 侧 `location /api/ { proxy_pass http://127.0.0.1:8081/; }` 会剥掉 `/api` 前缀，
所以对外是 `/api/v1/...`，进程内是 `/v1/...`。

## 4. M0 接口

| 方法 | 路径 | 权限 | 返回 |
| --- | --- | --- | --- |
| GET | `/healthz` | 公开 | 200 `{status,db}` / 503 |
| POST | `/v1/auth/login` | 公开 | 200 access + refresh |
| POST | `/v1/auth/refresh`、`/logout` | 公开 | 200 |
| GET | `/v1/me` | 登录 | 200 |
| GET | `/v1/tasks` | 登录 | 200 分页 `{items,total,page,size}` |
| POST | `/v1/tasks` | operator+ | **202 `{task_id,status}`** |
| GET | `/v1/tasks/:id` | 登录 | 200 详情 |
| GET | `/v1/clusters` | 登录 | 200 分页 |
| POST | `/v1/clusters` | operator+ | **202 `{task_id,status}`**（cluster.create 工单） |
| GET | `/v1/system/queue` | 登录 | 200 队列深度/最老任务年龄（TC-40） |

约定（见《API 与数据模型设计》）：

- 变更类接口一律 `202 + task_id`，不返回同步结果；这里**不直接建集群**，
  bootstrap 由 Agent 消费工单完成。
- 幂等：请求体 `idempotency_key` → `Idempotency-Key` 头 → 按
  `sha256(cluster_id + type + canonical(params))` 自动生成。重放返回首次工单，
  响应带 `Idempotency-Replayed: true`。
- `type` 必须在 `internal/task.AllowedTypes` 白名单内，未知类型 400。
- 错误统一 `{code, message, trace_id?}`，内部错误（含 DB 连接串）只进日志。
- 写操作先落审计 `intent`，审计不可用直接 503 拒绝执行（fail-closed）。

## 5. 任务表即队列（ADR-36）

`internal/task.Queue` 提供 Claim / ReclaimExpired / Renew / Finish / Stats：

- `Claim` 在**同一事务的同一条语句**里用 `FOR UPDATE SKIP LOCKED` 选取并改成 `RUNNING`，
  写 `lease_owner` / `leased_until`；多副本 Server 各拿各的行，不会重复派发。
- `ReclaimExpired` 回收超时租约：`attempt_count++`、按 `1s→5s→30s→2min→10min` 退避，
  达到 5 次进 `DEAD_LETTER`。
- `Renew` / `Finish` 都带 `lease_owner + status='RUNNING'` 条件，旧 Agent 的迟到回报
  不能覆盖新租约。

> M0 尚未接入 Agent 的 mTLS 任务流，所以这些方法是给下一阶段的 gRPC 流调用的；
> HTTP 层只暴露只读的队列指标。传输栈见《Agent-Server通信协议》。

## 6. 测试

```bash
cd server
gofmt -l .            # 应为空
go vet ./...
go test -race ./...   # 表驱动单测，不依赖数据库
```

关键路径都有单测：JWT 往返/篡改/过期、口令哈希、配置校验与 TTL 解析、
退避档位与死信上限一致性、幂等键确定性与防碰撞、任务类型白名单、
分页边界与统一错误体。SQL 侧语义需要连真实 PG 的集成测试（TC-40），
属于下一阶段。

## 7. M0 明确不做

Agent 通道（gRPC/mTLS）、WebSocket 进度推送、LDAP、RBAC 三级作用域、
凭据轮换、审计导出、任务步骤表、UUIDv7 主键（PG17 无内置函数）、
systemd unit 与离线制品入库。这些按依赖顺序在后续阶段补齐。

## 8. 实机验证记录（2026-11-15）

CentOS Stream 9 / Go 1.26.7 / PostgreSQL 17.11（经 HAProxy 5434 连 Patroni 集群）：

- 本地交叉编译 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` 的单二进制上传运行，
  目标机不编译（ADR-14）；用 `go mod vendor` 生成离线依赖在实机跑测试。
- `gofmt -l` 为空、`go vet ./...` 通过、`go test -race ./...` 全通过；
  `go test -race -tags integration ./internal/task`：8 并发派单无重复、
  租约回收 5 轮后进 `DEAD_LETTER`、续租 / 完成 / 队列深度统计均符合预期。
- `/healthz` 200；业务库与审计库共 9 张表自动迁移；break-glass `admin` 自动创建。
- REST：登录 / 刷新 / 登出后刷新被拒、401 与 400 统一错误体、变更 202 + `task_id`、
  幂等重放返回同一 `task_id`、分页、任务详情、队列深度；
  审计 `intent` 与 `result` 数量一一对应（fail-closed 生效）。
- 经 Nginx HTTPS 反代 `https://winter.local/api/v1/me` 返回 200，
  `/api/` 前缀剥离与 `nginx.api_upstream=127.0.0.1:8081` 对齐。
