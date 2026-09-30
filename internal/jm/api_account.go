package jm

// JM 账号互动上游调用与契约映射(MOBILE_API.md #17-#19 评论/点赞、#20-#23 收藏、
// #29-#30 签到)。口径逐条对齐 mobile/server/core/live.py 与 jmcomic SDK
// (jm_client_impl.py:GET 参数拼 URL query,POST 走表单)。
//
// 约定:cl 为会话客户端(带登录 cookies,svc.SessionClient);reqAPI 已处理
// token 头/域名轮换/AES 解密与外层 code!=200 → *APIError(2001)。

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 上游分页与排序常量(live.py / jm_config.py)
const (
	pageSizeComments = 20 // 评论页大小(契约 §2.4)
	pageSizeFavorite = 20 // 收藏页大小(契约 §2.4)
	orderByLatest    = "mr"
)

/* ── 上游调用小工具 ── */

// reqGETParams GET 请求:参数拼进 URL query(SDK append_params_to_url 语义)。
func reqGETParams(ctx context.Context, cl *Client, path string, q url.Values) ([]byte, error) {
	full := path
	if enc := q.Encode(); enc != "" {
		full = path + "?" + enc
	}
	return cl.reqAPI(ctx, "GET", full, nil)
}

// decodeMap 解析 reqAPI 返回的 decoded JSON;顶层非对象 → (nil, false)。
func decodeMap(data []byte) (map[string]any, bool) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// listOf 取 JSON 数组(缺失/类型不符 → nil,对应 Python `x or []`)。
func listOf(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

// requireStatusOK 业务状态校验(SDK require_resp_status_ok;live.py like/favorite 同款):
// decoded.status != "ok" → 2001(msg 为上游信息,缺失用 defaultMsg)。
func requireStatusOK(decoded map[string]any, defaultMsg string) error {
	if fieldStr(decoded, "status") == "ok" {
		return nil
	}
	msg := fieldStrOr(decoded, "msg", "")
	if msg == "" {
		msg = defaultMsg
	}
	return errUpstream(msg, truncate(fmt.Sprint(decoded), 300))
}

// anyToInt 宽松转 int(Python int() 语义:int("5")/5.0 → 5;失败 → false)。
func anyToInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// hasNextPage hasNext 判定(live.py _has_next,不含 max_page 分支):
// 本页不满一页(含空页)→ false(覆盖 total 缺失/为 0 的兜底);
// total>0 且 page*页大小 >= total → false;否则 true。
func hasNextPage(page, total, pageLen, pageSize int) bool {
	if pageLen < pageSize {
		return false
	}
	if total > 0 && page*pageSize >= total {
		return false
	}
	return true
}

/* ── #17/#18 评论 ── */

// mapComment 上游评论对象 → 契约 CommentItem(live.py _map_comment 直译):
// id=str(CID);content 剥 HTML;createdAt=epoch → 东八区 "%Y-%m-%d %H:%M";
// replyTo 恒 null(上游未提供映射);replies 取上游 replys 递归。
func mapComment(v map[string]any) map[string]any {
	replies := []any{}
	for _, r := range listOf(v["replys"]) {
		if m, ok := r.(map[string]any); ok {
			replies = append(replies, mapComment(m))
		}
	}
	return map[string]any{
		"id":        fieldStr(v, "CID"),
		"user":      map[string]any{"name": fieldStr(v, "username"), "avatarUrl": avatarURL(v["photo"])},
		"content":   stripHTML(fieldStr(v, "content")),
		"createdAt": epochToDongba(v["addtime"], true),
		"likes":     parseCount(v["likes"]),
		"replyTo":   nil,
		"replies":   replies,
	}
}

// mapCommentList 上游 /forum decoded 对象 → 契约分页结构(live.py comment_list):
// total 取上游 total(缺失按本页条数;显式 null → 0);hasNext 按页大小 20 判定。
func mapCommentList(raw map[string]any, page int) map[string]any {
	comments := []any{}
	for _, it := range listOf(raw["list"]) {
		if m, ok := it.(map[string]any); ok {
			comments = append(comments, mapComment(m))
		}
	}
	// live.py:_parse_count(raw.get("total", len(comments))) — 缺失按本页条数,显式 null → 0
	total := len(comments)
	if tv, ok := raw["total"]; ok {
		total = parseCount(tv)
	}
	return map[string]any{
		"page":    page,
		"total":   total,
		"hasNext": hasNextPage(page, total, len(comments), pageSizeComments),
		"list":    comments,
	}
}

// CommentList #17 评论列表:GET /forum?mode=manhua&aid&page(页大小 20)。
func CommentList(ctx context.Context, cl *Client, aid string, page int) (map[string]any, error) {
	q := url.Values{}
	q.Set("mode", "manhua")
	q.Set("aid", aid)
	q.Set("page", strconv.Itoa(page))
	data, err := reqGETParams(ctx, cl, "/forum", q)
	if err != nil {
		return nil, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, errUpstream("获取评论失败", truncate(string(data), 300))
	}
	return mapCommentList(raw, page), nil
}

// CommentPost #18 发布评论:POST /comment(表单 comment+aid);live 不回读。
func CommentPost(ctx context.Context, cl *Client, aid, content string) (map[string]any, error) {
	form := url.Values{}
	form.Set("comment", content)
	form.Set("aid", aid)
	if _, err := cl.reqAPI(ctx, "POST", "/comment", form); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

/* ── #19 点赞 ── */

// likeStateFromMsg 点赞切换语义的 msg 关键字判定(live.py like):
// 含"取消" → false;含"点赞/點讚/喜欢/喜歡" → true;均未命中 → nil(无法判定)。
func likeStateFromMsg(msg string) any {
	if strings.Contains(msg, "取消") {
		return false
	}
	if strings.Contains(msg, "点赞") || strings.Contains(msg, "點讚") ||
		strings.Contains(msg, "喜欢") || strings.Contains(msg, "喜歡") {
		return true
	}
	return nil
}

// LikeComic #19 点赞:POST /like(表单 aid);上游为切换语义(已赞则取消),
// 实际结果经解码 msg 关键字判定,liked 可为 true/false/null。
func LikeComic(ctx context.Context, cl *Client, aid string) (map[string]any, error) {
	form := url.Values{}
	form.Set("aid", aid)
	data, err := cl.reqAPI(ctx, "POST", "/like", form)
	if err != nil {
		return nil, err
	}
	decoded, ok := decodeMap(data)
	if !ok {
		decoded = map[string]any{}
	}
	if err := requireStatusOK(decoded, "点赞失败"); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "liked": likeStateFromMsg(fieldStrOr(decoded, "msg", ""))}, nil
}

/* ── #20-#23 收藏 ── */

// favoriteStateFromMsg 收藏切换语义的 msg 关键字判定(live.py _favorite_toggle):
// 含"取消" → false;含"收藏" → true;均未命中 → nil(无法判定)。
func favoriteStateFromMsg(msg string) any {
	if strings.Contains(msg, "取消") {
		return false
	}
	if strings.Contains(msg, "收藏") {
		return true
	}
	return nil
}

// favoriteToggle 上游 POST /favorite 切换端点(live.py _favorite_toggle):
// 添加与取消收藏使用同一端点(上游移动端无独立 folder 参数),仅传 aid;
// 返回 favorited:true 已收藏 / false 已取消 / nil 无法判定。
func favoriteToggle(ctx context.Context, cl *Client, aid string) (any, error) {
	form := url.Values{}
	form.Set("aid", aid)
	data, err := cl.reqAPI(ctx, "POST", "/favorite", form)
	if err != nil {
		return nil, err
	}
	decoded, ok := decodeMap(data)
	if !ok {
		decoded = map[string]any{}
	}
	if err := requireStatusOK(decoded, "收藏操作失败"); err != nil {
		return nil, err
	}
	return favoriteStateFromMsg(fieldStrOr(decoded, "msg", "")), nil
}

// FavoriteFolders #20 收藏夹列表:SDK favorite_folder(page=1) 对应上游
// GET /favorite?page=1&folder_id=0&o=mr,取响应 folder_list(FID/name/count,
// count 经 parseCount 兼容 "1.2K");total 取上游 total。
func FavoriteFolders(ctx context.Context, cl *Client) (map[string]any, error) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("folder_id", "0")
	q.Set("o", orderByLatest)
	data, err := reqGETParams(ctx, cl, "/favorite", q)
	if err != nil {
		return nil, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, errUpstream("获取收藏夹失败", truncate(string(data), 300))
	}
	folders := []any{}
	for _, it := range listOf(raw["folder_list"]) {
		f, ok := it.(map[string]any)
		if !ok {
			continue
		}
		fid := fieldStrOr(f, "FID", "0")
		if fid == "" {
			fid = "0"
		}
		folders = append(folders, map[string]any{
			"id":    fid,
			"name":  fieldStrOr(f, "name", ""),
			"count": parseCount(f["count"]),
		})
	}
	return map[string]any{"folders": folders, "total": parseCount(raw["total"])}, nil
}

