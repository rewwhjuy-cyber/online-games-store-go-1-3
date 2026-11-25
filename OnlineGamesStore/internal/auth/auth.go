package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func HashPassword(p string) (string, error) {
	b, e := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), e
}
func CheckPassword(h, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(p)) == nil
}
func NewAccess(secret string, ttl time.Duration, id int64, role string) (string, error) {
	c := Claims{UserID: id, Role: role, RegisteredClaims: jwt.RegisteredClaims{Issuer: "online-games-store", Audience: []string{"online-games-store-api"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)), IssuedAt: jwt.NewNumericDate(time.Now()), Subject: fmt.Sprint(id)}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}
func ParseAccess(secret, token string) (*Claims, error) {
	c := &Claims{}
	t, e := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	}, jwt.WithIssuer("online-games-store"), jwt.WithAudience("online-games-store-api"))
	if e != nil || !t.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}
func NewRefresh() (string, string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", "", e
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}
func HashToken(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func Bearer(h string) string {
	p := strings.SplitN(h, " ", 2)
	if len(p) == 2 && strings.EqualFold(p[0], "Bearer") {
		return p[1]
	}
	return ""
}
func Encode(v any) string { b, _ := json.Marshal(v); return string(b) }
