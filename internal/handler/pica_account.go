package handler

// PicACG 账号互动端点:收藏列表/收藏切换/爱心切换/打卡。
// 哔咔无状态 JWT:X-Pica-Token 必填(缺失按 1002/401 处理)。

import (
	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/pica"
)

// picaRequireToken 要求哔咔 token;缺失直接写 1002/401 响应并返回 false。
func picaRequireToken(c *gin.Context) (string, bool) {
	token := picaBearer(c)
	if token == "" {
		jmFail(c, pica.CodeUnauthorized, "登录已失效,请重新登录", nil)
		return "", false
	}
	return token, true
}

func registerPicaAccountRoutes(g *gin.RouterGroup) {
	// GET /favourites?s&page — 收藏列表(s ∈ dd|da)
	g.GET("/favourites", func(c *gin.Context) {
		token, ok := picaRequireToken(c)
		if !ok {
			return
		}
		page, valid := pica.ParsePageQuery(c.Query("page"))
		if !valid {
			picaContent422(c, "page 参数不合法")
			return
		}
		sort := c.DefaultQuery("s", "da")
		switch sort {
		case "dd", "da":
		default:
			picaContent422(c, "s 参数不合法(dd|da)")
			return
		}
		cl, ok := picaClient(c)
		if !ok {
			return
		}
		data, err := cl.FavouriteList(c.Request.Context(), token, sort, page)
		if err != nil {
			picaFailErr(c, err, "获取收藏列表失败")
			return
		}
		jmOK(c, data)
	})

	// POST /comics/:id/favourite — 收藏/取消收藏(切换式,action 表达结果)
	g.POST("/comics/:id/favourite", func(c *gin.Context) {
		token, ok := picaRequireToken(c)
		if !ok {
			return
		}
		cl, ok := picaClient(c)
		if !ok {
			return
		}
		data, err := cl.ToggleFavourite(c.Request.Context(), token, c.Param("id"))
		if err != nil {
			picaFailErr(c, err, "收藏操作失败")
			return
		}
		jmOK(c, data)
	})

	// POST /comics/:id/like — 爱心点赞(切换式)
	g.POST("/comics/:id/like", func(c *gin.Context) {
		token, ok := picaRequireToken(c)
		if !ok {
			return
		}
		cl, ok := picaClient(c)
		if !ok {
			return
		}
		data, err := cl.ToggleLike(c.Request.Context(), token, c.Param("id"))
		if err != nil {
			picaFailErr(c, err, "点赞操作失败")
			return
		}
		jmOK(c, data)
	})

	// POST /punch-in — 每日打卡
	g.POST("/punch-in", func(c *gin.Context) {
		token, ok := picaRequireToken(c)
		if !ok {
			return
		}
		cl, ok := picaClient(c)
		if !ok {
			return
		}
		data, err := cl.PunchIn(c.Request.Context(), token)
		if err != nil {
			picaFailErr(c, err, "打卡失败")
			return
		}
		jmOK(c, gin.H{"ok": true, "upstream": data})
	})
}
