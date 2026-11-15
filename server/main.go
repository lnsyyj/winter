// Command winter-server 是管理面的唯一 HTTP 入口。
//
// 启动顺序刻意固定：配置 → 连接池 → 迁移 → 本地管理员 → 监听。
// 任何一步失败都不进入监听状态，避免出现「接口能访问但表不存在」的半成品实例。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lnsyyj/winter/server/internal/api"
	"github.com/lnsyyj/winter/server/internal/config"
	"github.com/lnsyyj/winter/server/internal/db"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("服务异常退出", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DatabaseURL, cfg.AuditDBURL)
	if err != nil {
		return fmt.Errorf("连接数据库: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}

	// break-glass 本地管理员（ADR-34）：只在首次启动时创建，不覆盖已有口令。
	if cfg.AdminPassword == "" {
		slog.Warn("ADMIN_PASSWORD 未设置，跳过本地管理员初始化")
	} else if err := database.EnsureAdmin(ctx, cfg.AdminUser, cfg.AdminPassword); err != nil {
		return fmt.Errorf("初始化本地管理员: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.New(cfg, database).Router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("Server 启动", "addr", cfg.ListenAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("监听失败: %w", err)
	case <-ctx.Done():
		slog.Info("收到退出信号，开始优雅关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭: %w", err)
	}
	slog.Info("Server 已停止")
	return nil
}
