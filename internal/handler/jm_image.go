package handler

// JM 图片代理端点(契约:MOBILE_API.md #16 GET /api/image):
// 下载 → 乱序还原 → LRU 缓存 → 统一 image/jpeg 二进制输出。
// 管线实现见 internal/jm/image.go,本文件只做参数校验与组装。

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/config"
	"github.com/nowen-reader/nowen-reader/internal/jm"
)

var (
	jmScrambleRegex = regexp.MustCompile(`^(0|[1-9]\d*)$`)

	jmImageCacheOnce sync.Once
	jmImageCacheInst *jm.ImageCache

	jmImageClientMu    sync.Mutex
	jmImageClient      *http.Client
	jmImageClientProxy string
)

// jmImageCacheInstance 图片落盘缓存单例:
// 目录 <DataDir>/jm/cache,文件数上限读环境变量 JM_IMAGE_CACHE_LIMIT(默认 500,最小 10)。
func jmImageCacheInstance() *jm.ImageCache {
	jmImageCacheOnce.Do(func() {
		limit := 500
		if v := os.Getenv("JM_IMAGE_CACHE_LIMIT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 10 {
				limit = n
			}
		}
		jmImageCacheInst = jm.NewImageCache(filepath.Join(config.DataDir(), "jm", "cache"), limit)
	})
	return jmImageCacheInst
}

// jmImageHTTPClient 图片下载客户端(代理取自服务端设置;代理变更自动重建)。
func jmImageHTTPClient() *http.Client {
	proxy := jmService().Settings().Proxy
	jmImageClientMu.Lock()
	defer jmImageClientMu.Unlock()
	if jmImageClient == nil || jmImageClientProxy != proxy {
		jmImageClient = jm.NewImageHTTPClient(proxy)
		jmImageClientProxy = proxy
	}
	return jmImageClient
}

func registerJMImageRoutes(g *gin.RouterGroup) {
	// #16 GET /image?path=&scramble=&aid= — 图片代理(二进制,非 {code,msg,data} 包装)
	g.GET("/image", func(c *gin.Context) {
		// path:必填 ≥1 字符,缺失 → 422;合法性(白名单/SSRF/穿越)由管线校验 → 3001
		imagePath := c.Query("path")
		if imagePath == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "path 必填"})
			return
		}
		// scramble:默认 "0",非负整数字符串,非法 → 422(契约)
		scramble := c.Query("scramble")
		if scramble == "" {
			scramble = "0"
		}
		if !jmScrambleRegex.MatchString(scramble) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "scramble 格式错误"})
			return
		}
		aid := c.Query("aid")

		data, hit, err := jm.DownloadAndDecode(
			c.Request.Context(),
			jmImageHTTPClient(),
			jmImageCacheInstance(),
			imagePath, scramble, aid,
			jmService().Settings().ImageQuality,
		)
		if err != nil {
			jmFailErr(c, err, "图片获取失败") // *APIError 直通:3001/2001/2002
			return
		}
		if hit {
			c.Header("X-JM-Cache", "hit")
		} else {
			c.Header("X-JM-Cache", "miss")
		}
		c.Data(http.StatusOK, "image/jpeg", data)
	})
}