// FavoriteList #21 收藏列表:上游 GET /favorite?page&folder_id&o(SDK
// favorite_folder(page, folder_id));页大小 20;list 为 ComicItem 契约结构。
func FavoriteList(ctx context.Context, cl *Client, folderID string, page int) (map[string]any, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("folder_id", folderID)
	q.Set("o", orderByLatest)
	data, err := reqGETParams(ctx, cl, "/favorite", q)
	if err != nil {
		return nil, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, errUpstream("获取收藏列表失败", truncate(string(data), 300))
	}
	items := []any{}
	for _, it := range listOf(raw["list"]) {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, ComicItemFromUpstream(fieldStr(m, "id"), m))
	}
	total := parseCount(raw["total"])
	return map[string]any{
		"page":    page,
		"total":   total,
		"hasNext": hasNextPage(page, total, len(items), pageSizeFavorite),
		"list":    items,
	}, nil
}

// FavoriteAdd #22 添加收藏(切换端点):msg 判定失败 → 乐观默认 true
// (live.py add_favorite:bool(favorited) if favorited is not None else True)。
func FavoriteAdd(ctx context.Context, cl *Client, aid string) (map[string]any, error) {
	favorited, err := favoriteToggle(ctx, cl, aid)
	if err != nil {
		return nil, err
	}
	if b, ok := favorited.(bool); ok {
		return map[string]any{"ok": true, "favorited": b}, nil
	}
	return map[string]any{"ok": true, "favorited": true}, nil
}

