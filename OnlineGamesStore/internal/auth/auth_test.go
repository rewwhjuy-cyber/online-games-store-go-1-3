package auth

import (
	"testing"
	"time"
)

func TestPasswordHashAndCheck(t *testing.T) {
	h, e := HashPassword("CorrectHorse9!")
	if e != nil {
		t.Fatal(e)
	}
	if !CheckPassword(h, "CorrectHorse9!") {
		t.Fatal("password should match")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}
func TestJWTClaims(t *testing.T) {
	token, e := NewAccess("test-secret-long-enough", time.Minute, 42, "CUSTOMER")
	if e != nil {
		t.Fatal(e)
	}
	c, e := ParseAccess("test-secret-long-enough", token)
	if e != nil {
		t.Fatal(e)
	}
	if c.UserID != 42 || c.Role != "CUSTOMER" {
		t.Fatalf("unexpected claims: %+v", c)
	}
	if _, e = ParseAccess("wrong-secret", token); e == nil {
		t.Fatal("wrong secret accepted")
	}
}
func TestRefreshHash(t *testing.T) {
	raw, h, e := NewRefresh()
	if e != nil {
		t.Fatal(e)
	}
	if raw == "" || h == "" || h == raw {
		t.Fatal("refresh token/hash invalid")
	}
	if HashToken(raw) != h {
		t.Fatal("hash is not deterministic")
	}
}
