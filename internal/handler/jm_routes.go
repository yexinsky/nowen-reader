package handler

// JM 在线源(内置 Go 实现)路由与公共设施。
//
// 契约:docs/MOBILE_API.md(30 端点,{code,msg,data} 包装,业务失败 HTTP 200,1002→401)。
// 本文件持有服务单例与鉴权/信封助手,并注册 auth/settings/history 端点;
// content(浏览/搜索/详情/章节)与 image(图片代理)、account(收藏/评论/点赞/签到)
// 分别实现在 jm_content.go / jm_image.go / jm_account.go。

import (
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/config"
	"github.com/nowen-reader/nowen-reader/internal/jm"
	"github.com/nowen-reader/nowen-reader/internal/middleware"
)

const jmServiceVersion = "1.0.0"

// jmRuntime 进程级单例:惰性初始化,启动零外呼。
var jmRuntime struct {
	once sync.Once
	svc  *jm.Service
}

func jmService() *jm.Service {
	jmRuntime.once.Do(func() {
		jmRuntime.svc = jm.NewService(jm.DefaultDataDir(config.DataDir()), os.Getenv("JM_UPSTREAM_PROXY"))
	})
	return jmRuntime.svc
}

/* ── 响应信封(契约 §0.2) ── */

func jmOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
}

// jmFail 业务失败:HTTP 200 + 包装体;code=1002 时 HTTP 401(契约 §0.2)。
func jmFail(c *gin.Context, code int, msg string, data any) {
	status := http.StatusOK
	if code == jm.CodeUnauthorized {
		status = http.StatusUnauthorized
	}
	c.JSON(status, gin.H{"code": code, "msg": msg, "data": data})
}

// jmFailErr 错误 → 分类 → 信封(*jm.APIError 直通;其余按上游/网络分类)。
func jmFailErr(c *gin.Context, err error, defaultMsg string) {
	var apiErr *jm.APIError
	if e, ok := err.(*jm.APIError); ok {
		apiErr = e
	} else {
		apiErr = jm.ClassifyError(err, defaultMsg)
	}
	jmFail(c, apiErr.Code, apiErr.Msg, apiErr.Data)
}

// jmJMTokenHeader JM 会话令牌专用请求头。
// 不能用标准 Authorization:Bearer——nowen 的 AuthRequired 会把 Authorization
// 当作 API Key 校验且失败时不回退 Cookie(jm_routes 路由组挂了该中间件),
// 导致 JM 登录成功后所有带令牌的请求被 401 拦截、前端误判「登录已失效」。
const jmTokenHeader = "X-JM-Token"

// jmBearer 从 X-JM-Token 头提取 JM 会话令牌并查会话;无效时直接写 1002/401 响应
// 并返回 nil(调用方 return)。
func jmBearer(c *gin.Context) *jm.Session {
	svc := jmService()
	sess := svc.Sessions.Get(c.GetHeader(jmTokenHeader))
	if sess == nil {
		jmFail(c, jm.CodeUnauthorized, "登录已失效,请重新登录", nil)
		return nil
	}
	return sess
}

func registerJMRoutes(api *gin.RouterGroup) {
	jmGroup := api.Group("/jm")
	jmGroup.Use(middleware.AuthRequired())
	{
		registerJMAuthRoutes(jmGroup)   // 本文件:health + auth + settings + history
		registerJMContentRoutes(jmGroup) // jm_content.go:浏览/搜索/详情/章节
		registerJMImageRoutes(jmGroup)   // jm_image.go:图片代理
		registerJMAccountRoutes(jmGroup) // jm_account.go:收藏/评论/点赞/签到
		registerJMDownloadRoutes(jmGroup) // jm_download.go:批量下载(私有扩展)
	}
}

