// Package api 是管理面对外的 HTTP 层：路由、鉴权中间件与 REST 处理函数。
//
// 对外由 Nginx 把 /api/ 反代到本进程（proxy_pass 带尾斜杠，剥掉 /api 前缀），
// 因此这里的路径是 /v1/... 与 /healthz。
package api

import "github.com/gin-gonic/gin"

// APIError 是《API 与数据模型设计》§1 约定的统一错误体。
// code 是业务码，与 HTTP 状态分离；trace_id 让调用方能反查审计链。
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id,omitempty"`
	Details any    `json:"details,omitempty"`
}

const (
	codeBadRequest   = "BAD_REQUEST"
	codeUnauthorized = "UNAUTHORIZED"
	codeForbidden    = "FORBIDDEN"
	codeNotFound     = "NOT_FOUND"
	codeInternal     = "INTERNAL_ERROR"
	codeUnavailable  = "SERVICE_UNAVAILABLE"
)

func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, APIError{
		Code:    code,
		Message: message,
		TraceID: traceID(c),
	})
}

// traceID 取审计中间件写入的值；未落 intent 的请求（如登录失败）为空。
func traceID(c *gin.Context) string {
	v, _ := c.Get("trace_id")
	s, _ := v.(string)
	return s
}
