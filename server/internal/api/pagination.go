package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type pagination struct {
	Page   int `json:"page"`
	Size   int `json:"size"`
	Offset int `json:"-"`
}

// parsePagination 对非法/越界参数取安全值，而不是报错：
// 列表接口的查询串来自浏览器，拼错一个参数不该让整页 400。
func parsePagination(c *gin.Context) pagination {
	p := pagination{Page: 1, Size: defaultPageSize}
	if v, err := strconv.Atoi(c.Query("page")); err == nil && v > 0 {
		p.Page = v
	}
	if v, err := strconv.Atoi(c.Query("size")); err == nil && v > 0 {
		p.Size = v
	}
	if p.Size > maxPageSize {
		p.Size = maxPageSize
	}
	p.Offset = (p.Page - 1) * p.Size
	return p
}

// pageResult 统一 {items, total, page, size}（《API 与数据模型设计》§1）
func pageResult(items any, total int, p pagination) gin.H {
	return gin.H{"items": items, "total": total, "page": p.Page, "size": p.Size}
}