func registerJMAuthRoutes(g *gin.RouterGroup) {
	// #1 GET /api/health — 健康检查/模式探测(零上游调用)
	g.GET("/health", func(c *gin.Context) {
		jmOK(c, gin.H{
			"status":   "ok",
			"mock":     false,
			"version":  jmServiceVersion,
			"upstream": "jmcomic-2.6.17",
		})
	})

	// #2 GET /api/auth/captcha — 验证码图片(二进制,非包装)
	g.GET("/auth/captcha", func(c *gin.Context) {
		data, media, err := jmService().AnonClient().CaptchaBytes(c.Request.Context())
		if err != nil {
			jmFailErr(c, err, "获取验证码失败")
			return
		}
		c.Data(http.StatusOK, media, data)
	})

	// #3 POST /api/auth/login
	g.POST("/auth/login", func(c *gin.Context) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Captcha  string `json:"captcha"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
			return
		}
		if body.Username == "" || body.Password == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "username/password 必填"})
			return
		}
		svc := jmService()
		client := svc.NewSessionClient()
		userInfoRaw, cookies, err := client.Login(c.Request.Context(), body.Username, body.Password, body.Captcha)
		if err != nil {
			jmFailErr(c, err, "登录请求失败")
			return
		}
		userInfo := jm.UserInfoMap(userInfoRaw)
		if cookies["AVS"] == "" {
			// 上游登录成功却没拿到 AVS(响应无 Set-Cookie 且返回值无 s 字段):
			// 后续会员端点必 401,留下诊断线索
			log.Printf("[jm] login ok but no AVS cookie (upstream s missing?) user=%s", body.Username)
		}
		proxyKey := svc.Settings().Proxy
		sess := svc.Sessions.Create(cookies, userInfoRaw, proxyKey)
		jmOK(c, gin.H{"token": sess.Token, "userInfo": userInfo})
	})

	// #4 GET /api/auth/profile — 登录快照,不回源(契约 #4)
	g.GET("/auth/profile", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		jmOK(c, jm.UserInfoMap(sess.UserInfo))
	})

	// #5 POST /api/auth/logout — 销毁服务端会话
	g.POST("/auth/logout", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		jmService().Sessions.Delete(sess.Token)
		jmOK(c, gin.H{"ok": true})
	})

	// #27/#28 GET|PUT /api/settings — 服务端设置(本地)
	// 注意:代理是「内置服务的上游配置」而非 JM 账号数据,鉴权只用 nowen 登录
	//(组上 AuthRequired 已保证),不再要求 JM Bearer——否则未登录 JM 时
	// 无法配置代理,登录又依赖代理,形成引导死锁。
	g.GET("/settings", func(c *gin.Context) {
		st := jmService().Settings()
		jmOK(c, gin.H{
			"proxy": st.Proxy, "imageQuality": st.ImageQuality,
			"downloadDir": st.DownloadDir, "mock": false,
		})
	})
	g.PUT("/settings", func(c *gin.Context) {
		var body struct {
			Proxy        *string `json:"proxy"`
			ImageQuality *string `json:"imageQuality"`
			DownloadDir  *string `json:"downloadDir"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
			return
		}
		svc := jmService()
		st := svc.Settings()
		if body.Proxy != nil {
			st.Proxy = *body.Proxy // 空串 = 清除代理(直连)
		}
		if body.ImageQuality != nil {
			switch *body.ImageQuality {
			case "high", "medium", "low":
				st.ImageQuality = *body.ImageQuality
			default:
				c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "imageQuality 必须为 high|medium|low"})
				return
			}
		}
		if body.DownloadDir != nil {
			st.DownloadDir = *body.DownloadDir
		}
		if err := svc.SaveSettings(st); err != nil {
			jmFailErr(c, err, "保存设置失败")
			return
		}
		st = svc.Settings()
		jmOK(c, gin.H{
			"proxy": st.Proxy, "imageQuality": st.ImageQuality,
			"downloadDir": st.DownloadDir, "mock": false,
		})
	})

	// #24/#25/#26 /api/history — 阅读历史(本地持久化)
	g.GET("/history", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		page, valid := jm.ParsePageQuery(c.Query("page"))
		if !valid {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "page 参数不合法"})
			return
		}
		list, total, hasNext := jmService().Store.ListHistory(page)
		jmOK(c, gin.H{"list": list, "page": page, "total": total, "hasNext": hasNext})
	})
	g.POST("/history", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		var body struct {
			Aid        string `json:"aid"`
			Title      string `json:"title"`
			CoverURL   string `json:"coverUrl"`
			Pid        string `json:"pid"`
			EpTitle    *string `json:"epTitle"`
			ImageIndex *int   `json:"imageIndex"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Aid == "" || body.Pid == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "aid/pid 必填"})
			return
		}
		idx := 1
		if body.ImageIndex != nil && *body.ImageIndex > 0 {
			idx = *body.ImageIndex
		}
		epTitle := any(nil)
		if body.EpTitle != nil {
			epTitle = *body.EpTitle
		}
		item := jm.HistoryItem{
			Aid: body.Aid, Title: body.Title, CoverURL: body.CoverURL,
			Pid: body.Pid, EpTitle: epTitle, ImageIndex: idx,
			UpdatedAt: time.Now().Format("2006-01-02T15:04:05"),
		}
		if err := jmService().Store.ReportHistory(item); err != nil {
			jmFailErr(c, err, "保存历史失败")
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
	g.DELETE("/history", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		if err := jmService().Store.ClearHistory(); err != nil {
			jmFailErr(c, err, "清空历史失败")
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
}
