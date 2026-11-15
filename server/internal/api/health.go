package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) health(c *gin.Context) {
	if err := s.db.Healthy(c); err != nil {
		// 连接错误可能带主机/账号信息，只进日志，不回给前端
		slog.Warn("健康检查：数据库不可用", "error", err)
		fail(c, http.StatusServiceUnavailable, codeUnavailable, "数据库不可用")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "ok"})
}