// FavoriteDelete #23 取消收藏(与 #22 同一切换端点):msg 判定失败 → false
// (live.py delete_favorite;注意与 #22 的乐观默认相反)。
func FavoriteDelete(ctx context.Context, cl *Client, aid string) (map[string]any, error) {
	favorited, err := favoriteToggle(ctx, cl, aid)
	if err != nil {
		return nil, err
	}
	if b, ok := favorited.(bool); ok {
		return map[string]any{"ok": true, "favorited": b}, nil
	}
	return map[string]any{"ok": true, "favorited": false}, nil
}

/* ── #29/#30 签到 ── */

// SessionUID 从登录 decoded 快照取用户 id(#29/#30):上游原始字段为 uid,
// 兼容契约形态的 userId。
func SessionUID(userInfo map[string]any) string {
	if userInfo == nil {
		return ""
	}
	if uid := fieldStr(userInfo, "uid"); uid != "" {
		return uid
	}
	return fieldStr(userInfo, "userId")
}

// parseDaily 上游 /daily decoded → 签到状态(live.py _parse_daily 直译):
// daily_id(缺失/非法 → 0);record=[[{date,signed}],...](按周嵌套,兼容
// 非嵌套项);date 为"本月第几日"(非法项跳过);todaySigned = date==今天的
// day 的 signed。
func parseDaily(data map[string]any) map[string]any {
	dailyID := 0
	if v, ok := data["daily_id"]; ok {
		dailyID, _ = anyToInt(v)
	}
	today := time.Now().Day()
	todaySigned := false
	days := []any{}
	for _, week := range listOf(data["record"]) {
		items := []any{week} // Python: week if isinstance(week, list) else [week]
		if arr, ok := week.([]any); ok {
			items = arr
		}
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			day, ok := anyToInt(m["date"])
			if !ok {
				continue
			}
			signed := boolOf(m["signed"])
			days = append(days, map[string]any{"date": day, "signed": signed})
			if day == today {
				todaySigned = signed
			}
		}
	}
	return map[string]any{"dailyId": dailyID, "todaySigned": todaySigned, "days": days}
}

// SignStatus #29 签到状态:GET /daily?user_id={uid}(对齐桌面端 GetDailyReq2)。
func SignStatus(ctx context.Context, cl *Client, uid string) (map[string]any, error) {
	q := url.Values{}
	q.Set("user_id", uid)
	data, err := reqGETParams(ctx, cl, "/daily", q)
	if err != nil {
		return nil, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		raw = map[string]any{} // Python: data if isinstance(data, dict) else {}
	}
	return parseDaily(raw), nil
}

// signRewardRe 上游签到成功消息的奖励字段(如 "Jcoin:70 EXP:70")。
var signRewardRe = regexp.MustCompile(`(?i)Jcoin\s*:\s*(\d+)\s*[,，;；\s]+EXP\s*:\s*(\d+)`)

// prettifySignMsg 美化上游签到消息:
// "Jcoin:70 EXP:70" → "签到成功!获得 70 金币、70 经验";其余消息原样透传。
func prettifySignMsg(msg string) string {
	if m := signRewardRe.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("签到成功!获得 %s 金币、%s 经验", m[1], m[2])
	}
	return msg
}

// SignDo #30 执行签到:POST /daily_chk(表单 user_id+daily_id,对齐桌面端
// SignDailyReq2);msg 为上游返回信息(缺失/解密失败 → "")。
// 注意幂等由调用方"先查后签"实现(见 handler #30)。
func SignDo(ctx context.Context, cl *Client, uid string, dailyID int) (map[string]any, error) {
	form := url.Values{}
	form.Set("user_id", uid)
	form.Set("daily_id", strconv.Itoa(dailyID))
	data, err := cl.reqAPI(ctx, "POST", "/daily_chk", form)
	if err != nil {
		return nil, err
	}
	msg := ""
	if decoded, ok := decodeMap(data); ok {
		if m := fieldStrOr(decoded, "msg", ""); m != "" {
			msg = prettifySignMsg(m)
		}
	}
	return map[string]any{"ok": true, "msg": msg}, nil
}
