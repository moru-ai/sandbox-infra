package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimitConfig defines the rate limit configuration for an endpoint.
type RateLimitConfig struct {
	// RequestsPerMinute is the number of requests allowed per minute per key.
	RequestsPerMinute int
	// BurstSize is the maximum number of requests that can be made in a burst.
	// If not set, defaults to RequestsPerMinute.
	BurstSize int
}

// rateLimiter stores rate limiters per key (e.g., per team or per IP).
type rateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*limiterEntry
	config   RateLimitConfig
	// cleanupInterval is how often to clean up expired limiters
	cleanupInterval time.Duration
	// limiterTTL is how long a limiter lives without being accessed
	limiterTTL time.Duration
}

type limiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// newRateLimiter creates a new rate limiter with the given config.
func newRateLimiter(config RateLimitConfig) *rateLimiter {
	burstSize := config.BurstSize
	if burstSize == 0 {
		burstSize = config.RequestsPerMinute
	}

	rl := &rateLimiter{
		limiters:        make(map[string]*limiterEntry),
		config:          config,
		cleanupInterval: 5 * time.Minute,
		limiterTTL:      10 * time.Minute,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// getLimiter returns the rate limiter for the given key.
func (rl *rateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.RLock()
	entry, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if exists {
		rl.mu.Lock()
		entry.lastAccess = time.Now()
		rl.mu.Unlock()
		return entry.limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists = rl.limiters[key]; exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	// Create new limiter
	// rate.Limit is events per second, so divide by 60
	burstSize := rl.config.BurstSize
	if burstSize == 0 {
		burstSize = rl.config.RequestsPerMinute
	}
	limiter := rate.NewLimiter(rate.Limit(float64(rl.config.RequestsPerMinute)/60.0), burstSize)
	rl.limiters[key] = &limiterEntry{
		limiter:    limiter,
		lastAccess: time.Now(),
	}

	return limiter
}

// cleanup removes expired limiters periodically.
func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, entry := range rl.limiters {
			if now.Sub(entry.lastAccess) > rl.limiterTTL {
				delete(rl.limiters, key)
			}
		}
		rl.mu.Unlock()
	}
}

// KeyFunc is a function that returns the rate limit key for a request.
// Common implementations: ByTeamID, ByIP.
type KeyFunc func(c *gin.Context) string

// ByTeamID returns the team ID from the context as the rate limit key.
// Falls back to client IP if no team ID is found.
func ByTeamID(c *gin.Context) string {
	// Try to get team ID from context (set by auth middleware)
	if teamID := c.GetString("team_id"); teamID != "" {
		return "team:" + teamID
	}
	// Fall back to IP
	return "ip:" + c.ClientIP()
}

// ByIP returns the client IP as the rate limit key.
func ByIP(c *gin.Context) string {
	return "ip:" + c.ClientIP()
}

// RateLimitMiddleware creates a rate limiting middleware with the given config.
// The keyFunc determines how requests are grouped for rate limiting.
func RateLimitMiddleware(config RateLimitConfig, keyFunc KeyFunc) gin.HandlerFunc {
	rl := newRateLimiter(config)

	return func(c *gin.Context) {
		key := keyFunc(c)
		limiter := rl.getLimiter(key)

		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"message": "Rate limit exceeded. Please try again later.",
			})
			return
		}

		c.Next()
	}
}

// RateLimitForMethod creates a rate limiting middleware that only applies to specific HTTP methods.
func RateLimitForMethod(config RateLimitConfig, keyFunc KeyFunc, methods ...string) gin.HandlerFunc {
	rl := newRateLimiter(config)
	methodSet := make(map[string]bool, len(methods))
	for _, m := range methods {
		methodSet[m] = true
	}

	return func(c *gin.Context) {
		// Only apply rate limiting for specified methods
		if !methodSet[c.Request.Method] {
			c.Next()
			return
		}

		key := keyFunc(c)
		limiter := rl.getLimiter(key)

		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"message": "Rate limit exceeded. Please try again later.",
			})
			return
		}

		c.Next()
	}
}

// FileAPIRateLimits defines rate limits for file API endpoints.
var FileAPIRateLimits = struct {
	List     RateLimitConfig
	Upload   RateLimitConfig
	Download RateLimitConfig
	Delete   RateLimitConfig
}{
	List: RateLimitConfig{
		RequestsPerMinute: 100,
		BurstSize:         20,
	},
	Upload: RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
	},
	Download: RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
	},
	Delete: RateLimitConfig{
		RequestsPerMinute: 30,
		BurstSize:         5,
	},
}
