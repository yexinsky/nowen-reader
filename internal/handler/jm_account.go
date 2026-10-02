package handler

// JM 账号互动端点(M4):评论/点赞/收藏/签到。
// 契约:MOBILE_API.md #17-#19(评论/点赞)、#20-#23(收藏)、#29-#30(签到)。
// 全部需登录:先 jmBearer 取会话;上游经会话客户端 svc.SessionClient(带登录
// cookies);上游调用与契约映射在 internal/jm/api_account.go。

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/jm"
)

// jmSessionClient 会话上游客户端;token 失效(客户端为 nil)时写 1002 并返回 nil。
func jmSessionClient(c *gin.Context, sess *jm.Session) *jm.Client {
	cl, _ := jmService().SessionClient(sess.Token)
	if cl == nil {
		jmFail(c, jm.CodeUnauthorized, "登录已失效,请重新登录", nil)
		return nil
	}
	return cl
}

func registerJMAccountRoutes(g *gin.RouterGroup) {
	// #17 GET /comics/:aid/comments — 评论列表(页大小 20;评论读取也要求登录)
	g.GET("/comics/:aid/comments", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		page, valid := jm.ParsePageQuery(c.Query("page"))
		if !valid {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "page 参数不合法"})
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.CommentList(c.Request.Context(), cl, c.Param("aid"), page)
		if err != nil {
			jmFailErr(c, err, "获取评论失败")
			return
		}
		jmOK(c, data)
	})

	// #18 POST /comics/:aid/comments — 发布评论(live 仅透传,不回读)
	g.POST("/comics/:aid/comments", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Content == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "content 必填"})
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.CommentPost(c.Request.Context(), cl, c.Param("aid"), body.Content)
		if err != nil {
			jmFailErr(c, err, "发布评论失败")
			return
		}
		jmOK(c, data)
	})

	// #19 POST /comics/:aid/like — 点赞(上游切换语义:结果经 msg 关键字判定,
	// liked 可为 true/false/null,调用方以响应为准)
	g.POST("/comics/:aid/like", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.LikeComic(c.Request.Context(), cl, c.Param("aid"))
		if err != nil {
			log.Printf("[jm] like failed aid=%s: %v upstream=%s", c.Param("aid"), err, jm.UpstreamDetail(err))
			jmFailErr(c, err, "点赞失败")
			return
		}
		jmOK(c, data)
	})

	// #20 GET /favorites/folders — 收藏夹列表(上游 /favorite 第一页的 folder_list)
	g.GET("/favorites/folders", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.FavoriteFolders(c.Request.Context(), cl)
		if err != nil {
			jmFailErr(c, err, "获取收藏夹失败")
			return
		}
		jmOK(c, data)
	})

	// #21 GET /favorites — 收藏列表(folderId 默认 "0"=全部,live 语义;页大小 20)
	g.GET("/favorites", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		page, valid := jm.ParsePageQuery(c.Query("page"))
		if !valid {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "page 参数不合法"})
			return
		}
		folderID := c.Query("folderId")
		if folderID == "" {
			folderID = "0"
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.FavoriteList(c.Request.Context(), cl, folderID, page)
		if err != nil {
			jmFailErr(c, err, "获取收藏列表失败")
			return
		}
		jmOK(c, data)
	})

	// #22 POST /favorites — 添加收藏(切换端点;folderId live 忽略;无法判定 → true)
	g.POST("/favorites", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		var body struct {
			Aid      string `json:"aid"`
			FolderID string `json:"folderId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Aid == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "aid 必填"})
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.FavoriteAdd(c.Request.Context(), cl, body.Aid)
		if err != nil {
			log.Printf("[jm] favorite add failed aid=%s: %v upstream=%s", body.Aid, err, jm.UpstreamDetail(err))
			jmFailErr(c, err, "收藏操作失败")
			return
		}
		jmOK(c, data)
	})

	// #23 DELETE /favorites/:aid — 取消收藏(同一上游切换端点;无法判定 → false)
	g.DELETE("/favorites/:aid", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.FavoriteDelete(c.Request.Context(), cl, c.Param("aid"))
		if err != nil {
			log.Printf("[jm] favorite delete failed aid=%s: %v upstream=%s", c.Param("aid"), err, jm.UpstreamDetail(err))
			jmFailErr(c, err, "收藏操作失败")
			return
		}
		jmOK(c, data)
	})

	// #29 GET /user/sign — 签到状态(uid 取登录快照)
	g.GET("/user/sign", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		data, err := jm.SignStatus(c.Request.Context(), cl, jm.SessionUID(sess.UserInfo))
		if err != nil {
			jmFailErr(c, err, "获取签到状态失败")
			return
		}
		jmOK(c, data)
	})

	// #30 POST /user/sign — 执行签到(先查后签保证幂等):
	// ① 查状态;② 今日已签 → {ok,msg:"今日已签到"}(不调上游);
	// ③ dailyId<=0 → 2001("缺少 daily_id，无法签到");④ POST /daily_chk。
	g.POST("/user/sign", func(c *gin.Context) {
		sess := jmBearer(c)
		if sess == nil {
			return
		}
		cl := jmSessionClient(c, sess)
		if cl == nil {
			return
		}
		uid := jm.SessionUID(sess.UserInfo)
		status, err := jm.SignStatus(c.Request.Context(), cl, uid)
		if err != nil {
			jmFailErr(c, err, "获取签到状态失败")
			return
		}
		if signed, _ := status["todaySigned"].(bool); signed {
			jmOK(c, gin.H{"ok": true, "msg": "今日已签到"})
			return
		}
		dailyID, _ := status["dailyId"].(int)
		if dailyID <= 0 {
			jmFail(c, jm.CodeUpstream, "缺少 daily_id，无法签到", nil)
			return
		}
		data, err := jm.SignDo(c.Request.Context(), cl, uid, dailyID)
		if err != nil {
			log.Printf("[jm] sign failed uid=%s: %v upstream=%s", uid, err, jm.UpstreamDetail(err))
			jmFailErr(c, err, "签到失败")
			return
		}
		jmOK(c, data)
	})
}
