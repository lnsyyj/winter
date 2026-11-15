package config

import (
	"strings"
	"testing"
	"time"
)

var managedEnvKeys = []string{
	"DATABASE_URL", "AUDIT_DATABASE_URL", "JWT_SECRET", "LISTEN_ADDR",
	"ADMIN_USER", "ADMIN_PASSWORD", "APP_ENV", "ACCESS_TTL", "REFRESH_TTL",
}

func TestLoad(t *testing.T) {
	secret := strings.Repeat("s", 32)
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(*testing.T, *Config)
	}{
		{
			name:    "缺少 DATABASE_URL 拒绝启动",
			env:     map[string]string{"JWT_SECRET": secret},
			wantErr: true,
		},
		{
			name:    "JWT_SECRET 少于 32 字符拒绝启动",
			env:     map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": "short"},
			wantErr: true,
		},
		{
			name: "最小配置走默认值",
			env:  map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": secret},
			check: func(t *testing.T, c *Config) {
				if c.ListenAddr != "127.0.0.1:8081" {
					t.Fatalf("默认监听地址 = %q", c.ListenAddr)
				}
				if c.AccessTTL != 15*time.Minute || c.RefreshTTL != 7*24*time.Hour {
					t.Fatalf("默认 TTL 不正确: %s / %s", c.AccessTTL, c.RefreshTTL)
				}
				if c.AuditDBURL != c.DatabaseURL {
					t.Fatal("未指定审计库时应复用业务库连接")
				}
				if c.Env != "dev" || c.AdminUser != "admin" {
					t.Fatalf("默认环境/管理员不正确: %s / %s", c.Env, c.AdminUser)
				}
			},
		},
		{
			name: "全部覆盖",
			env: map[string]string{
				"DATABASE_URL":       "postgres://biz",
				"AUDIT_DATABASE_URL": "postgres://audit",
				"JWT_SECRET":         secret,
				"LISTEN_ADDR":        "0.0.0.0:9000",
				"ADMIN_USER":         "root",
				"ADMIN_PASSWORD":     "pw",
				"APP_ENV":            "prod",
				"ACCESS_TTL":         "30",
				"REFRESH_TTL":        "48h",
			},
			check: func(t *testing.T, c *Config) {
				if c.AuditDBURL != "postgres://audit" || c.Env != "prod" ||
					c.AdminUser != "root" || c.AdminPassword != "pw" ||
					c.ListenAddr != "0.0.0.0:9000" {
					t.Fatalf("覆盖未生效: %+v", c)
				}
				if c.AccessTTL != 30*time.Second || c.RefreshTTL != 48*time.Hour {
					t.Fatalf("TTL 解析不正确: %s / %s", c.AccessTTL, c.RefreshTTL)
				}
			},
		},
		{
			name: "非法 TTL 拒绝启动",
			env: map[string]string{
				"DATABASE_URL": "postgres://x", "JWT_SECRET": secret, "ACCESS_TTL": "not-a-duration",
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range managedEnvKeys {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatal("期望报错但成功了")
				}
				return
			}
			if err != nil {
				t.Fatalf("加载配置失败: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestDurationEnv(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		def     time.Duration
		want    time.Duration
		wantErr bool
	}{
		{"空值取默认", "", time.Minute, time.Minute, false},
		{"纯秒数", "45", time.Minute, 45 * time.Second, false},
		{"Go duration", "2m30s", time.Minute, 150 * time.Second, false},
		{"非法值", "abc", time.Minute, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_TTL", tc.value)
			got, err := durationEnv("TEST_TTL", tc.def)
			if tc.wantErr {
				if err == nil {
					t.Fatal("期望报错但成功了")
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got != tc.want {
				t.Fatalf("= %s，期望 %s", got, tc.want)
			}
		})
	}
}
