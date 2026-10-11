package handler

// PicACG 图片代理端点(GET /api/pica/image?fs=&path=):
// 白名单校验(fs 已知存储域 + path 字符集)→ 缓存 → 候选 URL 下载 → 原样字节输出。
// 管线实现见 internal/pica/image.go,本文件只做参数校验与组装。

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/pica"
)

func registerPicaImageRoutes(g *gin.RouterGroup) {
	// GET /image?fs=&path= — 图片代理(二进制,非 {code,msg,data} 包装)
	g.GET("/image", func(c *gin.Context) {
		fs := c.Query("fs")
		if fs == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "fs 必填"})
			return
		}
		imagePath := c.Query("path")
		if imagePath == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "path 必填"})
			return
		}
		imgClient, err := picaService().ImageHTTPClient()
		if err != nil {
			picaFailErr(c, err, "图片客户端初始化失败")
			return
		}
		data, contentType, hit, err := pica.FetchImage(
			c.Request.Context(),
			imgClient.HTTPClient(),
			picaService().ImageCache(),
			fs, imagePath,
			picaService().Settings().ImageQuality,
		)
		if err != nil {
			picaFailErr(c, err, "图片获取失败") // *APIError 直通:3001/2001/2002
			return
		}
		if hit {
			c.Header("X-Pica-Cache", "hit")
		} else {
			c.Header("X-Pica-Cache", "miss")
		}
		c.Data(http.StatusOK, contentType, data)
	})
}
