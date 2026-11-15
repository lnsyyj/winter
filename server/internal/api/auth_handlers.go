package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnsyyj/winter/server/internal/auth"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// POST /v1/auth/login
func (s *Server) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, codeBadRequest, "用户名与口令必填")
		return
	}

	var (
		id, hash, role string
		ver            int
		disabled       bool
	)
	err := s.db.Pool.QueryRow(c,
		`select id::text, password_hash, role, token_version, disabled from users where username = $1`,
		req.Username).Scan(&id, &hash, &role, &ver, &disabled)
	if err != nil || disabled || !auth.CheckPassword(hash, req.Password) {
		// 不区分「用户不存在」与「口令错」，避免枚举
		fail(c, http.StatusUnauthorized, codeUnauthorized, "用户名或口令不正确")
		return
	}

	access, err := auth.NewAccessToken(s.cfg.JWTSecret, s.cfg.AccessTTL, id, req.Username, role, ver)
	if err != nil {
		fail(c, http.StatusInternalServerError, codeInternal, "签发令牌失败")
		return
	}

	plain, digest, err := auth.NewRefreshToken()
	if err != nil {
		fail(c, http.StatusInternalServerError, codeInternal, "生成刷新令牌失败")
		return
	}
	expires := time.Now().Add(s.cfg.RefreshTTL)
	if _, err := s.db.Pool.Exec(c,
		`insert into refresh_tokens (user_id, token_hash, expires_at) values ($1::uuid, $2, $3)`,
		id, digest, expires); err != nil {
		fail(c, http.StatusInternalServerError, codeInternal, "保存刷新令牌失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"refresh_token": plain,
		"token_type":    "Bearer",
		"expires_in":    int(s.cfg.AccessTTL.Seconds()),
		"user":          gin.H{"id": id, "username": req.Username, "role": role},
	})
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// POST /v1/auth/refresh
func (s *Server) refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, codeBadRequest, "refresh_token 必填")
		return
	}
	digest := auth.HashRefreshToken(req.RefreshToken)

	var (
		id, username, role string
		ver                int
	)
	err := s.db.Pool.QueryRow(c, `
		select u.id::text, u.username, u.role, u.token_version
		from refresh_tokens rt
		join users u on u.id = rt.user_id
		where rt.token_hash = $1 and rt.revoked_at is null and rt.expires_at > now() and u.disabled = false`,
		digest).Scan(&id, &username, &role, &ver)
	if err != nil {
		fail(c, http.StatusUnauthorized, codeUnauthorized, "刷新令牌无效或已过期")
		return
	}

	access, err := auth.NewAccessToken(s.cfg.JWTSecret, s.cfg.AccessTTL, id, username, role, ver)
	if err != nil {
		fail(c, http.StatusInternalServerError, codeInternal, "签发令牌失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(s.cfg.AccessTTL.Seconds()),
	})
}

// POST /v1/auth/logout
func (s *Server) logout(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, codeBadRequest, "refresh_token 必填")
		return
	}
	if _, err := s.db.Pool.Exec(c,
		`update refresh_tokens set revoked_at = now() where token_hash = $1 and revoked_at is null`,
		auth.HashRefreshToken(req.RefreshToken)); err != nil {
		fail(c, http.StatusInternalServerError, codeInternal, "注销失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

// GET /v1/me
func (s *Server) me(c *gin.Context) {
	u := mustUser(c)
	c.JSON(http.StatusOK, gin.H{"id": u.ID, "username": u.Username, "role": u.Role})
}
