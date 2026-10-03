package handler

// JM 标签/作者收藏端点(私有扩展,不属于 MOBILE_API.md 的 30 端点契约;文档见 §7)。
//
//	GET    /api/jm/tag-favorites              → 收藏列表(type=tag|author,createdAt 倒序)
//	POST   /api/jm/tag-favorites              → 收藏(同 type+tag 幂等;body {tag,type?})
//	DELETE /api/jm/tag-favorites?tag=&type=   → 取消收藏(type 缺省 tag;不存在幂等成功)
//
// type 用于区分「标签」(searchType=tag)与「作者」(searchType=author);同一名值可
// 同时以两种类型收藏。鉴权口径:仅组级 nowen 登录(AuthRequired),不要求 JM 登录——
// 标签/作者收藏是设备级本地数据(同阅读历史),而浏览/搜索本就匿名可用,收藏跟随;
// 与 settings/下载同款,避免「未登录 JM 无法用快捷标签搜索」的引导死锁。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/jm"
)

// jmTagMaxLen 单个标签长度上限(上游标签为短语级文本,64 字符足够宽裕)。
const jmTagMaxLen = 64

func registerJMTagFavoriteRoutes(g *gin.RouterGroup) {
	// GET /tag-favorites — 列表(全量,createdAt 倒序)
	g.GET("/tag-favorites", func(c *gin.Context) {
		list, err := jmService().Store.ListTagFavorites()
		if err != nil {
			jmFailErr(c, err, "获取标签收藏失败")
			return
		}
		if list == nil {
			list = []jm.TagFavorite{}
		}
		jmOK(c, gin.H{"list": list, "total": len(list)})
	})

	// POST /tag-favorites — 收藏标签/作者(幂等)
	g.POST("/tag-favorites", func(c *gin.Context) {
		var body struct {
			Tag  string `json:"tag"`
			Type string `json:"type"` // 缺省 tag;枚举 tag|author
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "tag 必填"})
			return
		}
		if !jm.ValidTagFavoriteType(body.Type) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "type 必须为 tag|author"})
			return
		}
		tag := strings.TrimSpace(body.Tag)
		if tag == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "tag 必填"})
			return
		}
		if len(tag) > jmTagMaxLen {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "tag 过长(上限 64 字符)"})
			return
		}
		if err := jmService().Store.AddTagFavorite(body.Type, tag); err != nil {
			jmFailErr(c, err, "收藏失败")
			return
		}
		jmOK(c, gin.H{"ok": true})
	})

	// DELETE /tag-favorites?tag=&type= — 取消收藏(query 传参,规避中文路径参数编码问题)
	g.DELETE("/tag-favorites", func(c *gin.Context) {
		if !jm.ValidTagFavoriteType(c.Query("type")) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "type 必须为 tag|author"})
			return
		}
		tag := strings.TrimSpace(c.Query("tag"))
		if tag == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "tag 必填"})
			return
		}
		if err := jmService().Store.RemoveTagFavorite(c.Query("type"), tag); err != nil {
			jmFailErr(c, err, "取消收藏失败")
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
}
