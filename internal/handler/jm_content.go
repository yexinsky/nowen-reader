package handler

// JM 内容端点(浏览/搜索/详情/章节)。契约:MOBILE_API.md #6-#15。
// nowen 登录由 /jm 组级 middleware.AuthRequired() 统一要求(前端内容页不带
// JM Authorization 头,故此处不走 jmBearer);上游内容接口匿名可用,
// 统一取匿名客户端。
// 资源约束:每请求至多 2 次上游调用(详情/章节/搜车号伴随 /album),无并发批量拉取。

import (
	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/jm"
)

// jmContentAnon 内容端点统一匿名上游客户端(代理变更由 Service 内部失效重建)。
func jmContentAnon() *jm.Client { return jmService().AnonClient() }

// jmContent422 参数非法 → HTTP 422 + {"detail": ...}(契约 §0.2)。
func jmContent422(c *gin.Context, detail string) {
	c.JSON(422, gin.H{"detail": detail})
}

// jmContentGet 内容端点统一骨架:调包内方法 → 失败 jmFailErr(上游/网络分类,
// 默认消息对齐 live.py _map_live_error)/ 成功 jmOK。
func jmContentGet(c *gin.Context, fn func(cl *jm.Client) (any, error)) {
	data, err := fn(jmContentAnon())
	if err != nil {
		jmFailErr(c, err, "JM 服务端返回错误")
		return
	}
	jmOK(c, data)
}

func registerJMContentRoutes(g *gin.RouterGroup) {
	// 注意:/comics/index 等静态段先于 /comics/:aid 注册(契约 #14 备注)。

	// #6 GET /categories — 分类树
	g.GET("/categories", func(c *gin.Context) {
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.CategoriesList(c.Request.Context())
		})
	})

	// #7 GET /comics/index?page&order=Latest|View — 首页/全部漫画列表
	g.GET("/comics/index", func(c *gin.Context) {
		page, ok := jm.ParsePageQuery(c.Query("page"))
		if !ok {
			jmContent422(c, "page 参数不合法")
			return
		}
		order := c.DefaultQuery("order", "Latest")
		if order != "Latest" && order != "View" {
			jmContent422(c, "order 参数不合法")
			return
		}
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsIndex(c.Request.Context(), page, order)
		})
	})

	// #8 GET /comics/promote — 首页推荐区块
	g.GET("/comics/promote", func(c *gin.Context) {
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsPromote(c.Request.Context())
		})
	})

	// #9 GET /comics/latest?page — 最近更新
	g.GET("/comics/latest", func(c *gin.Context) {
		page, ok := jm.ParsePageQuery(c.Query("page"))
		if !ok {
			jmContent422(c, "page 参数不合法")
			return
		}
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsLatest(c.Request.Context(), page)
		})
	})

	// #10 GET /comics/serialization?day&type&page — 每周连载
	g.GET("/comics/serialization", func(c *gin.Context) {
		day := 1
		if raw := c.Query("day"); raw != "" {
			var ok bool
			if day, ok = jm.ParseIntInRange(raw, 1, 7); !ok {
				jmContent422(c, "day 参数不合法(1..7)")
				return
			}
		}
		serialType := c.DefaultQuery("type", "all")
		if serialType != "all" && serialType != "manga" && serialType != "hanman" {
			jmContent422(c, "type 参数不合法")
			return
		}
		page, ok := jm.ParsePageQuery(c.Query("page"))
		if !ok {
			jmContent422(c, "page 参数不合法")
			return
		}
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsSerialization(c.Request.Context(), day, serialType, page)
		})
	})

	// #11 GET /comics/week — 每周必看期数列表
	g.GET("/comics/week", func(c *gin.Context) {
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsWeek(c.Request.Context())
		})
	})

	// #12 GET /comics/week/filter?id&type — 每周必看单期列表(无翻页)
	g.GET("/comics/week/filter", func(c *gin.Context) {
		weekID := c.Query("id")
		if weekID == "" {
			jmContent422(c, "id 必填")
			return
		}
		weekType := c.DefaultQuery("type", "manga")
		if weekType != "manga" && weekType != "hanman" && weekType != "another" {
			jmContent422(c, "type 参数不合法")
			return
		}
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsWeekFilter(c.Request.Context(), weekID, weekType)
		})
	})

	// #13 GET /comics/search — 搜索/分类/排行
	g.GET("/comics/search", func(c *gin.Context) {
		page, ok := jm.ParsePageQuery(c.Query("page"))
		if !ok {
			jmContent422(c, "page 参数不合法")
			return
		}
		sortKey := c.Query("sort")
		switch sortKey {
		case "", "mr", "mv", "mv_m", "mv_w", "mv_t", "mp", "tf":
		default:
			jmContent422(c, "sort 参数不合法")
			return
		}
		searchType := c.Query("searchType")
		if searchType != "" {
			switch searchType {
			case "site", "work", "author", "tag", "character":
			default:
				jmContent422(c, "searchType 参数不合法")
				return
			}
		}
		year := 0
		if raw := c.Query("y"); raw != "" {
			if year, ok = jm.ParseIntInRange(raw, 1, 9999); !ok {
				jmContent422(c, "y 参数不合法(1..9999)")
				return
			}
		}
		month := 0
		if raw := c.Query("m"); raw != "" {
			if month, ok = jm.ParseIntInRange(raw, 1, 12); !ok {
				jmContent422(c, "m 参数不合法(1..12)")
				return
			}
		}
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicsSearch(c.Request.Context(), jm.SearchParams{
				Keyword:      c.Query("keyword"),
				Page:         page,
				Sort:         sortKey,
				SearchType:   searchType,
				Year:         year,
				Month:        month,
				MainCategory: c.Query("mainCategory"),
			})
		})
	})

	// #14 GET /comics/:aid — 漫画详情(上游 name 为空 → 3001)
	g.GET("/comics/:aid", func(c *gin.Context) {
		aid := c.Param("aid")
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.ComicDetail(c.Request.Context(), aid)
		})
	})

	// #15 GET /photos/:pid — 章节(图片清单;伴随 /album 拿章节顺序)
	g.GET("/photos/:pid", func(c *gin.Context) {
		pid := c.Param("pid")
		jmContentGet(c, func(cl *jm.Client) (any, error) {
			return cl.PhotoDetail(c.Request.Context(), pid)
		})
	})
}
