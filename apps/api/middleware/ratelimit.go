package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// windowState tracks request count within a single fixed window for one key.
type windowState struct {
	count     int
	windowEnd time.Time
}

// fixedWindowLimiter is a per-key fixed-window rate limiter backed by a map.
// It is safe for concurrent use.  A background goroutine cleans up expired
// entries so the map does not grow without bound.
type fixedWindowLimiter struct {
	mu      sync.Mutex
	clients map[string]*windowState
	max     int
	window  time.Duration
}

func newFixedWindowLimiter(max int, window time.Duration) *fixedWindowLimiter {
	l := &fixedWindowLimiter{
		clients: make(map[string]*windowState),
		max:     max,
		window:  window,
	}
	go l.cleanup()
	return l
}

// allow returns true if the key is within the rate limit for the current window.
func (l *fixedWindowLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	ws, ok := l.clients[key]
	if !ok || now.After(ws.windowEnd) {
		l.clients[key] = &windowState{count: 1, windowEnd: now.Add(l.window)}
		return true
	}
	if ws.count >= l.max {
		return false
	}
	ws.count++
	return true
}

// cleanup removes entries whose window expired more than one window-length ago.
func (l *fixedWindowLimiter) cleanup() {
	for {
		time.Sleep(5 * time.Minute)
		l.mu.Lock()
		cutoff := time.Now().Add(-l.window)
		for key, ws := range l.clients {
			if ws.windowEnd.Before(cutoff) {
				delete(l.clients, key)
			}
		}
		l.mu.Unlock()
	}
}

// MutationRateLimit returns a Gin middleware that limits write/mutation
// endpoints to maxPerMin requests per IP per minute.  Callers that exceed the
// limit receive HTTP 429 with a Retry-After header.
func MutationRateLimit(maxPerMin int) gin.HandlerFunc {
	limiter := newFixedWindowLimiter(maxPerMin, time.Minute)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.allow(ip) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": "60s",
			})
			return
		}
		c.Next()
	}
}
