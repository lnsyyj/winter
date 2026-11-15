//go:build integration

package task

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 这个包只在带 -tags integration 且设置 WINTER_TEST_DSN 时运行，
// 用于验证必须连真实 PostgreSQL 才能确认的队列语义（TC-40）。
// 默认 go test ./... 不依赖数据库。

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WINTER_TEST_DSN")
	if dsn == "" {
		t.Skip("设置 WINTER_TEST_DSN 才会运行集成测试")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("Ping 测试库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertPending(t *testing.T, ctx context.Context, pool *pgxpool.Pool, prefix string) string {
	t.Helper()
	key := prefix + "-" + uuid.NewString()
	var id string
	if err := pool.QueryRow(ctx, `
		insert into tasks (type, params, status, idempotency_key, next_retry_at)
		values ('host.precheck', '{}'::jsonb, 'PENDING', $1, now())
		returning id::text`, key).Scan(&id); err != nil {
		t.Fatalf("插入测试任务失败: %v", err)
	}
	return id
}

func taskState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) (string, int, time.Time) {
	t.Helper()
	var status string
	var attempts int
	var nextRetry time.Time
	if err := pool.QueryRow(ctx,
		`select status, attempt_count, next_retry_at from tasks where id = $1::uuid`, id).
		Scan(&status, &attempts, &nextRetry); err != nil {
		t.Fatalf("读取任务状态失败: %v", err)
	}
	return status, attempts, nextRetry
}

// TestQueueConcurrentClaim 对应 TC-40「两个 Server 副本同时派单，同一任务不会被派两次」。
func TestQueueConcurrentClaim(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	prefix := "it-concurrent-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`delete from tasks where idempotency_key like $1`, prefix+"%")
	})

	const n = 8
	for i := 0; i < n; i++ {
		insertPending(t, ctx, pool, prefix)
	}

	q := New(pool)
	var mu sync.Mutex
	claimed := make(map[string]bool)
	var duplicates int
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := q.Claim(ctx, "agent-"+strconv.Itoa(i), 30*time.Second)
			if err != nil {
				t.Errorf("并发派单报错: %v", err)
				return
			}
			if got == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if claimed[got.ID] {
				duplicates++
				t.Errorf("任务 %s 被派发了两次", got.ID)
			}
			claimed[got.ID] = true
		}(i)
	}
	wg.Wait()

	if duplicates != 0 {
		t.Fatalf("出现 %d 次重复派发", duplicates)
	}
	if len(claimed) != n {
		t.Fatalf("派发数量 = %d，期望 %d（每个 goroutine 各拿一条）", len(claimed), n)
	}
}

