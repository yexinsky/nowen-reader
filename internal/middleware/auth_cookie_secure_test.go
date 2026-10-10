package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Secure 标志必须跟随连接的实际安全性：经反代/Cloudflare 的 HTTPS 请求带上，
// 局域网明文直连不带——否则局域网明文登录下发的 Cookie 会被浏览器拒收，
// 表现为「登录成功但保持不住」。
func TestSessionCookieSecureFlagFollowsRequestScheme(t *testing.T) {
	cases := []struct {
		name       string
		proto      string
		wantSecure bool
	}{
		{"https via reverse proxy", "https", true},
		{"plain http LAN direct", "", false},
		{"http forwarded by proxy", "http", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest("POST", "/api/auth/login", nil)
			if tc.proto != "" {
				context.Request.Header.Set("X-Forwarded-Proto", tc.proto)
			}

			SetSessionCookie(context, "session-token")

			cookies := recorder.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("session cookies = %d, want 1", len(cookies))
			}
			if cookies[0].Secure != tc.wantSecure {
				t.Fatalf("cookie Secure = %v, want %v (X-Forwarded-Proto=%q)",
					cookies[0].Secure, tc.wantSecure, tc.proto)
			}
			if !cookies[0].HttpOnly {
				t.Fatal("session cookie must remain HttpOnly")
			}
		})
	}
}

// 清 Cookie 必须与下发使用同一 Secure 判定，否则明文请求上的登出指令会被
// 浏览器忽略，表现为「登出后仍是登录状态」。
func TestClearSessionCookieSecureFlagFollowsRequestScheme(t *testing.T) {
	cases := []struct {
		name       string
		proto      string
		wantSecure bool
	}{
		{"https via reverse proxy", "https", true},
		{"plain http LAN direct", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest("POST", "/api/auth/logout", nil)
			if tc.proto != "" {
				context.Request.Header.Set("X-Forwarded-Proto", tc.proto)
			}

			ClearSessionCookie(context)

			found := false
			for _, cookie := range recorder.Result().Cookies() {
				if cookie.Name == SessionCookie && cookie.MaxAge < 0 {
					found = true
					if cookie.Secure != tc.wantSecure {
						t.Fatalf("cleared cookie Secure = %v, want %v", cookie.Secure, tc.wantSecure)
					}
				}
			}
			if !found {
				t.Fatal("no cleared session cookie emitted")
			}
		})
	}
}

// 直接构造的测试上下文没有 Request，此时不能 panic，按不安全处理。
func TestIsRequestSecureWithNilRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = nil

	if IsRequestSecure(context) {
		t.Fatal("nil request must be treated as insecure")
	}
}
