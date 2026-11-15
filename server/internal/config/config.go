// Package config 只从环境变量读配置，不读配置文件。
// 理由（ADR-17）：容器化交付时配置由编排层注入，避免「配置在哪一份里」的歧义。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	AuditDBURL  string
	JWTSecret   string

	AccessTTL  time.Duration
	RefreshTTL time.Duration

	// 首次启动时创建的本地管理员（break-glass，ADR-34）
	AdminUser     string
	AdminPassword string

	Env string
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr:    env("LISTEN_ADDR", "127.0.0.1:8081"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		AuditDBURL:    env("AUDIT_DATABASE_URL", os.Getenv("DATABASE_URL")),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		AdminUser:     env("ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		Env:           env("APP_ENV", "dev"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL 未设置")
	}
	if len(c.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET 至少 32 字符（当前 %d）", len(c.JWTSecret))
	}

	var err error
	if c.AccessTTL, err = durationEnv("ACCESS_TTL", 15*time.Minute); err != nil {
		return nil, err
	}
	if c.RefreshTTL, err = durationEnv("REFRESH_TTL", 7*24*time.Hour); err != nil {
		return nil, err
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func durationEnv(k string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	// 兼容「15」这种纯秒数写法
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(v)
}
