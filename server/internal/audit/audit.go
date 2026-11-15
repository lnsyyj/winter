// Package audit：两段式审计（intent → result）+ 链式哈希。
//
// 为什么两段（ADR-29）：跨库没有共同事务，所以「写不进 intent 就不执行」是唯一可靠的
// fail-closed 落点；result 异步补写，靠「有 intent 无 result」对账兜底。
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Recorder struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Recorder { return &Recorder{pool: pool} }

// Intent 在真正执行前写入；返回 trace_id 供后续 result 使用
func (r *Recorder) Intent(ctx context.Context, actorID, actorName, action, target string, params any) (string, error) {
	traceID := uuid.NewString()
	pj, _ := json.Marshal(params)

	_, err := r.pool.Exec(ctx, `
		insert into audit_intents (trace_id, actor_id, actor_name, action, target, params)
		values ($1, nullif($2,'')::uuid, $3, $4, $5, $6)`,
		traceID, actorID, actorName, action, target, pj)
	if err != nil {
		return "", err
	}
	return traceID, nil
}

// Result 补写结果，并把它接进哈希链
func (r *Recorder) Result(ctx context.Context, traceID, status string, detail any) error {
	dj, _ := json.Marshal(detail)

	// 取链尾的 hash；并发写时用 advisory lock 串行化（审计写入量低，代价可接受）
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('audit_chain'))`); err != nil {
		return err
	}

	var prev string
	err = tx.QueryRow(ctx, `select hash from audit_results order by created_at desc limit 1`).Scan(&prev)
	if err != nil {
		prev = "" // 首条
	}

	sum := sha256.Sum256([]byte(prev + "|" + traceID + "|" + status + "|" + string(dj)))
	hash := hex.EncodeToString(sum[:])

	if _, err := tx.Exec(ctx, `
		insert into audit_results (trace_id, status, detail, prev_hash, hash)
		values ($1,$2,$3,$4,$5)
		on conflict (trace_id) do nothing`,
		traceID, status, dj, prev, hash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyChain 供后台任务/测试使用：从头重算，确认链未被篡改
func (r *Recorder) VerifyChain(ctx context.Context) (int, error) {
	rows, err := r.pool.Query(ctx, `
		select trace_id::text, status, coalesce(detail::text,'{}'), coalesce(prev_hash,''), hash
		from audit_results order by created_at asc`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var prev string
	var n int
	for rows.Next() {
		var traceID, status, detail, storedPrev, storedHash string
		if err := rows.Scan(&traceID, &status, &detail, &storedPrev, &storedHash); err != nil {
			return n, err
		}
		sum := sha256.Sum256([]byte(prev + "|" + traceID + "|" + status + "|" + detail))
		if hex.EncodeToString(sum[:]) != storedHash {
			return n, fmt.Errorf("链在 %s 处断裂", traceID)
		}
		prev = storedHash
		n++
	}
	return n, rows.Err()
}
