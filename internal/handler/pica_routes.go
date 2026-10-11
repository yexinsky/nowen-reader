package handler

// PicACG(哔咔漫画)在线源(内置 Go 实现)路由与公共设施。
//
// 与 JM 流同构的 {code,msg,data} 信封与错误码语义(复用 jmOK/jmFail);
// 差异:哔咔会话是无状态 JWT——前端经 X-Pica-Token 头携带上游 token,
// 服务端不建会话表,登出即前端丢弃。settings 仍仅需 nowen 登录
// (代理配置先于哔咔登录,避免引导死锁)。

import (
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/config"
	"github.com/nowen-reader/nowen-reader/internal/middleware"
	"github.com/nowen-reader/nowen-reader/internal/pica"
)

const picaServiceVersion = "1.0.0"

// picaRuntime 进程级单例:惰性初始化,启动零外呼。
var picaRuntime struct {
	once sync.Once
	svc  *pica.Service
}

func picaService() *pica.Service {
	picaRuntime.once.Do(func() {
		picaRuntime.svc = pica.NewService(pica.DefaultDataDir(config.DataDir()), os.Getenv("PICA_UPSTREAM_PROXY"))
	})
	return picaRuntime.svc
}

// picaTokenHeader 哔咔上游 JWT 专用请求头(不用 Authorization,原因同 X-JM-Token)。
const picaTokenHeader = "X-Pica-Token"

// picaBearer 提取哔咔上游 token;未携带返回空串(内容端点匿名可用,调用方自判)。
func picaBearer(c *gin.Context) string { return c.GetHeader(picaTokenHeader) }

// picaFailErr 错误 → 分类 → 信封(*pica.APIError 直通;其余按上游/网络分类)。
func picaFailErr(c *gin.Context, err error, defaultMsg string) {
	var apiErr *pica.APIError
	if e, ok := err.(*pica.APIError); ok {
		apiErr = e
	} else {
		apiErr = pica.ClassifyError(err, defaultMsg)
	}
	jmFail(c, apiErr.Code, apiErr.Msg, apiErr.Data)
}

// picaClient 当前设置下的上游客户端(代理/画质变更自动重建)。
func picaClient(c *gin.Context) (*pica.Client, bool) {
	cl, err := picaService().APIClient()
	if err != nil {
		picaFailErr(c, err, "客户端初始化失败")
		return nil, false
	}
	return cl, true
}

func registerPicaRoutes(api *gin.RouterGroup) {
	picaGroup := api.Group("/pica")
	picaGroup.Use(middleware.AuthRequired())
	{
		registerPicaAuthRoutes(picaGroup)    // 本文件:health + auth + settings + history
		registerPicaContentRoutes(picaGroup) // pica_content.go:浏览/搜索/详情/章节/评论
		registerPicaImageRoutes(picaGroup)   // pica_image.go:图片代理
		registerPicaAccountRoutes(picaGroup) // pica_account.go:收藏/爱心/打卡
		registerPicaDownloadRoutes(picaGroup) // pica_download.go:批量下载(私有扩展)
	}
}

