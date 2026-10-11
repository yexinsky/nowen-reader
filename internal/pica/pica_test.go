package pica

import (
	"os"
	"path/filepath"
	"testing"
)

// 签名向量由 Python(api_test.py 同款 HMAC-SHA256)独立生成,交叉验证 Go 实现:
// sign(path, now, nonce, method) = HMAC-SHA256(secret, lowercase(path+now+nonce+METHOD+apiKey))
func TestSignatureKnownVectors(t *testing.T) {
	cases := []struct {
		path, now, nonce, method, want string
	}{
		{
			path: "comics/leaderboard?tt=D7&ct=VC", now: "1728648000",
			nonce: "aabbccddeeff00112233445566778899", method: "GET",
			want: "4fd20d2a8d87b51e2d155095e2b341d33426668cbe1420ac2f3808008722f6c9",
		},
		{
			path: "auth/sign-in", now: "1728648000",
			nonce: "00112233445566778899aabbccddeeff", method: "POST",
			want: "413992854e4139ffc57eb3249709f4f787767dc2fe0f72a74e0f4db5e22f4484",
		},
	}
	for _, tc := range cases {
		if got := signature(tc.path, tc.now, tc.nonce, tc.method); got != tc.want {
			t.Errorf("signature(%q,%q,%q,%q) = %s, want %s", tc.path, tc.now, tc.nonce, tc.method, got, tc.want)
		}
	}
	// 方法大小写不影响结果(内部统一大写后转小写)
	if got := signature("auth/sign-in", "1728648000", "00112233445566778899aabbccddeeff", "post"); got != cases[1].want {
		t.Errorf("小写 method 签名不一致: %s", got)
	}
	// 密钥长度哨兵:上游密钥固定 63 字符,防止转义被意外改动
	if len(apiSecret) != 63 {
		t.Fatalf("apiSecret 长度 = %d, want 63", len(apiSecret))
	}
}

func TestBuildHeaders(t *testing.T) {
	h := buildHeaders("users/favourite?s=da&page=1", "GET", "jwt-token", "original")
	if h.Get("api-key") != apiKey {
		t.Errorf("api-key 不符: %s", h.Get("api-key"))
	}
	if h.Get("accept") != acceptValue {
		t.Errorf("accept 不符: %s", h.Get("accept"))
	}
	if h.Get("authorization") != "jwt-token" {
		t.Errorf("authorization 应为裸 JWT: %s", h.Get("authorization"))
	}
	if h.Get("image-quality") != "original" {
		t.Errorf("image-quality 不符: %s", h.Get("image-quality"))
	}
	if h.Get("signature") == "" || h.Get("nonce") == "" || h.Get("time") == "" {
		t.Error("签名三件套缺失")
	}
	if h.Get("Content-Type") != "" {
		t.Errorf("GET 不应带 Content-Type: %s", h.Get("Content-Type"))
	}
	// 登录请求不带 authorization
	h2 := buildHeaders("auth/sign-in", "POST", "", "original")
	if h2.Get("authorization") != "" {
		t.Errorf("登录不应带 authorization: %s", h2.Get("authorization"))
	}
	if h2.Get("Content-Type") != "application/json; charset=UTF-8" {
		t.Errorf("POST 应带 JSON Content-Type: %s", h2.Get("Content-Type"))
	}
}

