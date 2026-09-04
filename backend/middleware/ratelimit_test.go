package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newTestRouter(l *Limiter) *gin.Engine {
	r := gin.New()
	r.GET("/x", l.Middleware(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.POST("/heavy", l.Strict(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestLimiterAllowsBurstThenDenies(t *testing.T) {
	l := New(1, 3, 1, 1)
	r := newTestRouter(l)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header must be set")
	}
}

func TestStrictAndNormalAreSeparateBuckets(t *testing.T) {
	l := New(100, 100, 0.001, 1)
	r := newTestRouter(l)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/heavy", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("first strict: want 200, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/heavy", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second strict: want 429, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("normal must be unaffected: got %d", w.Code)
	}
}

func TestCleanupRemovesIdle(t *testing.T) {
	l := New(1, 2, 1, 2)
	l.allow("1.1.1.1|n", 1, 2)
	l.allow("2.2.2.2|n", 1, 2)
	l.mu.Lock()
	l.buckets["2.2.2.2|n"].last = time.Now().Add(-time.Hour)
	l.mu.Unlock()

	if n := l.Cleanup(10 * time.Minute); n != 1 {
		t.Fatalf("want 1 removed, got %d", n)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.buckets["1.1.1.1|n"]; !ok {
		t.Fatal("active bucket must survive cleanup")
	}
	if _, ok := l.buckets["2.2.2.2|n"]; ok {
		t.Fatal("idle bucket must be removed")
	}
}