func registerPicaAuthRoutes(g *gin.RouterGroup) {
	// GET /api/pica/health — 健康检查/模式探测(零上游调用)
	g.GET("/health", func(c *gin.Context) {
		jmOK(c, gin.H{
			"status":   "ok",
			"version":  picaServiceVersion,
			"upstream": "picacg-api-2.2.1.3.3.4",
		})
	})

	// POST /api/pica/auth/login — 代理上游 sign-in,返回 {token, userInfo}
	g.POST("/auth/login", func(c *gin.Context) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
			return
		}
		if body.Email == "" || body.Password == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "email/password 必填"})
			return
		}
		cl, ok := picaClient(c)
		if !ok {
			return
		}
		token, userInfo, err := cl.SignIn(c.Request.Context(), body.Email, body.Password)
		if err != nil {
			picaFailErr(c, err, "登录请求失败")
			return
		}
		jmOK(c, gin.H{"token": token, "userInfo": userInfo})
	})

	// GET /api/pica/auth/profile — 实时拉取用户资料(token 由前端透传)
	g.GET("/auth/profile", func(c *gin.Context) {
		token := picaBearer(c)
		if token == "" {
			jmFail(c, pica.CodeUnauthorized, "登录已失效,请重新登录", nil)
			return
		}
		cl, ok := picaClient(c)
		if !ok {
			return
		}
		user, err := cl.Profile(c.Request.Context(), token)
		if err != nil {
			picaFailErr(c, err, "获取用户资料失败")
			return
		}
		jmOK(c, user)
	})

	// POST /api/pica/auth/logout — 哔咔无服务端会话,仅确认响应(前端丢弃 token)
	g.POST("/auth/logout", func(c *gin.Context) {
		jmOK(c, gin.H{"ok": true})
	})

	// GET|PUT /api/pica/settings — 服务端设置(仅 nowen 登录;与 JM 同款引导死锁规避)
	g.GET("/settings", func(c *gin.Context) {
		st := picaService().Settings()
		jmOK(c, gin.H{
			"proxy": st.Proxy, "imageQuality": st.ImageQuality,
			"downloadDir": st.DownloadDir, "downloadTags": st.DownloadTags,
		})
	})
	g.PUT("/settings", func(c *gin.Context) {
		var body struct {
			Proxy        *string `json:"proxy"`
			ImageQuality *string `json:"imageQuality"`
			DownloadDir  *string `json:"downloadDir"`
			DownloadTags *bool   `json:"downloadTags"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
			return
		}
		svc := picaService()
		st := svc.Settings()
		if body.Proxy != nil {
			st.Proxy = *body.Proxy // 空串 = 清除代理(直连)
		}
		if body.ImageQuality != nil {
			if !pica.ValidImageQuality(*body.ImageQuality) {
				c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "imageQuality 必须为 original|low|medium|high"})
				return
			}
			st.ImageQuality = *body.ImageQuality
		}
		if body.DownloadDir != nil {
			st.DownloadDir = *body.DownloadDir
		}
		if body.DownloadTags != nil {
			st.DownloadTags = *body.DownloadTags
		}
		if err := svc.SaveSettings(st); err != nil {
			picaFailErr(c, err, "保存设置失败")
			return
		}
		st = svc.Settings()
		jmOK(c, gin.H{
			"proxy": st.Proxy, "imageQuality": st.ImageQuality,
			"downloadDir": st.DownloadDir, "downloadTags": st.DownloadTags,
		})
	})

	// GET|POST|DELETE /api/pica/history — 源内阅读历史(本地持久化)
	g.GET("/history", func(c *gin.Context) {
		page, valid := pica.ParsePageQuery(c.Query("page"))
		if !valid {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "page 参数不合法"})
			return
		}
		list, total, hasNext := picaService().Store.ListHistory(page)
		jmOK(c, gin.H{"list": list, "page": page, "total": total, "hasNext": hasNext})
	})
	g.POST("/history", func(c *gin.Context) {
		var body struct {
			ID         string  `json:"id"`
			Title      string  `json:"title"`
			CoverURL   string  `json:"coverUrl"`
			Order      int     `json:"order"`
			EpTitle    *string `json:"epTitle"`
			ImageIndex *int    `json:"imageIndex"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.ID == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "id 必填"})
			return
		}
		if body.Order < 1 {
			body.Order = 1
		}
		idx := 1
		if body.ImageIndex != nil && *body.ImageIndex > 0 {
			idx = *body.ImageIndex
		}
		item := pica.HistoryItem{
			ID: body.ID, Title: body.Title, CoverURL: body.CoverURL,
			Order: body.Order, EpTitle: body.EpTitle, ImageIndex: idx,
			UpdatedAt: time.Now().Format("2006-01-02T15:04:05"),
		}
		if err := picaService().Store.ReportHistory(item); err != nil {
			picaFailErr(c, err, "保存历史失败")
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
	g.DELETE("/history", func(c *gin.Context) {
		if err := picaService().Store.ClearHistory(); err != nil {
			picaFailErr(c, err, "清空历史失败")
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
}
