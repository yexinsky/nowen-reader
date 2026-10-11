package pica

// 真实上游端到端测试(默认跳过,需显式开启):
//
//	PICA_LIVE_TEST=1 go test ./internal/pica/ -run TestLive -v -timeout 15m
//	可选:PICA_LIVE_PROXY=http://127.0.0.1:5070(默认直连)、
//	     PICA_LIVE_EMAIL / PICA_LIVE_PASSWORD(默认 API 文档实测账号)
//
// 覆盖链路:登录 → 分类 → 周榜 → 搜索 → 详情 → 章节 → 图片列表 → 图片字节。
// 同时验证假响应检测:登录响应无 data 时必须报错而非静默成功。

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func liveProxy() string {
	if v := os.Getenv("PICA_LIVE_PROXY"); v != "" {
		return v
	}
	return ""
}

func liveCreds() (string, string) {
	email := os.Getenv("PICA_LIVE_EMAIL")
	password := os.Getenv("PICA_LIVE_PASSWORD")
	if email == "" {
		email = "woainilinxinru"
		password = "qwe123321"
	}
	return email, password
}

func newLiveClient(t *testing.T) *Client {
	t.Helper()
	cl, err := NewClient(liveProxy(), "original")
	if err != nil {
		t.Fatalf("构建客户端失败: %v", err)
	}
	return cl
}

// TestLiveSignIn 登录拿 JWT + 用户资料(验证 TLS 指纹路径与签名)。
func TestLiveSignIn(t *testing.T) {
	if os.Getenv("PICA_LIVE_TEST") == "" {
		t.Skip("需要 PICA_LIVE_TEST=1 才执行真实上游测试")
	}
	cl := newLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	email, password := liveCreds()
	token, user, err := cl.SignIn(ctx, email, password)
	if err != nil {
		t.Fatalf("登录失败: %v (upstream=%s)", err, UpstreamDetail(err))
	}
	if token == "" {
		t.Fatal("token 为空")
	}
	t.Logf("登录成功: name=%v level=%v token=%s...", user["name"], user["level"], token[:12])
	profile, err := cl.Profile(ctx, token)
	if err != nil {
		t.Fatalf("用户资料失败: %v", err)
	}
	t.Logf("资料: %v", profile)
}

// TestLiveContentChain 内容链路(哔咔内容接口一律要求登录,实测无 token 被拒):
// 登录 → 分类 → 周榜 → 搜索 → 详情 → 章节 → 图片列表 → 图片字节。
func TestLiveContentChain(t *testing.T) {
	if os.Getenv("PICA_LIVE_TEST") == "" {
		t.Skip("需要 PICA_LIVE_TEST=1 才执行真实上游测试")
	}
	cl := newLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 0) 登录拿 token
	email, password := liveCreds()
	token, _, err := cl.SignIn(ctx, email, password)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	// 1) 分类
	cats, err := cl.Categories(ctx, token)
	if err != nil {
		t.Fatalf("分类失败: %v (upstream=%s)", err, UpstreamDetail(err))
	}
	catList := cats.(map[string]any)["list"].([]any)
	if len(catList) == 0 {
		t.Fatal("分类列表为空")
	}
	firstCat := catList[0].(map[string]any)["title"].(string)
	t.Logf("分类 %d 个,首个: %s", len(catList), firstCat)

	// 2) 周榜
	board, err := cl.Leaderboard(ctx, token, "D7")
	if err != nil {
		t.Fatalf("周榜失败: %v", err)
	}
	books := board.(map[string]any)["list"].([]any)
	if len(books) == 0 {
		t.Fatal("周榜为空")
	}
	first := books[0].(map[string]any)
	bookID := first["id"].(string)
	t.Logf("周榜第 1: %v (id=%s, 封面=%s)", first["title"], bookID, first["coverUrl"])

	// 3) 分类浏览
	browse, err := cl.ComicsByCategory(ctx, token, firstCat, "dd", 1)
	if err != nil {
		t.Fatalf("分类浏览失败: %v", err)
	}
	t.Logf("分类浏览 %s: total=%v", firstCat, browse.(map[string]any)["total"])

	// 4) 搜索
	search, err := cl.AdvancedSearch(ctx, token, SearchParams{Keyword: "百合", Sort: "dd"}, 1)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if total := search.(map[string]any)["total"].(int); total == 0 {
		t.Fatal("搜索结果为空")
	} else {
		t.Logf("搜索「百合」: total=%d", total)
	}

	// 5) 详情
	detail, err := cl.ComicDetail(ctx, token, bookID)
	if err != nil {
		t.Fatalf("详情失败: %v", err)
	}
	detailMap := detail.(map[string]any)
	epsCount := detailMap["epsCount"].(int)
	t.Logf("详情: %v eps=%d 标签=%v", detailMap["title"], epsCount, detailMap["tags"])

	// 6) 章节
	eps, err := cl.Eps(ctx, token, bookID, 1)
	if err != nil {
		t.Fatalf("章节失败: %v", err)
	}
	epList := eps.(map[string]any)["list"].([]any)
	if len(epList) == 0 {
		t.Fatal("章节列表为空")
	}
	firstOrder := epList[0].(map[string]any)["order"].(int)
	t.Logf("章节 %d 个,第 1 章 order=%d", len(epList), firstOrder)

	// 7) 图片列表
	pages, err := cl.EpsPages(ctx, token, bookID, firstOrder, 1)
	if err != nil {
		t.Fatalf("图片列表失败: %v", err)
	}
	imgList := pages.(map[string]any)["list"].([]any)
	if len(imgList) == 0 {
		t.Fatal("图片列表为空")
	}
	img0 := imgList[0].(PageImage)
	t.Logf("图片 %d 张,第 1 张: %s", len(imgList), img0.URL)

	// 8) 图片字节(直连上游,经候选 URL 轮换)
	imgClient, err := NewClient(liveProxy(), "original")
	if err != nil {
		t.Fatalf("图片客户端构建失败: %v", err)
	}
	data, ct, hit, err := FetchImage(ctx, imgClient.hc, nil, img0.FileServer, img0.Path, "original")
	if err != nil {
		t.Fatalf("图片下载失败: %v", err)
	}
	if len(data) < 1024 {
		t.Fatalf("图片过小: %d bytes", len(data))
	}
	t.Logf("图片下载成功: %d bytes ct=%s hit=%v", len(data), ct, hit)
}

// TestLiveFakeResponseDetection 假响应检测:错误凭据必须显式报错(1001),
// 而非被指纹风控投喂的假 success 静默骗过。
func TestLiveFakeResponseDetection(t *testing.T) {
	if os.Getenv("PICA_LIVE_TEST") == "" {
		t.Skip("需要 PICA_LIVE_TEST=1 才执行真实上游测试")
	}
	cl := newLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, _, err := cl.SignIn(ctx, "definitely-not-exist-account-xyz", "wrong-password")
	if err == nil {
		t.Fatal("错误凭据登录应报错(若返回成功说明假响应检测被绕过)")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("错误应为 *APIError: %v", err)
	}
	// 真实上游:1004 → 1001;被指纹拦截时的假响应 → 2001(带 TLS 检测提示)
	if apiErr.Code == CodeOK {
		t.Fatal("不应返回成功")
	}
	t.Logf("错误凭据按预期报错: code=%d msg=%s", apiErr.Code, apiErr.Msg)
	if apiErr.Code == CodeUpstream && strings.Contains(apiErr.Msg, "TLS") {
		t.Log("提示:当前出口被上游指纹风控拦截(2001 假响应),检查代理/网络环境")
	}
}
