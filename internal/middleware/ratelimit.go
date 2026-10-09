package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// rateLimiter implements a token bucket rate limiter per client IP.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // tokens per interval
	interval time.Duration // refill interval
	burst    int           // max tokens (bucket size)
}

type visitor struct {
	tokens   int
	lastSeen time.Time
}

// newRateLimiter creates a rate limiter.
//
//	rate: number of requests allowed per interval
//	interval: the time window
//	burst: maximum burst size
func newRateLimiter(rate int, interval time.Duration, burst int) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		interval: interval,
		burst:    burst,
	}

	// Start cleanup goroutine — remove stale visitors every minute
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()

	return rl
}

// allow checks if a request from the given key is allowed.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[key]
	now := time.Now()

	if !exists {
		rl.visitors[key] = &visitor{
			tokens:   rl.burst - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(v.lastSeen)
	refill := int(elapsed / rl.interval) * rl.rate
	if refill > 0 {
		v.tokens += refill
		if v.tokens > rl.burst {
			v.tokens = rl.burst
		}
		v.lastSeen = now
	}

	if v.tokens <= 0 {
		return false
	}

	v.tokens--
	return true
}

// cleanup removes visitors not seen in the last 5 minutes.
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for key, v := range rl.visitors {
		if v.lastSeen.Before(cutoff) {
			delete(rl.visitors, key)
		}
	}
}

// reset 清除某个键的计数（例如登录成功后清零该账号的失败次数）。
func (rl *rateLimiter) reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.visitors, key)
}

// getClientIP extracts the client IP for rate limiting.
func getClientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if ip == "" {
		ip = c.RemoteIP()
	}
	return ip
}

// ============================================================
// Exported middleware constructors
// ============================================================

// RateLimit returns a general-purpose rate limit middleware.
// Default: 100 requests per second with a burst of 200.
func RateLimit() gin.HandlerFunc {
	limiter := newRateLimiter(100, time.Second, 200)
	return func(c *gin.Context) {
		if !limiter.allow(getClientIP(c)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please try again later.",
			})
			return
		}
		c.Next()
	}
}

// RateLimitStrict returns a stricter rate limiter for sensitive endpoints.
// Default: 10 requests per minute with a burst of 20.
func RateLimitStrict() gin.HandlerFunc {
	limiter := newRateLimiter(10, time.Minute, 20)
	return func(c *gin.Context) {
		if !limiter.allow(getClientIP(c)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please try again later.",
			})
			return
		}
		c.Next()
	}
}

// RateLimitAuth returns a rate limiter for auth endpoints (login/register).
// Default: 10 requests per minute with a burst of 20.
func RateLimitAuth() gin.HandlerFunc {
	limiter := newRateLimiter(10, time.Minute, 20)
	return func(c *gin.Context) {
		if !limiter.allow(getClientIP(c)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many login attempts. Please wait a moment.",
			})
			return
		}
		c.Next()
	}
}

// RateLimitLogin 对登录接口按来源 IP 限流，用于挡住 bcrypt 带来的 CPU 放大。
// 账号维度的防爆破不在这里做，而是由 handler 在「口令校验失败」后调用
// RecordLoginFailure 计数：这样正确口令永远不会被限流挡下（否则攻击者用
// 错误口令持续请求就能把唯一管理员锁在门外），也不会因中间件与 handler 的
// body 解析语义不一致（超长/带尾随字节的 JSON）而被静默绕过。
func RateLimitLogin() gin.HandlerFunc {
	ipLimiter := newRateLimiter(10, time.Minute, 20)

	return func(c *gin.Context) {
		if !ipLimiter.allow(getClientIP(c)) {
			abortLoginLimited(c)
			return
		}
		c.Next()
	}
}

func abortLoginLimited(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error": "Too many login attempts. Please wait a moment.",
	})
}

// loginFailureLimiter 按账号统计失败次数，与来源 IP 无关，
// 因此换 IP / 用代理池都无法重置：每个账号每分钟最多 10 次口令尝试。
var loginFailureLimiter = newRateLimiter(10, time.Minute, 10)

func loginAccountKey(username string) string {
	return "acct:" + strings.ToLower(strings.TrimSpace(username))
}

// RecordLoginFailure 记录一次口令校验失败；返回 false 表示该账号在窗口内的
// 失败额度已用尽，调用方应返回 429 而不是 401。
func RecordLoginFailure(username string) bool {
	return loginFailureLimiter.allow(loginAccountKey(username))
}

// ResetLoginFailures 在登录成功后清零该账号的失败计数。
func ResetLoginFailures(username string) {
	loginFailureLimiter.reset(loginAccountKey(username))
}
