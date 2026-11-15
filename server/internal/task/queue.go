package task

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// DefaultLease 是派单时给的租约窗口；Agent 心跳续租，Server 侧只认这个时间。
	DefaultLease = 30 * time.Second
	// MaxAttempts 达到即进 DEAD_LETTER，不再自动重投（ADR-36 §2）。
	MaxAttempts = 5
)

// backoffSteps 是 ADR-36 的延迟重试序列：1s → 5s → 30s → 2min → 10min。
// 索引 0 对应「第 1 次尝试」；超过序列长度保持最后一档。
var backoffSteps = []time.Duration{
	1 * time.Second,
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

// BackoffDelay 返回第 attempt 次尝试失败后应等待的时间。
// 单独导出是为了让退避策略能被表驱动单测锁住，不依赖数据库。
func BackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(backoffSteps) {
		return backoffSteps[len(backoffSteps)-1]
	}
	return backoffSteps[attempt-1]
}

// Queue 封装 tasks 表上的队列操作。
type Queue struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Queue { return &Queue{pool: pool} }

// ClaimedTask 是派单结果，交给后续的 Agent 任务流序列化下发。
type ClaimedTask struct {
	ID             string
	Type           string
	ClusterID      string
	Params         []byte
	AttemptCount   int
	IdempotencyKey string
}

