package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter — token bucket per-IP с двумя классами: обычный и строгий
// (тяжёлые ручки: создание задач, отправка input, cancel).
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
	srate   float64
	sburst  float64
}

func New(rate, burst, strictRate, strictBurst float64) *Limiter {
	return &Limiter{
		buckets: map[string]*bucket{},
		rate:    rate, burst: burst, srate: strictRate, sburst: strictBurst,
	}
}

func (l *Limiter) allow(key string, rate, burst float64) (bool, float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	now := time.Now()
	if b == nil {
		b = &bucket{tokens: burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, (1 - b.tokens) / rate
}

func (l *Limiter) deny(c *gin.Context, wait float64) {
	c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait))))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
}

// Middleware — мягкий общий лимит на все /api запросы.
func (l *Limiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ok, wait := l.allow(c.ClientIP()+"|n", l.rate, l.burst); !ok {
			l.deny(c, wait)
			return
		}
		c.Next()
	}
}

// Strict — строгий лимит для тяжёлых/опасных ручек.
func (l *Limiter) Strict() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ok, wait := l.allow(c.ClientIP()+"|s", l.srate, l.sburst); !ok {
			l.deny(c, wait)
			return
		}
		c.Next()
	}
}

// Cleanup удаляет неактивные лимитеры.
func (l *Limiter) Cleanup(idle time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for k, b := range l.buckets {
		if time.Since(b.last) > idle {
			delete(l.buckets, k)
			n++
		}
	}
	return n
}

// StartCleanup запускает фоновую очистку неактивных лимитеров.
func (l *Limiter) StartCleanup(interval, idle time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				l.Cleanup(idle)
			}
		}
	}()
}
