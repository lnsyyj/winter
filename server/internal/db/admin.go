package db

import (
	"context"
	"fmt"

	"github.com/lnsyyj/winter/server/internal/auth"
)

// EnsureAdmin 幂等创建 break-glass 本地管理员（ADR-21/ADR-34）。
//
// 目录服务故障时本地管理员仍要能登录，所以这个账号独立于 LDAP；
// 已存在则什么都不做——不能因为重启把运维改过的口令覆盖回去。
func (d *DB) EnsureAdmin(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return fmt.Errorf("管理员用户名与口令不能为空")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("哈希管理员口令: %w", err)
	}
	_, err = d.Pool.Exec(ctx, `
		insert into users (username, password_hash, role)
		values ($1, $2, 'admin')
		on conflict (username) do nothing`, username, hash)
	if err != nil {
		return fmt.Errorf("写入本地管理员: %w", err)
	}
	return nil
}