// TestQueueLeaseLifecycle 覆盖租约、续租、回收退避、死信与完成（ADR-36 §4）。
func TestQueueLeaseLifecycle(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	prefix := "it-lease-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`delete from tasks where idempotency_key like $1`, prefix+"%")
	})
	q := New(pool)

	id := insertPending(t, ctx, pool, prefix)
	got, err := q.Claim(ctx, "agent-a", 30*time.Second)
	if err != nil || got == nil || got.ID != id {
		t.Fatalf("Claim 未领到预期任务: got=%+v err=%v（期望 %s）", got, err, id)
	}
	if status, _, _ := taskState(t, ctx, pool, id); status != "RUNNING" {
		t.Fatalf("派单后状态 = %s，期望 RUNNING", status)
	}

	renewed, err := q.Renew(ctx, id, "agent-a", 30*time.Second)
	if err != nil || !renewed {
		t.Fatalf("续租失败: renewed=%v err=%v", renewed, err)
	}
	if _, err := q.Renew(ctx, id, "wrong-owner", 30*time.Second); err != nil {
		t.Fatalf("错误 owner 的续租不应报错: %v", err)
	}

	// 模拟 Agent 崩溃：租约过期 → 回收重投
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		if _, err := pool.Exec(ctx, `
			update tasks set leased_until = now() - interval '1 minute'
			where id = $1::uuid`, id); err != nil {
			t.Fatalf("制造过期租约失败: %v", err)
		}
		reclaimed, err := q.ReclaimExpired(ctx)
		if err != nil {
			t.Fatalf("回收失败: %v", err)
		}
		if reclaimed == 0 {
			t.Fatalf("第 %d 轮应回收到任务", attempt)
		}
		status, attempts, nextRetry := taskState(t, ctx, pool, id)
		if attempts != attempt {
			t.Fatalf("第 %d 轮 attempt_count = %d", attempt, attempts)
		}
		wantStatus := "RETRYING"
		if attempt >= MaxAttempts {
			wantStatus = "DEAD_LETTER"
		}
		if status != wantStatus {
			t.Fatalf("第 %d 轮状态 = %s，期望 %s", attempt, status, wantStatus)
		}
		if attempt < MaxAttempts && !nextRetry.After(time.Now()) {
			t.Fatalf("第 %d 轮 next_retry_at 未落在未来: %s", attempt, nextRetry)
		}
		if attempt >= MaxAttempts {
			break
		}
		// 跳过退避等待：把任务放回可领状态再制造下一次过期
		if _, err := pool.Exec(ctx, `
			update tasks set next_retry_at = now(), status = 'PENDING'
			where id = $1::uuid`, id); err != nil {
			t.Fatalf("重置退避失败: %v", err)
		}
		claimed, err := q.Claim(ctx, "agent-a", 30*time.Second)
		if err != nil || claimed == nil || claimed.ID != id {
			t.Fatalf("重投后未领到任务: got=%+v err=%v", claimed, err)
		}
	}

	// 死信不再被派发
	deadID := insertPending(t, ctx, pool, prefix)
	if _, err := pool.Exec(ctx, `
		update tasks set status = 'DEAD_LETTER' where id = $1::uuid`, deadID); err != nil {
		t.Fatalf("构造死信失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		delete from tasks where status = 'RUNNING' and idempotency_key like $1`, prefix+"%"); err != nil {
		t.Fatalf("清理 RUNNING 失败: %v", err)
	}
	afterDead, err := q.Claim(ctx, "agent-a", 30*time.Second)
	if err != nil {
		t.Fatalf("死信清理后派单报错: %v", err)
	}
	if afterDead != nil && afterDead.ID == deadID {
		t.Fatal("DEAD_LETTER 任务不应被重新派发")
	}

	// 正常完成路径与 owner 校验
	finishID := insertPending(t, ctx, pool, prefix)
	if _, err := pool.Exec(ctx, `
		delete from tasks where status = 'RUNNING' and idempotency_key like $1`, prefix+"%"); err != nil {
		t.Fatalf("清理 RUNNING 失败: %v", err)
	}
	claimed, err := q.Claim(ctx, "agent-b", 30*time.Second)
	if err != nil || claimed == nil || claimed.ID != finishID {
		t.Fatalf("完成路径未领到任务: got=%+v err=%v（期望 %s）", claimed, err, finishID)
	}
	ok, err := q.Finish(ctx, finishID, "wrong-owner", "SUCCEEDED", "", "")
	if err != nil || ok {
		t.Fatalf("错误 owner 不应能结束任务: ok=%v err=%v", ok, err)
	}
	ok, err = q.Finish(ctx, finishID, "agent-b", "SUCCEEDED", "", "")
	if err != nil || !ok {
		t.Fatalf("结束任务失败: ok=%v err=%v", ok, err)
	}
	if status, _, _ := taskState(t, ctx, pool, finishID); status != "SUCCEEDED" {
		t.Fatalf("完成后状态 = %s，期望 SUCCEEDED", status)
	}
	if _, err := q.Finish(ctx, finishID, "agent-b", "NOT_A_STATE", "", ""); err == nil {
		t.Fatal("非法终态必须被拒绝")
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("统计队列深度失败: %v", err)
	}
	if stats.DeadLetter < 1 {
		t.Fatalf("死信统计应至少为 1，实际 %+v", stats)
	}
}
