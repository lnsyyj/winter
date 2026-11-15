// Package db 负责连接池与迁移。
//
// 两个池：业务库与审计库（ADR-29 要求独立库）。M0 允许两者指向同一实例，
// 但代码里始终走两个池，后续拆库时不需要改调用方。
package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var businessMigrations embed.FS

//go:embed migrations/audit/*.sql
var auditMigrations embed.FS

type DB struct {
	Pool  *pgxpool.Pool
	Audit *pgxpool.Pool
}

func Open(ctx context.Context, businessURL, auditURL string) (*DB, error) {
	bp, err := openPool(ctx, businessURL)
	if err != nil {
		return nil, fmt.Errorf("连接业务库: %w", err)
	}
	ap, err := openPool(ctx, auditURL)
	if err != nil {
		bp.Close()
		return nil, fmt.Errorf("连接审计库: %w", err)
	}
	return &DB{Pool: bp, Audit: ap}, nil
}

func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// 单副本默认 20 个连接（ADR-37：3 副本 × 20 远低于 PG max_connections）
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func (d *DB) Close() {
	if d.Pool != nil {
		d.Pool.Close()
	}
	if d.Audit != nil && d.Audit != d.Pool {
		d.Audit.Close()
	}
}

// Migrate 按文件名顺序执行嵌入的 SQL（幂等：全部 create ... if not exists）
func (d *DB) Migrate(ctx context.Context) error {
	if err := runMigrations(ctx, d.Pool, businessMigrations, "migrations"); err != nil {
		return fmt.Errorf("业务库迁移: %w", err)
	}
	if err := runMigrations(ctx, d.Audit, auditMigrations, "migrations/audit"); err != nil {
		return fmt.Errorf("审计库迁移: %w", err)
	}
	return nil
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, fs embed.FS, dir string) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		b, err := fs.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// Healthy 供 /healthz 使用：两个池都要能通
func (d *DB) Healthy(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := d.Pool.Ping(c); err != nil {
		return fmt.Errorf("业务库: %w", err)
	}
	if err := d.Audit.Ping(c); err != nil {
		return fmt.Errorf("审计库: %w", err)
	}
	return nil
}
