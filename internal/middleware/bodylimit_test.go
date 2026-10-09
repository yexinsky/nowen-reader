package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 精确断言每个路由命中的档位：用 300KB 这种「低于默认档、高于小额度档」的
// body 只能证明「不是小额度档」，无法区分默认档与上传档，因此这里直接断言
// 选中的上限值。
func TestBodyLimitForPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want int64
	}{
		{"login", "/api/auth/login", smallBodyLimit},
		{"register", "/api/auth/register", smallBodyLimit},
		{"site-settings", "/api/site-settings", smallBodyLimit},
		{"health", "/api/health", smallBodyLimit},
		{"authenticated-json", "/api/comics/abc", defaultBodyLimit},
		{"chapter", "/api/comics/abc/chapter/0", defaultBodyLimit},
		{"upload", "/api/upload", uploadBodyLimit},
		{"site-icon-upload", "/api/site-settings/icon", uploadBodyLimit},
		{"cover-upload", "/api/comics/abc123/cover", uploadBodyLimit},
		// /thumbnail 只有 GET（无 body），不应占大额度档
		{"thumbnail-get", "/api/comics/abc123/thumbnail", defaultBodyLimit},
		{"cover-other", "/api/comics/abc123/cover/extra", defaultBodyLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyLimitForPath(tc.path); got != tc.want {
				t.Errorf("bodyLimitForPath(%q) = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

// BASE_PATH 未配置时 config.BasePath() 返回 "/"，此时不能把前导斜杠删掉，
// 否则所有路由都会退化成默认档位（上传会被 16MB 截断、登录不再受小额度保护）。
func TestBodyLimitForPathWithoutBasePath(t *testing.T) {
	t.Setenv("BASE_PATH", "")

	if got := bodyLimitForPath("/api/auth/login"); got != smallBodyLimit {
		t.Errorf("login without BASE_PATH = %d, want %d", got, int64(smallBodyLimit))
	}
	if got := bodyLimitForPath("/api/upload"); got != uploadBodyLimit {
		t.Errorf("upload without BASE_PATH = %d, want %d", got, int64(uploadBodyLimit))
	}
}

func TestBodyLimitForPathWithBasePath(t *testing.T) {
	t.Setenv("BASE_PATH", "/reader")

	if got := bodyLimitForPath("/reader/api/auth/login"); got != smallBodyLimit {
		t.Errorf("login under BASE_PATH = %d, want %d", got, int64(smallBodyLimit))
	}
	if got := bodyLimitForPath("/reader/api/upload"); got != uploadBodyLimit {
		t.Errorf("upload under BASE_PATH = %d, want %d", got, int64(uploadBodyLimit))
	}
}

// runBodyLimit 经 BodyLimit 中间件发送一个指定大小的 POST，
// 返回被读取的字节数以及读取是否因超出上限而失败。
func runBodyLimit(t *testing.T, path, contentType string, size int) (int64, bool) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit())

	var read int64
	var readErr error
	r.POST("/*rest", func(c *gin.Context) {
		read, readErr = io.Copy(io.Discard, c.Request.Body)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("A", size)))
	req.Header.Set("Content-Type", contentType)
	r.ServeHTTP(httptest.NewRecorder(), req)
	return read, readErr != nil
}

// 伪造 Content-Type 不能换到更大的额度：gin 的 ShouldBindJSON 完全不看
// Content-Type，若按请求头分档，给 JSON 请求加一个 multipart/form-data 头
// 就能拿到上传档位的大额度。
func TestBodyLimitIgnoresSpoofedContentType(t *testing.T) {
	const size = 300 << 10 // 高于未鉴权端点的 256KB 额度

	read, truncated := runBodyLimit(t, "/api/auth/login", "multipart/form-data; boundary=x", size)
	if !truncated || read >= int64(size) {
		t.Errorf("spoofed multipart on an auth path must still be capped: read=%d truncated=%v", read, truncated)
	}

	if _, truncated := runBodyLimit(t, "/api/auth/login", "application/json", size); !truncated {
		t.Error("JSON body on an auth path must be capped")
	}
}

// 合法的大 body 不能被误伤：鉴权 JSON 接口与上传路由都要放行。
func TestBodyLimitAllowsLegitimateBodies(t *testing.T) {
	const size = 300 << 10

	if _, truncated := runBodyLimit(t, "/api/comics/abc", "application/json", size); truncated {
		t.Error("authenticated JSON endpoints should allow a 300KB body")
	}
	if _, truncated := runBodyLimit(t, "/api/upload", "multipart/form-data; boundary=x", size); truncated {
		t.Error("upload endpoint should allow a 300KB body")
	}
}
