package handler

// PicACG 内容端点(浏览/搜索/排行/详情/章节/评论)。
// ⚠️ 哔咔上游对内容接口一律要求登录(实测无 token/坏 token 均被拒,与"匿名可浏览"
// 的 JM 不同):nowen 登录由 /pica 组级 AuthRequired 统一要求,X-Pica-Token
// (哔咔 JWT)必填,缺失/失效按 1002/401 处理由前端引导重登。
// 每请求至多 1 次上游调用,无并发批量拉取。

import (
	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/pica"
)

// picaContent422 参数非法 → HTTP 422。
func picaContent422(c *gin.Context, detail string) {
	c.JSON(422, gin.H{"detail": detail})
}

// picaContentGet 内容端点统一骨架:调包内方法 → 失败 picaFailErr / 成功 jmOK。
func picaContentGet(c *gin.Context, fn func(cl *pica.Client, token string) (any, error)) {
	token, ok := picaRequireToken(c)
	if !ok {
		return
	}
	cl, ok := picaClient(c)
	if !ok {
		return
	}
	data, err := fn(cl, token)
	if err != nil {
		picaFailErr(c, err, "哔咔服务端返回错误")
		return
	}
	jmOK(c, data)
}

func registerPicaContentRoutes(g *gin.RouterGroup) {
	// GET /categories — 分类列表
	g.GET("/categories", func(c *gin.Context) {
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Categories(c.Request.Context(), token)
		})
	})

	// GET /comics?page&c&s — 分类浏览(s ∈ dd|da|ld|vd)
	g.GET("/comics", func(c *gin.Context) {
		page, ok := pica.ParsePageQuery(c.Query("page"))
		if !ok {
			picaContent422(c, "page 参数不合法")
			return
		}
		category := c.Query("c")
		if category == "" {
			picaContent422(c, "c(分类名)必填")
			return
		}
		sort := c.DefaultQuery("s", "dd")
		switch sort {
		case "dd", "da", "ld", "vd":
		default:
			picaContent422(c, "s 参数不合法(dd|da|ld|vd)")
			return
		}
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.ComicsByCategory(c.Request.Context(), token, category, sort, page)
		})
	})

	// GET /search?keyword&page&sort&categories=逗号分隔 — 高级搜索
	g.GET("/search", func(c *gin.Context) {
		page, ok := pica.ParsePageQuery(c.Query("page"))
		if !ok {
			picaContent422(c, "page 参数不合法")
			return
		}
		sort := c.Query("sort")
		switch sort {
		case "", "dd", "da", "ld", "vd":
		default:
			picaContent422(c, "sort 参数不合法(dd|da|ld|vd)")
			return
		}
		var categories []string
		if raw := c.Query("categories"); raw != "" {
			categories = splitAndTrim(raw)
		}
		keyword := c.Query("keyword")
		if keyword == "" && len(categories) == 0 {
			picaContent422(c, "keyword 与 categories 至少提供其一")
			return
		}
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.AdvancedSearch(c.Request.Context(), token, pica.SearchParams{
				Keyword:    keyword,
				Categories: categories,
				Sort:       sort,
			}, page)
		})
	})

	// GET /keywords — 搜索热词
	g.GET("/keywords", func(c *gin.Context) {
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Keywords(c.Request.Context(), token)
		})
	})

	// GET /random — 随机本子
	g.GET("/random", func(c *gin.Context) {
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Random(c.Request.Context(), token)
		})
	})

	// GET /leaderboard?tt=H24|D7|D30 — 排行榜
	g.GET("/leaderboard", func(c *gin.Context) {
		tt := c.DefaultQuery("tt", "D7")
		switch tt {
		case "H24", "D7", "D30":
		default:
			picaContent422(c, "tt 参数不合法(H24|D7|D30)")
			return
		}
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Leaderboard(c.Request.Context(), token, tt)
		})
	})

	// GET /collections — 神魔推荐
	g.GET("/collections", func(c *gin.Context) {
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Collections(c.Request.Context(), token)
		})
	})

	// GET /comics/:id — 漫画详情
	g.GET("/comics/:id", func(c *gin.Context) {
		id := c.Param("id")
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.ComicDetail(c.Request.Context(), token, id)
		})
	})

	// GET /comics/:id/eps?page — 章节列表
	g.GET("/comics/:id/eps", func(c *gin.Context) {
		id := c.Param("id")
		page, ok := pica.ParsePageQuery(c.Query("page"))
		if !ok {
			picaContent422(c, "page 参数不合法")
			return
		}
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Eps(c.Request.Context(), token, id, page)
		})
	})

	// GET /comics/:id/eps/:order/pages?page — 章节图片列表(order 为章节序号)
	g.GET("/comics/:id/eps/:order/pages", func(c *gin.Context) {
		id := c.Param("id")
		order, ok := pica.ParseIntInRange(c.Param("order"), 1, 10000)
		if !ok {
			picaContent422(c, "order 参数不合法(章节序号,≥1)")
			return
		}
		page, ok := pica.ParsePageQuery(c.Query("page"))
		if !ok {
			picaContent422(c, "page 参数不合法")
			return
		}
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.EpsPages(c.Request.Context(), token, id, order, page)
		})
	})

	// GET /comics/:id/comments?page — 评论列表(只读)
	g.GET("/comics/:id/comments", func(c *gin.Context) {
		id := c.Param("id")
		page, ok := pica.ParsePageQuery(c.Query("page"))
		if !ok {
			picaContent422(c, "page 参数不合法")
			return
		}
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Comments(c.Request.Context(), token, id, page)
		})
	})

	// GET /comics/:id/recommendation — 相关推荐
	g.GET("/comics/:id/recommendation", func(c *gin.Context) {
		id := c.Param("id")
		picaContentGet(c, func(cl *pica.Client, token string) (any, error) {
			return cl.Recommendation(c.Request.Context(), token, id)
		})
	})
}
