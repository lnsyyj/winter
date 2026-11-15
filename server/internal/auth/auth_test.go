package auth

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatalf("哈希口令失败: %v", err)
	}
	if hash == "s3cret-pass" {
		t.Fatal("口令不能以明文保存")
	}

	cases := []struct {
		name     string
		password string
		want     bool
	}{
		{"正确口令", "s3cret-pass", true},
		{"错误口令", "wrong", false},
		{"空口令", "", false},
		{"前缀相同不算命中", "s3cret-pass-extra", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckPassword(hash, tc.password); got != tc.want {
				t.Fatalf("CheckPassword(%q) = %v，期望 %v", tc.password, got, tc.want)
			}
		})
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	token, err := NewAccessToken(testSecret, 15*time.Minute, "user-1", "alice", "admin", 3)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	claims, err := ParseAccessToken(testSecret, token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.Subject != "user-1" || claims.Username != "alice" ||
		claims.Role != "admin" || claims.Ver != 3 {
		t.Fatalf("claims 与签发内容不一致: %+v", claims)
	}
	if claims.Issuer != "winter" || len(claims.Audience) != 1 || claims.Audience[0] != "winter-platform" {
		t.Fatalf("issuer/audience 不正确: %+v", claims)
	}
	if claims.ExpiresAt.Time.Before(time.Now()) {
		t.Fatal("token 不应已过期")
	}
}

func TestAccessTokenRejectsInvalid(t *testing.T) {
	valid, err := NewAccessToken(testSecret, 15*time.Minute, "user-1", "alice", "admin", 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	expired, err := NewAccessToken(testSecret, -time.Minute, "user-1", "alice", "admin", 0)
	if err != nil {
		t.Fatalf("签发过期 token 失败: %v", err)
	}
	tampered := valid[:len(valid)-2] + "xx"

	cases := []struct {
		name   string
		token  string
		secret string
	}{
		{"错误密钥", valid, strings.Repeat("x", 32)},
		{"签名被篡改", tampered, testSecret},
		{"已过期", expired, testSecret},
		{"空串", "", testSecret},
		{"完全不是 JWT", "not-a-jwt", testSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseAccessToken(tc.secret, tc.token); err == nil {
				t.Fatal("无效 token 必须被拒绝")
			}
		})
	}
}

func TestRefreshToken(t *testing.T) {
	plain1, hash1, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("生成刷新令牌失败: %v", err)
	}
	if plain1 == hash1 {
		t.Fatal("明文与哈希不能相同")
	}
	if len(hash1) != 64 {
		t.Fatalf("sha256 十六进制长度应为 64，实际 %d", len(hash1))
	}
	if hash1 != HashRefreshToken(plain1) {
		t.Fatal("同一明文的哈希必须稳定")
	}

	plain2, hash2, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("第二次生成失败: %v", err)
	}
	if plain1 == plain2 || hash1 == hash2 {
		t.Fatal("两次生成的刷新令牌不能相同")
	}
	if hash1 == HashRefreshToken(plain1+"x") {
		t.Fatal("不同明文不能撞同一个哈希")
	}
}
