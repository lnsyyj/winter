package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type clusterListItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	FSID        string     `json:"fsid,omitempty"`
	AZID        string     `json:"az_id,omitempty"`
	Capability  string     `json:"capability"`
	CephVersion string     `json:"ceph_version,omitempty"`
	Health      string     `json:"health,omitempty"`
	DriftState  string     `json:"drift_state"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSyncAt  *time.Time `json:"last_sync_at"`
}

type createClusterReq struct {
	Name       string         `json:"name" binding:"required"`
	FSID       string         `json:"fsid"`
	Capability string         `json:"capability"`
	AZID       string         `json:"az_id"`
	Params     map[string]any `json:"params"`
}

// GET /v1/clusters —— 纳管单位是 cluster（ADR-27/ADR-35），列表按 capability/health 筛选。
func (s *Server) listClusters(c *gin.Context) {
	p := parsePagination(c)
	capability := c.Query("capability")
	health := c.Query("health")

	where := `where ($1 = '' or capability = $1)
	            and ($2 = '' or health = $2)`
	var total int
	if err := s.db.Pool.QueryRow(c,
		`select count(*) from clusters `+where, capability, health).Scan(&total); err != nil {
		slogError("统计集群失败", err)
		fail(c, http.StatusInternalServerError, codeInternal, "查询集群失败")
		return
	}

	rows, err := s.db.Pool.Query(c, `
		select id::text, name, coalesce(fsid, ''), coalesce(az_id::text, ''), capability,
		       coalesce(ceph_version, ''), coalesce(health, ''), drift_state,
		       created_at, last_sync_at
		  from clusters `+where+`
		 order by created_at desc
		 limit $3 offset $4`, capability, health, p.Size, p.Offset)
	if err != nil {
		slogError("查询集群失败", err)
		fail(c, http.StatusInternalServerError, codeInternal, "查询集群失败")
		return
	}
	defer rows.Close()

	items := make([]clusterListItem, 0)
	for rows.Next() {
		var it clusterListItem
		var lastSync pgtype.Timestamptz
		if err := rows.Scan(&it.ID, &it.Name, &it.FSID, &it.AZID, &it.Capability,
			&it.CephVersion, &it.Health, &it.DriftState, &it.CreatedAt, &lastSync); err != nil {
			slogError("读取集群行失败", err)
			fail(c, http.StatusInternalServerError, codeInternal, "查询集群失败")
			return
		}
		it.LastSyncAt = timestamptzPtr(lastSync)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		slogError("遍历集群行失败", err)
		fail(c, http.StatusInternalServerError, codeInternal, "查询集群失败")
		return
	}
	c.JSON(http.StatusOK, pageResult(items, total, p))
}

// POST /v1/clusters —— 新建集群是一张异步工单，不在这里同步建集群。
// 真正的 bootstrap 由 Agent 消费 cluster.create 任务完成（ADR-35：Server 不碰 cephadm）。
func (s *Server) createCluster(c *gin.Context) {
	var req createClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, codeBadRequest, "name 必填")
		return
	}
	if len(req.Name) > 128 {
		fail(c, http.StatusBadRequest, codeBadRequest, "name 不能超过 128 字符")
		return
	}
	if req.Capability == "" {
		req.Capability = "managed"
	}
	if req.Capability != "managed" && req.Capability != "limited" {
		fail(c, http.StatusBadRequest, codeBadRequest, "capability 只能是 managed 或 limited")
		return
	}
	if req.AZID != "" {
		if _, err := uuid.Parse(req.AZID); err != nil {
			fail(c, http.StatusBadRequest, codeBadRequest, "az_id 不是合法 UUID")
			return
		}
	}
	if req.FSID != "" {
		if _, err := uuid.Parse(req.FSID); err != nil {
			fail(c, http.StatusBadRequest, codeBadRequest, "fsid 不是合法 UUID")
			return
		}
	}

	params := req.Params
	if params == nil {
		params = map[string]any{}
	}
	params["name"] = req.Name
	params["capability"] = req.Capability
	if req.FSID != "" {
		params["fsid"] = req.FSID
	}
	if req.AZID != "" {
		params["az_id"] = req.AZID
	}

	id, status, replayed, err := s.submitTask(c, createTaskReq{
		Type:   "cluster.create",
		Params: params,
	})
	if err != nil {
		taskSubmitError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": id, "status": status})
}
