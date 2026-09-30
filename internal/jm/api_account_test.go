package jm

// 账号互动契约映射单测:切换语义 msg 关键字判定、/daily 解析、评论映射、
// ComicItem 封面归一。口径对照 mobile/server/core/live.py 与 MOBILE_API.md。

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("json 解析失败: %v", err)
	}
	return m
}

// ── #19 点赞切换语义(live.py like 的 msg 关键字判定) ──

func TestLikeStateFromMsg(t *testing.T) {
	cases := []struct {
		msg  string
		want any // bool 或 nil
	}{
		{"取消点赞成功", false}, // 含"取消"优先
		{"取消", false},
		{"点赞成功", true},
		{"點讚成功", true}, // 繁体
		{"喜欢上了", true},
		{"喜歡成功", true},
		{"", nil},       // 无法判定 → null
		{"什么都没有", nil},  // 无法判定 → null
		{"取消xxxx点赞", false}, // 取消优先于点赞
	}
	for _, c := range cases {
		got := likeStateFromMsg(c.msg)
		if got != c.want {
			t.Fatalf("likeStateFromMsg(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

// ── #22/#23 收藏切换语义(live.py _favorite_toggle) ──

func TestFavoriteStateFromMsg(t *testing.T) {
	if got := favoriteStateFromMsg("取消收藏成功"); got != false {
		t.Fatalf("取消 → false, got %v", got)
	}
	if got := favoriteStateFromMsg("收藏成功"); got != true {
		t.Fatalf("收藏 → true, got %v", got)
	}
	if got := favoriteStateFromMsg("未知消息"); got != nil {
		t.Fatalf("无法判定 → nil, got %v", got)
	}
	// #22 乐观默认 true / #23 无法判定 → false(由 FavoriteAdd/FavoriteDelete 应用)
	if _, ok := favoriteStateFromMsg("未知消息").(bool); ok {
		t.Fatal("无法判定时不应是 bool")
	}
}

// ── #29/#30 _parse_daily ──

func TestParseDaily(t *testing.T) {
	today := time.Now().Day()

	// record 按周嵌套 [[{date,signed}],...];含今天的日期
	raw := mustJSON(t, `{
		"daily_id": 260901,
		"record": [
			[{"date": 1, "signed": true}, {"date": 2, "signed": false}],
			[{"date": `+strconv.Itoa(today)+`, "signed": true}]
		]
	}`)
	st := parseDaily(raw)
	if st["dailyId"] != 260901 {
		t.Fatalf("dailyId = %v, want 260901", st["dailyId"])
	}
	if st["todaySigned"] != true {
		t.Fatalf("todaySigned 应为 true(今天已签)")
	}
	days := st["days"].([]any)
	if len(days) != 3 {
		t.Fatalf("days 长度 = %d, want 3", len(days))
	}
	first := days[0].(map[string]any)
	if first["date"] != 1 || first["signed"] != true { // date 为 int(本月第几日)
		t.Fatalf("days[0] = %v", first)
	}

	// 今天未签
	raw2 := mustJSON(t, `{"daily_id": 5, "record": [[{"date": `+strconv.Itoa(today)+`, "signed": false}]]}`)
	if st2 := parseDaily(raw2); st2["todaySigned"] != false {
		t.Fatalf("todaySigned 应为 false")
	}

	// 非 dict 项跳过 + record 缺失 → 空 days
	st3 := parseDaily(mustJSON(t, `{"daily_id": 5, "record": [[{"date": "x"}], "junk", 3]}`))
	if d, ok := st3["days"].([]any); !ok || len(d) != 0 {
		t.Fatalf("非法项应跳过, days = %v", st3["days"])
	}
}

func TestParseDailyDailyID(t *testing.T) {
	// daily_id 字符串数字 → int
	if st := parseDaily(mustJSON(t, `{"daily_id": "260901", "record": []}`)); st["dailyId"] != 260901 {
		t.Fatalf("字符串 daily_id 应转 int, got %v", st["dailyId"])
	}
	// daily_id 非法("abc"/null) → 0
	if st := parseDaily(mustJSON(t, `{"daily_id": "abc", "record": []}`)); st["dailyId"] != 0 {
		t.Fatalf("非法 daily_id 应为 0, got %v", st["dailyId"])
	}
	if st := parseDaily(mustJSON(t, `{"daily_id": null, "record": []}`)); st["dailyId"] != 0 {
		t.Fatalf("null daily_id 应为 0, got %v", st["dailyId"])
	}
	// daily_id 缺失 → 0
	if st := parseDaily(mustJSON(t, `{"record": []}`)); st["dailyId"] != 0 {
		t.Fatalf("缺失 daily_id 应为 0, got %v", st["dailyId"])
	}
	// record 为非嵌套对象(Python: week if isinstance(week, list) else [week])
	st := parseDaily(mustJSON(t, `{"daily_id": 1, "record": [{"date": 3, "signed": true}]}`))
	days := st["days"].([]any)
	if len(days) != 1 || days[0].(map[string]any)["date"] != 3 {
		t.Fatalf("非嵌套 record 项应按单项处理, days = %v", days)
	}
}

// ── #17 评论映射(_map_comment / comment_list) ──

func TestMapComment(t *testing.T) {
	raw := mustJSON(t, `{
		"CID": 9527,
		"username": "路人甲",
		"photo": "abc.jpg",
		"content": "<br>好<b>看</b>&amp;赞",
		"addtime": 1700000000,
		"likes": 3,
		"replys": [
			{"CID": 9528, "username": "路人乙", "content": "回复", "replys": []}
		]
	}`)
	m := mapComment(raw)

	if m["id"] != "9527" {
		t.Fatalf("id 应为 str(CID), got %v", m["id"])
	}
	if m["replyTo"] != nil {
		t.Fatalf("replyTo 恒为 null, got %v", m["replyTo"])
	}
	if m["content"] != "好看&赞" {
		// <br> → \n,剥标签,实体还原,首尾去空白(Python strip() 口径)
		t.Fatalf("content = %q", m["content"])
	}
	user := m["user"].(map[string]any)
	if user["name"] != "路人甲" {
		t.Fatalf("user.name = %v", user["name"])
	}
	if user["avatarUrl"] == nil {
		t.Fatal("avatarUrl 不应为 null")
	}
	// 东八区 "%Y-%m-%d %H:%M":1700000000 → 2023-11-15 06:13(UTC+8)
	if m["createdAt"] != "2023-11-15 06:13" {
		t.Fatalf("createdAt = %v, want 2023-11-15 06:13", m["createdAt"])
	}
	if m["likes"] != 3 {
		t.Fatalf("likes = %v", m["likes"])
	}
	replies := m["replies"].([]any)
	if len(replies) != 1 {
		t.Fatalf("replies 长度 = %d", len(replies))
	}
	r0 := replies[0].(map[string]any)
	if r0["id"] != "9528" || r0["replyTo"] != nil {
		t.Fatalf("replies[0] = %v", r0)
	}
	if _, ok := r0["replies"].([]any); !ok {
		t.Fatal("嵌套 replies 应为数组")
	}
}

func TestMapCommentDefaults(t *testing.T) {
	// 缺失字段兜底:likes → 0;photo 缺失 → null avatarUrl;空 replies 数组
	m := mapComment(mustJSON(t, `{"CID": 1, "username": "x", "content": "hi"}`))
	if m["likes"] != 0 {
		t.Fatalf("likes 缺失应为 0, got %v", m["likes"])
	}
	user := m["user"].(map[string]any)
	if user["avatarUrl"] != nil {
		t.Fatalf("avatarUrl 缺失应为 null, got %v", user["avatarUrl"])
	}
	if r := m["replies"].([]any); len(r) != 0 {
		t.Fatalf("replies 缺失应为空数组, got %v", r)
	}
}

func TestMapCommentListTotal(t *testing.T) {
	// total 缺失 → 按本页条数
	raw := mustJSON(t, `{"list": [{"CID": 1, "content": "a"}, {"CID": 2, "content": "b"}]}`)
	res := mapCommentList(raw, 1)
	if res["total"] != 2 {
		t.Fatalf("total 缺失应按本页条数, got %v", res["total"])
	}
	if res["hasNext"] != false {
		t.Fatalf("本页不满一页 → hasNext false, got %v", res["hasNext"])
	}

	// total 显式 null → 0
	raw2 := mustJSON(t, `{"list": [{"CID": 1, "content": "a"}], "total": null}`)
	if res2 := mapCommentList(raw2, 1); res2["total"] != 0 {
		t.Fatalf("total 显式 null 应为 0, got %v", res2["total"])
	}

	// total 正常 + hasNext 判定(页大小 20)
	raw3 := mustJSON(t, `{"list": [], "total": 50}`)
	// 空页必为末页
	if res3 := mapCommentList(raw3, 1); res3["hasNext"] != false {
		t.Fatalf("空页 hasNext 应为 false")
	}
	full := map[string]any{"total": float64(50)}
	list := make([]any, 20)
	for i := range list {
		list[i] = map[string]any{"CID": i + 1, "content": "x"}
	}
	full["list"] = list
	res4 := mapCommentList(full, 1)
	if res4["hasNext"] != true {
		t.Fatalf("满页且 total=50 → hasNext true, got %v", res4["hasNext"])
	}
	res5 := mapCommentList(full, 3)
	if res5["hasNext"] != false {
		t.Fatalf("page=3 时 3*20>=50 → hasNext false, got %v", res5["hasNext"])
	}
}

// ── ComicItemFromUpstream(§2.1;live.py _comic_item) ──

func TestComicItemFromUpstreamCover(t *testing.T) {
	// 相对路径 + query 剥离
	m := ComicItemFromUpstream("123", mustJSON(t, `{"image": "/media/albums/123_3x4.jpg?u=abc", "name": "测试"}`))
	if m["coverUrl"] != imageProxyURL("media/albums/123_3x4.jpg", "0", "123") {
		t.Fatalf("coverUrl 应剥 query, got %v", m["coverUrl"])
	}
	// 绝对 URL 只留路径 + 反斜杠归一
	m2 := ComicItemFromUpstream("456", mustJSON(t, `{"image": "https://cdn.x.com/media/a\\b.jpg?q=1"}`))
	if m2["coverUrl"] != imageProxyURL("media/a/b.jpg", "0", "456") {
		t.Fatalf("绝对 URL 应只留路径, got %v", m2["coverUrl"])
	}
	// 无 image → 回退 media/albums/{aid}_3x4.jpg
	m3 := ComicItemFromUpstream("789", mustJSON(t, `{}`))
	if m3["coverUrl"] != imageProxyURL("media/albums/789_3x4.jpg", "0", "789") {
		t.Fatalf("缺 image 应回退默认封面, got %v", m3["coverUrl"])
	}
}

func TestComicItemFromUpstreamFields(t *testing.T) {
	m := ComicItemFromUpstream("100", mustJSON(t, `{
		"name": "标题",
		"author": "作者",
		"category": {"id": "1", "title": "同人"},
		"category_sub": {"id": "2", "title": "长篇"},
		"tags": ["a", "b"],
		"likes": "[1K]",
		"views": "40K",
		"images_count": 12,
		"adddate": "2025-10-04"
	}`))
	if m["aid"] != "100" || m["title"] != "标题" || m["author"] != "作者" {
		t.Fatalf("基础字段映射错误: %v", m)
	}
	if m["category"] != "同人" {
		t.Fatalf("category 应取 title, got %v", m["category"])
	}
	if m["categorySub"] != "长篇" {
		t.Fatalf("categorySub 应取 title, got %v", m["categorySub"])
	}
	if tags, ok := m["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("tags = %v", m["tags"])
	}
	if m["likes"] != 1000 || m["views"] != 40000 || m["imageCount"] != 12 {
		t.Fatalf("计数解析错误: likes=%v views=%v imageCount=%v", m["likes"], m["views"], m["imageCount"])
	}
	if m["updateAt"] != "2025-10-04" {
		t.Fatalf("updateAt 应优先 adddate, got %v", m["updateAt"])
	}

	// category 非 dict → str();category_sub 缺失 → null;epoch update_at → 东八区日期
	m2 := ComicItemFromUpstream("101", mustJSON(t, `{"category": "单本", "update_at": 1700000000}`))
	if m2["category"] != "单本" {
		t.Fatalf("category 非 dict 应 str(), got %v", m2["category"])
	}
	if m2["categorySub"] != nil {
		t.Fatalf("category_sub 缺失应为 null, got %v", m2["categorySub"])
	}
	if m2["updateAt"] != "2023-11-15" {
		t.Fatalf("updateAt epoch → 东八区日期, got %v", m2["updateAt"])
	}
	// 数值缺失 → 0
	if m2["likes"] != 0 || m2["views"] != 0 || m2["imageCount"] != 0 {
		t.Fatalf("缺失计数应为 0")
	}
}

// ── hasNextPage(live.py _has_next,无 max_page) ──

func TestHasNextPage(t *testing.T) {
	if hasNextPage(1, 0, 5, 20) {
		t.Fatal("本页不满一页 → false(total 缺失/为 0 兜底)")
	}
	if !hasNextPage(1, 100, 20, 20) {
		t.Fatal("满页且未到末页 → true")
	}
	if hasNextPage(5, 100, 20, 20) {
		t.Fatal("5*20>=100 → false")
	}
	// live.py _has_next 语义:total 无效(0)但本页满时返回 true(与 Python 一致)
	if !hasNextPage(2, 0, 20, 20) {
		t.Fatal("total=0 且满页 → true(Python _has_next 口径)")
	}
}

// ── SessionUID ──

func TestSessionUID(t *testing.T) {
	if got := SessionUID(map[string]any{"uid": "123"}); got != "123" {
		t.Fatalf("uid = %q", got)
	}
	if got := SessionUID(map[string]any{"userId": "456"}); got != "456" {
		t.Fatalf("userId 兼容形态 = %q", got)
	}
	if got := SessionUID(nil); got != "" {
		t.Fatalf("nil 快照应返回空串, got %q", got)
	}
}

// ── anyToInt ──

func TestAnyToInt(t *testing.T) {
	if n, ok := anyToInt(float64(42)); !ok || n != 42 {
		t.Fatalf("float64 → int 失败: %v %v", n, ok)
	}
	if n, ok := anyToInt("17"); !ok || n != 17 {
		t.Fatalf("数字字符串 → int 失败: %v %v", n, ok)
	}
	if _, ok := anyToInt("abc"); ok {
		t.Fatal("非数字字符串应失败")
	}
	if _, ok := anyToInt(nil); ok {
		t.Fatal("nil 应失败")
	}
}


func TestPrettifySignMsg(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Jcoin:70 EXP:70", "签到成功!获得 70 金币、70 经验"},
		{"jcoin:5,exp:3", "签到成功!获得 5 金币、3 经验"},
		{"Jcoin : 120 , EXP : 80", "签到成功!获得 120 金币、80 经验"},
		{"今日已签到", "今日已签到"},
		{"", ""},
	}
	for _, c := range cases {
		if got := prettifySignMsg(c.in); got != c.want {
			t.Fatalf("prettifySignMsg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
