package middleware

import (
	"net"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.Mutex
	limit    int
	window   time.Duration
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    5,           // Allow 5 requests
		window:   time.Minute, // In 1 minute
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// Remove old timestamps outside the window
	requests := rl.requests[ip]
	filteredRequests := []time.Time{}
	for _, t := range requests {
		if t.After(windowStart) {
			filteredRequests = append(filteredRequests, t)
		}
	}

	// Update the map with filtered requests
	rl.requests[ip] = filteredRequests

	// Check if request limit is exceeded
	if len(filteredRequests) >= rl.limit {
		return false
	}

	// Add current request timestamp
	rl.requests[ip] = append(rl.requests[ip], now)
	return true
}

func getIP(c *gin.Context) string {
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.ClientIP()
	}
	return ip
}

func RateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := getIP(c)
		if !rl.Allow(ip) {
			c.JSON(429, gin.H{"error": "Too many requests. Please wait."})
			c.Abort()
			return
		}
		c.Next()
	}
}
