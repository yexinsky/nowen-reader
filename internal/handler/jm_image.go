package handler

// JM 图片代理端点(契约:MOBILE_API.md #16 GET /api/image):
// 下载 → 乱序还原 → LRU 缓存 → 统一 image/jpeg 二进制输出。
// 管线实现见 internal/jm/image.go,本文件只做参数校验与组装。

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/jm"
)

var jmScrambleRegex = regexp.MustCompile(`^(0|[1-9]\d*)$`)

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
			jmService().ImageHTTPClient(),
			jmService().ImageCache(),
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
