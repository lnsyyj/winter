package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func slogError(message string, err error) {
	slog.Error(message, "error", err)
}

// GET /v1/system/queue —— ADR-36 §5 要求自建队列深度指标，TC-40 用它验收。
func (s *Server) queueStats(c *gin.Context) {
	stats, err := s.queue.Stats(c)
	if err != nil {
		slog.Error("统计队列深度失败", "error", err)
		fail(c, http.StatusInternalServerError, codeInternal, "统计队列深度失败")
		return
	}
	c.JSON(http.StatusOK, stats)
}