// Claim 领取一条可执行任务。
//
// 两个硬要求（ADR-36 §4）都在这里：
//   - SKIP LOCKED：多副本 Server 并发派单时各拿各的行，互不阻塞；
//   - 选取与状态迁移在同一个事务、同一条语句里完成：不可能出现
//     「派单成功但状态还是 PENDING」导致重复派发。
//
// 队列空返回 (nil, nil)，不是错误；调用方据此退避轮询。
func (q *Queue) Claim(ctx context.Context, owner string, lease time.Duration) (*ClaimedTask, error) {
	if owner == "" {
		return nil, fmt.Errorf("派单 owner 不能为空（必须是 Agent 的 node_id）")
	}
	if lease <= 0 {
		lease = DefaultLease
	}

	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("开启派单事务: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var t ClaimedTask
	err = tx.QueryRow(ctx, `
		with picked as (
			select id
			  from tasks
			 where status in ('PENDING', 'RETRYING')
			   and next_retry_at <= now()
			 order by next_retry_at, created_at
			   for update skip locked
			 limit 1
		)
		update tasks t
		   set status            = 'RUNNING',
		       lease_owner       = $1,
		       leased_until      = now() + make_interval(secs => $2::double precision),
		       started_at        = coalesce(t.started_at, now()),
		       last_progress_at  = now()
		  from picked p
		 where t.id = p.id
		returning t.id::text, t.type, coalesce(t.cluster_id::text, ''),
		          t.params::text, t.attempt_count, coalesce(t.idempotency_key, '')`,
		owner, lease.Seconds()).Scan(
		&t.ID, &t.Type, &t.ClusterID, &t.Params, &t.AttemptCount, &t.IdempotencyKey)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("领取任务: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("提交派单: %w", err)
	}
	return &t, nil
}

// ReclaimExpired 回收租约过期的任务，返回受影响条数。
//
// 退避与死信判定放在 Go 侧逐行做，只保留一份策略（BackoffDelay），
// 避免 SQL 里再抄一遍序列导致两边漂移。每行都带 status='RUNNING' 条件，
// 保证与刚刚完成/续租的任务并发时不会把终态改回去。
func (q *Queue) ReclaimExpired(ctx context.Context) (int, error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("开启回收事务: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		select id::text, attempt_count
		  from tasks
		 where status = 'RUNNING' and leased_until < now()
		 order by leased_until
		   for update skip locked`)
	if err != nil {
		return 0, fmt.Errorf("扫描过期租约: %w", err)
	}
	type expired struct {
		id      string
		attempt int
	}
	var candidates []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.attempt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("读取过期任务: %w", err)
		}
		candidates = append(candidates, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("遍历过期任务: %w", err)
	}
	rows.Close()

	reclaimed := 0
	for _, e := range candidates {
		nextAttempt := e.attempt + 1
		status := "RETRYING"
		if nextAttempt >= MaxAttempts {
			status = "DEAD_LETTER"
		}
		_, err := tx.Exec(ctx, `
			update tasks
			   set attempt_count    = $2,
			       status           = $3,
			       next_retry_at    = now() + make_interval(secs => $4::double precision),
			       leased_until     = null,
			       lease_owner      = null,
			       error_code       = 'LEASE_EXPIRED',
			       last_progress_at = now()
			 where id = $1::uuid and status = 'RUNNING'`,
			e.id, nextAttempt, status, BackoffDelay(nextAttempt).Seconds())
		if err != nil {
			return 0, fmt.Errorf("回收任务 %s: %w", e.id, err)
		}
		reclaimed++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("提交回收: %w", err)
	}
	return reclaimed, nil
}

// Renew 由 Agent 心跳续租；返回 false 表示任务已不在自己名下（已被回收或完成）。
func (q *Queue) Renew(ctx context.Context, taskID, owner string, lease time.Duration) (bool, error) {
	if lease <= 0 {
		lease = DefaultLease
	}
	tag, err := q.pool.Exec(ctx, `
		update tasks
		   set leased_until = now() + make_interval(secs => $3::double precision),
		       last_progress_at = now()
		 where id = $1::uuid and lease_owner = $2 and status = 'RUNNING'`,
		taskID, owner, lease.Seconds())
	if err != nil {
		return false, fmt.Errorf("续租任务 %s: %w", taskID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// Finish 把任务推到终态。owner 不匹配时拒绝写入，防止旧 Agent 的迟到回报覆盖新租约。
func (q *Queue) Finish(ctx context.Context, taskID, owner, status, errCode, errMessage string) (bool, error) {
	switch status {
	case "SUCCEEDED", "FAILED", "CANCELED", "DEAD_LETTER":
	default:
		return false, fmt.Errorf("非法终态 %q", status)
	}
	tag, err := q.pool.Exec(ctx, `
		update tasks
		   set status           = $3,
		       finished_at      = now(),
		       error_code       = nullif($4, ''),
		       error_message    = nullif($5, ''),
		       leased_until     = null,
		       last_progress_at = now()
		 where id = $1::uuid and lease_owner = $2 and status = 'RUNNING'`,
		taskID, owner, status, errCode, errMessage)
	if err != nil {
		return false, fmt.Errorf("结束任务 %s: %w", taskID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// QueueStats 供「队列深度可观测」验收（TC-40）使用。
type QueueStats struct {
	Pending    int     `json:"pending"`
	OldestSecs float64 `json:"oldest_age_seconds"`
	Running    int     `json:"running"`
	DeadLetter int     `json:"dead_letter"`
}

func (q *Queue) Stats(ctx context.Context) (QueueStats, error) {
	var s QueueStats
	// 用一次扫描 + CASE，而不是把 FILTER 挂在 extract()/非聚合表达式上：
	// FILTER 只能紧跟聚合函数调用，挂在 extract(...) 后会直接语法错误。
	err := q.pool.QueryRow(ctx, `
		select
			coalesce(sum(case when status in ('PENDING', 'RETRYING') then 1 else 0 end), 0),
			coalesce(extract(epoch from now() - min(
				case when status in ('PENDING', 'RETRYING') then created_at end)), 0),
			coalesce(sum(case when status = 'RUNNING' then 1 else 0 end), 0),
			coalesce(sum(case when status = 'DEAD_LETTER' then 1 else 0 end), 0)
		  from tasks`).Scan(&s.Pending, &s.OldestSecs, &s.Running, &s.DeadLetter)
	if err != nil {
		return s, fmt.Errorf("统计队列深度: %w", err)
	}
	return s, nil
}
