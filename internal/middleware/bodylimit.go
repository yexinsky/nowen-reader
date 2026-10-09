package middleware

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/nowen-reader/nowen-reader/internal/config"
)

// 请求体上限按「路由」选择，不按 Content-Type 判断：
// gin 的 ShouldBindJSON 完全不看 Content-Type，若按请求头分档，攻击者只要给
// JSON 请求带上 multipart/form-data 头就能拿到上传档位的大额度，
// 未鉴权接口的内存放大面会原样保留。
const (
	smallBodyLimit   = 256 << 10 // 未鉴权可达的端点：登录/注册/站点设置/健康检查
	defaultBodyLimit = 16 << 20  // 需鉴权的 JSON 接口（封面 base64、AI 对话附页图等合法大字段）
	uploadBodyLimit  = 1 << 30   // 文件上传（需管理员/书库管理权限）
)

// uploadBodyPaths 是真正会收大 body 的上传路由；匹配前会先去掉 BASE_PATH。
// 注意：只有 POST /api/comics/:id/cover 会收文件（UpdateCover 的 multipart 分支），
// /thumbnail 只有 GET（无 body），不要写进来。
var uploadBodyPaths = []*regexp.Regexp{
	regexp.MustCompile(`^/api/upload$`),
	regexp.MustCompile(`^/api/site-settings/icon$`),
	regexp.MustCompile(`^/api/comics/[^/]+/cover$`),
}

// smallBodyPrefixes 是未鉴权可达、且只应收到小 JSON 的路由前缀。
var smallBodyPrefixes = []string{
	"/api/auth/",
	"/api/health",
	"/api/site-settings",
}

// BodyLimit 为请求体设置读取上限。上限只由路由决定，与请求头无关，
// 因此无法通过伪造 Content-Type 换取更大的额度。
func BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimitForPath(c.Request.URL.Path))
		}
		c.Next()
	}
}

// bodyLimitForPath 返回该请求路径对应的 body 上限。
// 注意 config.BasePath() 未配置时返回 "/"（不是空串），必须先排除这种情况，
// 否则会把前导斜杠一并删掉，导致所有路由都退化成默认档位。
func bodyLimitForPath(requestPath string) int64 {
	if basePath := config.BasePath(); basePath != "" && basePath != "/" {
		requestPath = strings.TrimPrefix(requestPath, basePath)
	}

	switch {
	case matchesAnyPath(uploadBodyPaths, requestPath):
		return uploadBodyLimit
	case hasAnyPrefix(smallBodyPrefixes, requestPath):
		return smallBodyLimit
	default:
		return defaultBodyLimit
	}
}

func matchesAnyPath(patterns []*regexp.Regexp, path string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(prefixes []string, path string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
