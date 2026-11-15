package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnsyyj/winter/server/internal/audit"
	"github.com/lnsyyj/winter/server/internal/auth"
	"github.com/lnsyyj/winter/server/internal/config"
	"github.com/lnsyyj/winter/server/internal/db"
	"github.com/lnsyyj/winter/server/internal/task"
)

type Server struct {
	cfg   *config.Config
	db    *db.DB
	audit *audit.Recorder
	queue *task.Queue
}

func New(cfg *config.Config, database *db.DB) *Server {
	return &Server{
		cfg:   cfg,
		db:    database,
		audit: audit.New(database.Audit),
		queue: task.New(database.Pool),
	}
}

// Router 组装路由。
// 对外由 Nginx 的 /api/ 反代到这里，proxy_pass 带尾斜杠已剥掉 /api 前缀，
// 所以这里挂的是 /v1/... 与 /healthz。
func (s *Server) Router() *gin.Engine {
	if s.cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), requestLog(), cors())

	r.GET("/healthz", s.health)

	v1 := r.Group("/v1")
	{
		a := v1.Group("/auth")
		a.POST("/login", s.login)
		a.POST("/refresh", s.refresh)
		a.POST("/logout", s.logout)

		// 以下都需要 access token
		priv := v1.Group("")
		priv.Use(s.requireAuth())
		priv.Use(s.auditMutatingActions())
		{
			priv.GET("/me", s.me)

			priv.GET("/tasks", s.listTasks)
			priv.POST("/tasks", s.requireRole("operator"), s.createTask)
			priv.GET("/tasks/:id", s.getTask)

			priv.GET("/clusters", s.listClusters)
			priv.POST("/clusters", s.requireRole("operator"), s.createCluster)

			priv.GET("/system/queue", s.queueStats)
		}
	}
	return r
}

// ---------- 中间件 ----------

func requestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		// M0 只打一行，结构化日志等接入 VictoriaLogs 时再换
		_, _ = gin.DefaultWriter.Write([]byte(
			c.Request.Method + " " + c.Request.URL.Path +
				" " + itoa(c.Writer.Status()) +
				" " + time.Since(start).Truncate(time.Millisecond).String() + "\n"))
	}
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 同域经 Nginx 反代，正常不需要 CORS；这里只放开本地调试
		if origin := c.GetHeader("Origin"); strings.HasSuffix(origin, ":5173") {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

type currentUser struct {
	ID       string
	Username string
	Role     string
	Ver      int
}

func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			fail(c, http.StatusUnauthorized, codeUnauthorized, "缺少 Bearer 令牌")
			return
		}
		claims, err := auth.ParseAccessToken(s.cfg.JWTSecret, strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			fail(c, http.StatusUnauthorized, codeUnauthorized, "令牌无效或已过期")
			return
		}

		// token_version 校验：改角色/踢人时 +1，旧 token 立刻失效（ADR-34）
		var ver int
		var disabled bool
		err = s.db.Pool.QueryRow(c, `select token_version, disabled from users where id = $1::uuid`, claims.Subject).
			Scan(&ver, &disabled)
		if err != nil || disabled || ver != claims.Ver {
			fail(c, http.StatusUnauthorized, codeUnauthorized, "会话已失效，请重新登录")
			return
		}

		c.Set("user", currentUser{ID: claims.Subject, Username: claims.Username, Role: claims.Role, Ver: claims.Ver})
		c.Next()
	}
}

func (s *Server) requireRole(min string) gin.HandlerFunc {
	rank := map[string]int{"viewer": 1, "operator": 2, "admin": 3}
	return func(c *gin.Context) {
		u := mustUser(c)
		if rank[u.Role] < rank[min] {
			fail(c, http.StatusForbidden, codeForbidden, "权限不足，需要 "+min)
			return
		}
		c.Next()
	}
}

// auditMutatingActions：写操作先落 intent，失败即拒绝执行（fail-closed，ADR-29）
func (s *Server) auditMutatingActions() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}

		u := mustUser(c)
		trace, err := s.audit.Intent(c, u.ID, u.Username,
			c.Request.Method+" "+c.FullPath(), c.Param("id"), nil)
		if err != nil {
			fail(c, http.StatusServiceUnavailable, codeUnavailable, "审计不可用，操作已拒绝")
			return
		}
		c.Set("trace_id", trace)
		c.Next()

		status := "ok"
		if c.Writer.Status() >= 400 {
			status = "error"
		}
		// 用 Background：即使客户端断开也要把结果补上
		_ = s.audit.Result(context.Background(), trace, status, map[string]any{
			"http_status": c.Writer.Status(),
		})
	}
}

func mustUser(c *gin.Context) currentUser {
	v, _ := c.Get("user")
	u, _ := v.(currentUser)
	return u
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
