package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPaginationBounds(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/games?page=-1&limit=1000", nil)
	p, l := pagination(r)
	if p != 1 || l != 100 {
		t.Fatalf("got %d %d", p, l)
	}
}
func TestMoneyFormatting(t *testing.T) {
	if money(12345) != "123.45" || money(0) != "0.00" {
		t.Fatal("money formatting incorrect")
	}
}
func TestRateLimitReturns429(t *testing.T) {
	s := &Server{hits: map[string][]time.Time{}}
	h := s.rateLimit(httpHandlerOK())
	for i := 0; i < 121; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "127.0.0.1:1"
		h.ServeHTTP(w, r)
		if i == 120 && w.Code != 429 {
			t.Fatalf("expected 429 got %d", w.Code)
		}
	}
}
func httpHandlerOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
}
