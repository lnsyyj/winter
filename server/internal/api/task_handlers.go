package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lnsyyj/winter/server/internal/task"
)

// validationError 区分「调用方传错」与「系统故障」，让 handler 能给出正确的 HTTP 状态。
type validationError struct{ message string }

func (e *validationError) Error() string { return e.message }

type createTaskReq struct {
	Type           string         `json:"type" binding:"required"`
	ClusterID      string         `json:"cluster_id"`
	Params         map[string]any `json:"params"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type taskListItem struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	ClusterID    string     `json:"cluster_id"`
	Status       string     `json:"status"`
	AttemptCount int        `json:"attempt_count"`
	ErrorCode    string     `json:"error_code,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

type taskDetail struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	ClusterID    string          `json:"cluster_id"`
	Params       json.RawMessage `json:"params"`
	Status       string          `json:"status"`
	AttemptCount int             `json:"attempt_count"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	LeaseOwner   string          `json:"lease_owner,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	StartedAt    *time.Time      `json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at"`
	NextRetryAt  time.Time       `json:"next_retry_at"`
}

// submitTask 是「变更只落任务、不直接改资源」的唯一入口。
// 返回 (task_id, status, 是否幂等重放, error)。
//
// 幂等键优先用请求显式给出的，其次用 Idempotency-Key 头，
// 最后按 business 参数生成——同一请求重放不会开出第二张工单。
func (s *Server) submitTask(c *gin.Context, spec createTaskReq) (string, string, bool, error) {
	if err := task.ValidateType(spec.Type); err != nil {
		return "", "", false, &validationError{err.Error()}
	}
	if spec.ClusterID != "" {
		if _, err := uuid.Parse(spec.ClusterID); err != nil {
			return "", "", false, &validationError{"cluster_id 不是合法 UUID"}
		}
	}

	key := spec.IdempotencyKey
	if key == "" {
		key = c.GetHeader("Idempotency-Key")
	}
	if key == "" {
		key = task.IdempotencyKey(spec.ClusterID, spec.Type, spec.Params)
	}

	params := []byte("{}")
	if spec.Params != nil {
		b, err := json.Marshal(spec.Params)
		if err != nil {
			return "", "", false, fmt.Errorf("序列化任务参数: %w", err)
		}
		params = b
	}

	u := mustUser(c)
	var id, status string
	err := s.db.Pool.QueryRow(c, `
		insert into tasks (cluster_id, type, params, status, idempotency_key, created_by, next_retry_at)
		values (nullif($1, '')::uuid, $2, $3::jsonb, 'PENDING', $4, nullif($5, '')::uuid, now())
		on conflict (idempotency_key) do nothing
		returning id::text, status`,
		spec.ClusterID, spec.Type, params, key, u.ID).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		// 冲突即重放：返回首次创建的那张工单，而不是报错或新建。
		err = s.db.Pool.QueryRow(c,
			`select id::text, status from tasks where idempotency_key = $1`, key).Scan(&id, &status)
		if err != nil {
			return "", "", false, fmt.Errorf("查询既有任务: %w", err)
		}
		return id, status, true, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("写入任务: %w", err)
	}
	return id, status, false, nil
}

func taskSubmitError(c *gin.Context, err error) {
	var ve *validationError
	if errors.As(err, &ve) {
		fail(c, http.StatusBadRequest, codeBadRequest, ve.message)
		return
	}
	slog.Error("提交任务失败", "error", err)
	fail(c, http.StatusInternalServerError, codeInternal, "提交任务失败")
}

// POST /v1/tasks —— 参数化任务提交，变更类接口一律 202 + task_id。
func (s *Server) createTask(c *gin.Context) {
	var req createTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, codeBadRequest, "type 必填，params 必须是 JSON 对象")
		return
	}
	id, status, replayed, err := s.submitTask(c, req)
	if err != nil {
		taskSubmitError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": id, "status": status})
}

// GET /v1/tasks —— 分页 + 状态/集群筛选。
func (s *Server) listTasks(c *gin.Context) {
	p := parsePagination(c)
	status := c.Query("status")
	clusterID := c.Query("cluster_id")
	if clusterID != "" {
		if _, err := uuid.Parse(clusterID); err != nil {
			fail(c, http.StatusBadRequest, codeBadRequest, "cluster_id 不是合法 UUID")
			return
		}
	}

	where := `where ($1 = '' or status = $1)
	            and ($2 = '' or cluster_id = nullif($2, '')::uuid)`
	var total int
	if err := s.db.Pool.QueryRow(c,
		`select count(*) from tasks `+where, status, clusterID).Scan(&total); err != nil {
		slog.Error("统计任务失败", "error", err)
		fail(c, http.StatusInternalServerError, codeInternal, "查询任务失败")
		return
	}

	rows, err := s.db.Pool.Query(c, `
		select id::text, type, coalesce(cluster_id::text, ''), status, attempt_count,
		       coalesce(error_code, ''), created_at, started_at, finished_at
		  from tasks `+where+`
		 order by created_at desc
		 limit $3 offset $4`, status, clusterID, p.Size, p.Offset)
	if err != nil {
		slog.Error("查询任务失败", "error", err)
		fail(c, http.StatusInternalServerError, codeInternal, "查询任务失败")
		return
	}
	defer rows.Close()

	items := make([]taskListItem, 0)
	for rows.Next() {
		var it taskListItem
		var started, finished pgtype.Timestamptz
		if err := rows.Scan(&it.ID, &it.Type, &it.ClusterID, &it.Status,
			&it.AttemptCount, &it.ErrorCode, &it.CreatedAt, &started, &finished); err != nil {
			slog.Error("读取任务行失败", "error", err)
			fail(c, http.StatusInternalServerError, codeInternal, "查询任务失败")
			return
		}
		it.StartedAt = timestamptzPtr(started)
		it.FinishedAt = timestamptzPtr(finished)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		slog.Error("遍历任务行失败", "error", err)
		fail(c, http.StatusInternalServerError, codeInternal, "查询任务失败")
		return
	}
	c.JSON(http.StatusOK, pageResult(items, total, p))
}

// GET /v1/tasks/:id —— 详情含参数与租约信息，供排障直接看 tasks 表事实。
func (s *Server) getTask(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		fail(c, http.StatusBadRequest, codeBadRequest, "task id 不是合法 UUID")
		return
	}

	var it taskDetail
	var params []byte
	var started, finished pgtype.Timestamptz
	err := s.db.Pool.QueryRow(c, `
		select id::text, type, coalesce(cluster_id::text, ''), params::text, status,
		       attempt_count, coalesce(error_code, ''), coalesce(error_message, ''),
		       coalesce(lease_owner, ''), created_at, started_at, finished_at, next_retry_at
		  from tasks where id = $1::uuid`, id).Scan(
		&it.ID, &it.Type, &it.ClusterID, &params, &it.Status, &it.AttemptCount,
		&it.ErrorCode, &it.ErrorMessage, &it.LeaseOwner,
		&it.CreatedAt, &started, &finished, &it.NextRetryAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, http.StatusNotFound, codeNotFound, "任务不存在")
		return
	}
	if err != nil {
		slog.Error("查询任务详情失败", "error", err, "task_id", id)
		fail(c, http.StatusInternalServerError, codeInternal, "查询任务失败")
		return
	}
	it.Params = json.RawMessage(params)
	it.StartedAt = timestamptzPtr(started)
	it.FinishedAt = timestamptzPtr(finished)
	c.JSON(http.StatusOK, it)
}

func timestamptzPtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}
