package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := newRateLimiter(5, time.Second, 5)

	// First 5 requests should be allowed
	for i := 0; i < 5; i++ {
		if !rl.allow("client1") {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied
	if rl.allow("client1") {
		t.Error("6th request should be denied (rate limit exceeded)")
	}

	// Different client should still be allowed
	if !rl.allow("client2") {
		t.Error("Different client should be allowed")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	rl := newRateLimiter(5, 50*time.Millisecond, 5)

	// Exhaust tokens
	for i := 0; i < 5; i++ {
		rl.allow("client1")
	}
	if rl.allow("client1") {
		t.Error("Should be rate limited")
	}

	// Wait for refill
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !rl.allow("client1") {
		t.Error("Should be allowed after refill")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	rl := newRateLimiter(5, time.Second, 5)
	rl.allow("old-client")

	// Manually set lastSeen to 10 minutes ago
	rl.mu.Lock()
	rl.visitors["old-client"].lastSeen = time.Now().Add(-10 * time.Minute)
	rl.mu.Unlock()

	// Run cleanup
	rl.cleanup()

	rl.mu.Lock()
	_, exists := rl.visitors["old-client"]
	rl.mu.Unlock()

	if exists {
		t.Error("Old client should be cleaned up")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	r := gin.New()

	// Very restrictive limiter for testing: 2 per second, burst 2
	limiter := newRateLimiter(2, time.Second, 2)
	r.Use(func(c *gin.Context) {
		if !limiter.allow(getClientIP(c)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests",
			})
			return
		}
		c.Next()
	})
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 3rd request should be rate limited
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("3rd request: expected 429, got %d", w.Code)
	}
}

func TestRateLimitAuthMiddleware(t *testing.T) {
	// Just verify it can be constructed without panic
	mw := RateLimitAuth()
	if mw == nil {
		t.Error("RateLimitAuth returned nil")
	}
}

func TestRateLimitStrictMiddleware(t *testing.T) {
	mw := RateLimitStrict()
	if mw == nil {
		t.Error("RateLimitStrict returned nil")
	}
}

// 登录失败计数按账号统计、与来源 IP 无关：换 IP 无法重置，
// 且只统计失败——正确口令不会被这个额度挡住（否则攻击者可以用错误口令
// 持续请求，把唯一管理员锁在门外）。
func TestLoginFailureLimiterPerAccount(t *testing.T) {
	const username = "account-under-test"
	ResetLoginFailures(username)
	t.Cleanup(func() { ResetLoginFailures(username) })

	for i := 0; i < 10; i++ {
		if !RecordLoginFailure(username) {
			t.Fatalf("failure %d should be within quota", i+1)
		}
	}
	if RecordLoginFailure(username) {
		t.Error("11th failure should exceed the per-account quota")
	}

	// 大小写变体共享同一配额，攻击者不能靠变体稀释
	if RecordLoginFailure(strings.ToUpper(username)) {
		t.Error("quota should be shared across username case variants")
	}

	// 其他账号各有独立配额
	if !RecordLoginFailure("another-account-under-test") {
		t.Error("different account should have its own quota")
	}

	// 登录成功清零后立即恢复
	ResetLoginFailures(username)
	if !RecordLoginFailure(username) {
		t.Error("quota should be restored after a successful login resets it")
	}
	ResetLoginFailures(strings.ToUpper(username))
}