func TestMapUpstreamCode(t *testing.T) {
	cases := []struct {
		code   int
		msg    string
		want   int
		msgHas string
	}{
		{1004, "邮箱或密码错误", CodeBadCredentials, ""},
		{1002, "未登录", CodeUnauthorized, ""},
		{401, "unauthorized", CodeUnauthorized, ""},
		{1017, "", CodeUpstream, "收藏已满"},
		{1019, "", CodeUpstream, "等级"},
		{1023, "", CodeUpstream, "频繁"},
		{1007, "", CodeNotFound, ""},
		{500, "服务器内部错误", CodeUpstream, "内部错误"},
	}
	for _, tc := range cases {
		err := mapUpstreamCode(tc.code, tc.msg)
		if err == nil {
			t.Fatalf("code=%d 应产生错误", tc.code)
		}
		if err.Code != tc.want {
			t.Errorf("code=%d → %d, want %d", tc.code, err.Code, tc.want)
		}
		if tc.msgHas != "" && !contains(err.Msg, tc.msgHas) {
			t.Errorf("code=%d msg = %q, 应含 %q", tc.code, err.Msg, tc.msgHas)
		}
	}
	if err := mapUpstreamCode(200, ""); err != nil {
		t.Errorf("code=200 不应产生错误: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestEnvelopeCodeInt(t *testing.T) {
	// code 可能为字符串("1004")或数字(上游实测两种形态都有)
	env := &apiEnvelope{Code: []byte(`"1004"`)}
	if got := env.codeInt(); got != 1004 {
		t.Errorf("字符串 code = %d, want 1004", got)
	}
	env = &apiEnvelope{Code: []byte(`200`)}
	if got := env.codeInt(); got != 200 {
		t.Errorf("数字 code = %d, want 200", got)
	}
}

func TestBuildImageURL(t *testing.T) {
	cases := []struct {
		fs, path, want string
	}{
		{
			fs:   "https://storage-b.picacomic.com",
			path: "tobs/sub_storage_1/a0/2a/a02af421-0000.jpg",
			want: "https://storage-b.picacomic.com/static/tobs/sub_storage_1/a0/2a/a02af421-0000.jpg",
		},
		{
			fs:   "https://storage-b.picacomic.com/static/",
			path: "tobs/x.jpg",
			want: "https://storage-b.picacomic.com/static/tobs/x.jpg",
		},
		{
			fs:   "https://storage-b.picacomic.com/static",
			path: "tobs/x.jpg",
			want: "https://storage-b.picacomic.com/static/tobs/x.jpg",
		},
		{
			// static/static 归一(picacg-qt DownloadBookReq 同款修正)
			fs:   "https://storage-b.picacomic.com/static/",
			path: "static/tobs/x.jpg",
			want: "https://storage-b.picacomic.com/static/tobs/x.jpg",
		},
	}
	for _, tc := range cases {
		if got := BuildImageURL(tc.fs, tc.path); got != tc.want {
			t.Errorf("BuildImageURL(%q,%q) = %q, want %q", tc.fs, tc.path, got, tc.want)
		}
	}
}

func TestImageCandidatesTobeimgJump(t *testing.T) {
	url := BuildImageURL("https://storage-b.picacomic.com", "static/tobeimg/tobs/x.jpg")
	candidates := imageCandidates("https://storage-b.picacomic.com", "static/tobeimg/tobs/x.jpg")
	// picacg-qt 同款:替换 host 为 img 域并去掉 /static/tobeimg 段(绕过 301)
	if candidates[0] != "https://img.picacomic.com/tobs/x.jpg" {
		t.Errorf("tobeimg 预跳转候选不符: %s", candidates[0])
	}
	if candidates[1] != url {
		t.Errorf("第二候选应为原始 URL: %s", candidates[1])
	}
}

func TestValidateImagePathAndFS(t *testing.T) {
	if err := ValidateImagePath("tobs/sub/a.jpg"); err != nil {
		t.Errorf("合法 path 被拒: %v", err)
	}
	if err := ValidateImagePath("1.webp:300:400"); err != nil {
		t.Errorf("imgproxy 后缀 path 被拒: %v", err)
	}
	for _, bad := range []string{"", "/abs.jpg", "..\\x.jpg", "a/../b.jpg", "http://evil/x.jpg", "a b.jpg"} {
		if err := ValidateImagePath(bad); err == nil {
			t.Errorf("非法 path 未被拒: %q", bad)
		}
	}
	if err := ValidateImageFS("https://storage-b.picacomic.com"); err != nil {
		t.Errorf("合法 fs 被拒: %v", err)
	}
	if err := ValidateImageFS("https://img.tipatipa.xyz"); err != nil {
		t.Errorf("合法跳转域 fs 被拒: %v", err)
	}
	for _, bad := range []string{"https://evil.com", "ftp://storage-b.picacomic.com", "not a url"} {
		if err := ValidateImageFS(bad); err == nil {
			t.Errorf("非法 fs 未被拒: %q", bad)
		}
	}
}

func TestComicItemFromUpstream(t *testing.T) {
	item := comicItemFromUpstream(map[string]any{
		"_id":       "abc123",
		"title":     "标题",
		"author":    "作者",
		"likesCount": nil,
		"totalLikes": float64(12),
		"totalViews": float64(345),
		"pagesCount": float64(26),
		"epsCount":   float64(1),
		"finished":   false,
		"categories": []any{"全彩", "百合"},
		"updated_at": "2026-10-11T00:00:00Z",
		"thumb": map[string]any{
			"fileServer": "https://storage-b.picacomic.com",
			"path":       "tobs/x.jpg",
		},
	})
	if item["id"] != "abc123" || item["title"] != "标题" || item["author"] != "作者" {
		t.Errorf("基础字段不符: %v", item)
	}
	if item["likes"].(int) != 12 || item["views"].(int) != 345 || item["imageCount"].(int) != 26 {
		t.Errorf("计数字段不符: %v", item)
	}
	cats := item["categories"].([]string)
	if len(cats) != 2 || cats[0] != "全彩" {
		t.Errorf("categories 不符: %v", cats)
	}
	wantCover := imageProxyURL("https://storage-b.picacomic.com", "tobs/x.jpg")
	if item["coverUrl"] != wantCover {
		t.Errorf("coverUrl 不符: %v, want %v", item["coverUrl"], wantCover)
	}
}

func TestDecodePageAndListPage(t *testing.T) {
	data := map[string]any{
		"comics": map[string]any{
			"docs":  []any{map[string]any{"_id": "a", "title": "A"}, map[string]any{"_id": "b", "title": "B"}},
			"total": float64(42), "pages": float64(3), "limit": float64(20), "page": float64(2),
		},
	}
	env, ok := decodePage(data, "comics")
	if !ok {
		t.Fatal("decodePage 应成功")
	}
	if env.Total != 42 || env.Pages != 3 {
		t.Fatalf("分页字段不符: %d/%d", env.Total, env.Pages)
	}
	page := listPage(2, env, comicItemsFromUpstream(env.Docs))
	if page["hasNext"] != true {
		t.Errorf("page=2/3 应有下一页: %v", page["hasNext"])
	}
	if page["total"].(int) != 42 {
		t.Errorf("total 不符: %v", page["total"])
	}
	list := page["list"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["id"] != "a" {
		t.Errorf("list 映射不符: %v", list)
	}
	// 尾页无下一页
	if listPage(3, env, nil)["hasNext"] != false {
		t.Error("page=3/3 不应有下一页")
	}
	// total 缺失回退 docs 长度
	data2 := map[string]any{"comics": map[string]any{"docs": []any{map[string]any{"_id": "a"}}}}
	env2, _ := decodePage(data2, "comics")
	if env2.Total != 1 {
		t.Errorf("total 回退不符: %d", env2.Total)
	}
}

func TestProfileFromData(t *testing.T) {
	user := profileFromData(map[string]any{
		"user": map[string]any{
			"email": "a@b.c", "name": "昵称", "level": float64(1),
			"isPunched": true,
			"avatar":    map[string]any{"fileServer": "https://storage-b.picacomic.com", "path": "a.jpg"},
		},
	})
	if user == nil || user["name"] != "昵称" || user["level"].(int) != 1 || user["isPunched"] != true {
		t.Fatalf("user 映射不符: %v", user)
	}
	if user["avatarUrl"] != imageProxyURL("https://storage-b.picacomic.com", "a.jpg") {
		t.Errorf("avatarUrl 不符: %v", user["avatarUrl"])
	}
	if profileFromData(map[string]any{"other": 1}) != nil {
		t.Error("无 user 数据应返回 nil")
	}
}

func TestStoreSettingsAndHistory(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// 默认设置:直连(env 为空)+ original + downloadTags 开
	def := s.LoadSettings("")
	if def.Proxy != "" || def.ImageQuality != "original" || !def.DownloadTags {
		t.Fatalf("默认设置不符: %+v", def)
	}
	// env 代理回退:落盘值缺省时使用 env
	if got := NewStore(t.TempDir()).LoadSettings("http://127.0.0.1:5070").Proxy; got != "http://127.0.0.1:5070" {
		t.Fatalf("env 代理回退不符: %q", got)
	}

	// 写读回环
	if err := s.SaveSettings(Settings{Proxy: "http://127.0.0.1:5070", ImageQuality: "low", DownloadDir: "D:/x", DownloadTags: false}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	st := s.LoadSettings("")
	if st.Proxy != "http://127.0.0.1:5070" || st.ImageQuality != "low" || st.DownloadTags {
		t.Fatalf("设置回读不符: %+v", st)
	}

	// 非法画质归一
	if err := s.SaveSettings(Settings{ImageQuality: "bogus"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if got := s.LoadSettings("").ImageQuality; got != "original" {
		t.Fatalf("非法画质应归一 original: %s", got)
	}

	// 历史:幂等键 id+order, updatedAt 倒序
	mk := func(id string, order int, updatedAt string) HistoryItem {
		return HistoryItem{ID: id, Title: id, Order: order, UpdatedAt: updatedAt}
	}
	if err := s.ReportHistory(mk("a", 1, "2026-01-01T00:00:00")); err != nil {
		t.Fatalf("ReportHistory: %v", err)
	}
	_ = s.ReportHistory(mk("b", 1, "2026-01-02T00:00:00"))
	_ = s.ReportHistory(mk("a", 1, "2026-01-03T00:00:00")) // 同键更新,不重复
	list, total, _ := s.ListHistory(1)
	if total != 2 {
		t.Fatalf("历史应去重为 2 条: %d", total)
	}
	if list[0].ID != "a" || list[0].UpdatedAt != "2026-01-03T00:00:00" {
		t.Fatalf("历史置顶不符: %+v", list)
	}
	_ = s.ClearHistory()
	_, total2, _ := s.ListHistory(1)
	if total2 != 0 {
		t.Fatalf("清空后应为 0: %d", total2)
	}

	// settings.json 落盘位置
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatalf("settings.json 未落盘: %v", err)
	}
}

func TestParsePageQuery(t *testing.T) {
	if n, ok := ParsePageQuery(""); !ok || n != 1 {
		t.Errorf("空页码应为 1: %d %v", n, ok)
	}
	if _, ok := ParsePageQuery("0"); ok {
		t.Error("0 应非法")
	}
	if n, ok := ParsePageQuery("3"); !ok || n != 3 {
		t.Errorf("3 应合法: %d %v", n, ok)
	}
}

func TestContentTypeOf(t *testing.T) {
	if got := contentTypeOf("image/webp; charset=binary", ""); got != "image/webp" {
		t.Errorf("带参 Content-Type 不符: %s", got)
	}
	if got := contentTypeOf("", "x/y/1.webp"); got != "image/webp" {
		t.Errorf("webp 扩展名推断不符: %s", got)
	}
	if got := contentTypeOf("text/html", "x.jpg"); got != "image/jpeg" {
		t.Errorf("非 image Content-Type 应兜底: %s", got)
	}
}

func TestSniffImageType(t *testing.T) {
	// 实测:上游对 webp 字节错误标注 image/jpeg,以魔数嗅探为准
	webp := append([]byte("RIFF"), append([]byte{0x00, 0x00, 0x00, 0x00}, []byte("WEBP")...)...)
	if got := sniffImageType(webp); got != "image/webp" {
		t.Errorf("webp 魔数嗅探不符: %s", got)
	}
	if got := sniffImageType([]byte{0xFF, 0xD8, 0xFF, 0xE0}); got != "image/jpeg" {
		t.Errorf("jpeg 魔数嗅探不符: %s", got)
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00")
	if got := sniffImageType(png); got != "image/png" {
		t.Errorf("png 魔数嗅探不符: %s", got)
	}
	if got := sniffImageType([]byte("GIF89a0100")); got != "image/gif" {
		t.Errorf("gif 魔数嗅探不符: %s", got)
	}
	if got := sniffImageType([]byte("not an image")); got != "" {
		t.Errorf("非图片应返回空: %s", got)
	}
}
