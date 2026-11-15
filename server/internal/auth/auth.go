// Package auth：口令哈希、JWT 签发与校验。
//
// 设计（ADR-34）：access 是短命 JWT（不查库），refresh 是不透明串（存哈希）。
// 撤销靠 users.token_version（全员失效）+ refresh_tokens.revoked_at（单会话）。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidToken = errors.New("令牌无效")

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Ver      int    `json:"ver"`
	jwt.RegisteredClaims
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// NewAccessToken 签发 access token
func NewAccessToken(secret string, ttl time.Duration, userID, username, role string, ver int) (string, error) {
	now := time.Now()
	claims := Claims{
		Username: username,
		Role:     role,
		Ver:      ver,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    "winter",
			Audience:  jwt.ClaimStrings{"winter-platform"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ParseAccessToken(secret, token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期签名算法: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithIssuer("winter"), jwt.WithAudience("winter-platform"))
	if err != nil {
		return nil, ErrInvalidToken
	}
	c, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	return c, nil
}

// NewRefreshToken 生成不透明串并返回 (明文, 哈希)
// 明文只返回给客户端一次；库里只存哈希，泄库也无法直接使用。
func NewRefreshToken() (plain, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	return plain, HashRefreshToken(plain), nil
}

func HashRefreshToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
