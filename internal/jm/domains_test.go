package jm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// resetDomainState 把进程级域名池状态清回初值,供各测试独立使用。
func resetDomainState(t *testing.T) {
	t.Helper()
	domainMu.Lock()
	domainUpdated = nil
	domainTriedAt = time.Time{}
	domainRetryAt = time.Time{}
	domainMu.Unlock()
	domainErr.Store("")
	t.Cleanup(func() {
		domainMu.Lock()
		domainUpdated = nil
		domainTriedAt = time.Time{}
		domainRetryAt = time.Time{}
		domainMu.Unlock()
		domainErr.Store("")
	})
}

// 静态兜底池不允许再放已下线的域名:2026-11-24 上游把 www.cdnhjk.net /
// www.cdngwc.cc 双双下线,旧静态池整组返回 nginx 404,整站「JM 服务端返回错误」。
func TestDefaultAPIDomainsAreNotKnownDead(t *testing.T) {
	dead := map[string]bool{
		"www.cdnhjk.net": true,
		"www.cdngwc.cc":  true,
	}
	if len(defaultAPIDomains) == 0 {
		t.Fatal("静态兜底池为空")
	}
	for _, d := range defaultAPIDomains {
		if dead[d] {
			t.Errorf("静态兜底池仍包含已下线域名 %q", d)
		}
	}
}

// 域名池未刷新成功时必须回落到静态池(而不是空池),否则出网路径会直接报「无可用域名」。
func TestAPIDomainsFallbackToStaticWhenNeverRefreshed(t *testing.T) {
	resetDomainState(t)
	got := apiDomainsWithUpdated()
	if len(got) != len(defaultAPIDomains) {
		t.Fatalf("回退池 = %v, want %v", got, defaultAPIDomains)
	}
	st := APIDomainsStatus()
	if st.Source != "static" && st.Source != "env" {
		t.Errorf("Source = %q, want static/env", st.Source)
	}
}

// 刷新成功后动态池优先于静态池,且状态里能看出刷新时刻与来源。
func TestAPIDomainsPreferDynamicPool(t *testing.T) {
	resetDomainState(t)
	domainMu.Lock()
	domainUpdated = []string{"dyn-a.example", "dyn-b.example"}
	domainTriedAt = time.Now()
	domainRetryAt = time.Now().Add(apiDomainRefreshTTL)
	domainMu.Unlock()

	got := apiDomainsWithUpdated()
	if len(got) != 2 || got[0] != "dyn-a.example" {
		t.Fatalf("动态池未优先生效: %v", got)
	}
	st := APIDomainsStatus()
	if st.Source != "dynamic" {
		t.Errorf("Source = %q, want dynamic", st.Source)
	}
	if st.RefreshedAt.IsZero() {
		t.Error("RefreshedAt 未记录")
	}
	if st.Stale {
		t.Error("TTL 未到期却被标记 stale")
	}
}

// 关键回归:刷新失败不能被永久钉死(TTL 到期后必须再次尝试)。
// 旧实现用 sync.Once,一次失败即锁死到进程退出 —— 就是线上那个故障。
func TestRefreshAPIDomainsRetriesAfterFailure(t *testing.T) {
	resetDomainState(t)

	// 两个更新服务都 500,模拟代理/更新服务不可达
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	origServers := domainUpdateServers
	domainUpdateServers = []string{srv.URL}
	t.Cleanup(func() { domainUpdateServers = origServers })

	refreshAPIDomains(context.Background(), srv.Client())
	if calls == 0 {
		t.Fatal("首次刷新没有发起请求")
	}
	if msg, _ := domainErr.Load().(string); msg == "" {
		t.Error("刷新失败未记录原因")
	}

	// 退避期内不再打更新服务
	before := calls
	refreshAPIDomains(context.Background(), srv.Client())
	if calls != before {
		t.Errorf("退避期内重复请求: calls %d -> %d", before, calls)
	}

	// 把退避点拨到过去,模拟 apiDomainRetryAfter 已过 → 必须重试
	domainMu.Lock()
	domainRetryAt = time.Now().Add(-time.Second)
	domainMu.Unlock()
	refreshAPIDomains(context.Background(), srv.Client())
	if calls == before {
		t.Fatal("退避期结束后没有重试:失败被永久钉死")
	}
}

// 刷新成功后清空错误并替换池(与失败路径对称)。
func TestRefreshAPIDomainsStoresFetchedPool(t *testing.T) {
	resetDomainState(t)

	payload, err := encodeRespData(
		[]byte(`{"Server":["n1.example","n2.example"]}`), "", apiDomainServerSecret)
	if err != nil {
		t.Fatalf("构造密文失败: %v", err)
	}
	// 上游文本前面有若干非 ASCII 字符,解析前要剥掉(SDK 同款)
	body := "\ufeff\ufeff" + payload
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	origServers := domainUpdateServers
	domainUpdateServers = []string{srv.URL}
	t.Cleanup(func() { domainUpdateServers = origServers })

	refreshAPIDomains(context.Background(), srv.Client())

	got := apiDomainsWithUpdated()
	if len(got) != 2 || got[0] != "n1.example" || got[1] != "n2.example" {
		t.Fatalf("动态池未写入: %v", got)
	}
	if msg, _ := domainErr.Load().(string); msg != "" {
		t.Errorf("成功后仍残留错误: %q", msg)
	}
	if gotReq == nil || gotReq.Header.Get("User-Agent") != appUserAgent {
		t.Error("更新服务请求缺少 app User-Agent")
	}
	st := APIDomainsStatus()
	if st.Stale {
		t.Error("刚刷新成功却标记 stale")
	}
}

// 死域名(nginx 404 之类的裸 HTML)必须被识别为「域名级失效」,
// 而纯网络错误 / 业务拒绝不应触发域名轮换。
//
// 注意:reqAPI 里 *APIError 会提前短路(业务拒绝不重试),所以这里只覆盖
// doAPIRequest 可能抛出的裸 error 形态。
func TestIsDeadDomainError(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"nginx 404 HTML", "HTTP 404: <html><head><title>404 Not Found</title></head></html>", true},
		{"HTTP 502", "HTTP 502: bad gateway", true},
		{"HTTP 503", "HTTP 503: upstream busy", true},
		{"HTTP 521", "HTTP 521: web server is down", true},
		{"风控 HTML(非 JSON)", "响应非 JSON: <html>风控</html>", true},
		{"network timeout", "dial tcp: i/o timeout", false},
		{"connection reset", "read tcp 172.24.0.2:34604->192.168.5.196:1070: read: connection reset by peer", false},
	}
	for _, tc := range cases {
		if got := isDeadDomainError(&plainErr{tc.text}); got != tc.want {
			t.Errorf("%s: isDeadDomainError(%q) = %v, want %v", tc.name, tc.text, got, tc.want)
		}
	}
	if isDeadDomainError(nil) {
		t.Error("nil error 被判为死域名")
	}
}

// plainErr 是只带文本的 error,用来模拟 doAPIRequest 返回的裸错误。
type plainErr struct{ s string }

func (e *plainErr) Error() string { return e.s }

// fakeUpstream 按 host 返回预设响应,用来在单测里跑真实的 reqAPI 轮换路径。
// hosts 里没有的 host(例如更新服务)走转发,把请求交给 fallback。
type fakeUpstream struct {
	byHost   map[string]func() (int, string)
	fallback http.RoundTripper
	seen     []string
}

func (f *fakeUpstream) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	f.seen = append(f.seen, host)
	h, ok := f.byHost[host]
	if !ok {
		if f.fallback != nil {
			return f.fallback.RoundTrip(req)
		}
		return nil, fmt.Errorf("no fake for host %s", host)
	}
	status, body := h()
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// asClient 让更新服务(不同 host)走真实 httptest 服务器。
func (f *fakeUpstream) asClient(srv *httptest.Server) *http.Client {
	f.fallback = srv.Client().Transport
	return &http.Client{Transport: f}
}

// 关键回归:前两个域名都返回 nginx 404 时,reqAPI 必须继续翻到池里第三个可用域名。
// 旧实现按 attempt%len(domains) 只试 2 个位置,4 个域名的池里剩两个可用也永远翻不出来。
func TestReqAPIRotatesThroughWholePool(t *testing.T) {
	resetDomainState(t)

	// 第三个域名的合法响应:信封里 data 是 AES 密文
	aliveBody, err := encodeRespData([]byte(`{"ok":true}`), "1700000000", "")
	if err != nil {
		t.Fatalf("构造响应失败: %v", err)
	}
	up := &fakeUpstream{byHost: map[string]func() (int, string){
		"dead1.example": func() (int, string) { return http.StatusNotFound, "<html>404 Not Found</html>" },
		"dead2.example": func() (int, string) { return http.StatusNotFound, "<html>404 Not Found</html>" },
		"alive.example": func() (int, string) {
			return http.StatusOK, `{"code":200,"data":"` + aliveBody + `"}`
		},
	}}

	domainMu.Lock()
	domainUpdated = []string{"dead1.example", "dead2.example", "alive.example"}
	domainTriedAt = time.Now() // 刚刷新过 → 死域名触发的强制刷新会被最小间隔挡住
	domainRetryAt = time.Now().Add(apiDomainRefreshTTL)
	domainMu.Unlock()

	c := &Client{
		http:       &http.Client{Transport: up},
		cookies:    map[string]string{},
		appVersion: defaultAppVersion,
	}
	c.ts.Store(1700000000)
	c.initOnce.Do(func() {}) // 跳过 ensureInit 里的 cookies 引导

	data, err := c.reqAPI(context.Background(), http.MethodGet, "/promote?page=0&lang=CN", nil)
	if err != nil {
		t.Fatalf("reqAPI 未翻到可用域名: %v", err)
	}
	if !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("返回内容不符: %s", data)
	}
	want := []string{"dead1.example", "dead2.example", "alive.example"}
	if len(up.seen) != len(want) {
		t.Fatalf("轮换次数 = %v, want %v", up.seen, want)
	}
	for i := range want {
		if up.seen[i] != want[i] {
			t.Fatalf("轮换顺序 = %v, want %v", up.seen, want)
		}
	}
}

// 关键回归:域名池整组换新时,刷新拿到的域名必须在同一次请求里就被用上,
// 不能等下一次请求(用户视角就是"点了没反应,再点一次才行")。
//
// 这里让首个域名返回 nginx 404,刷新服务给出一个全新的可用域名,
// 断言 reqAPI 在一次调用里就完成了「走旧池 → 遇 404 → 刷新 → 走新域名」。
func TestReqAPIPicksUpRefreshedDomainInSameCall(t *testing.T) {
	resetDomainState(t)

	aliveBody, err := encodeRespData([]byte(`{"ok":true}`), "1700000000", "")
	if err != nil {
		t.Fatalf("构造响应失败: %v", err)
	}
	up := &fakeUpstream{byHost: map[string]func() (int, string){
		"stale.example": func() (int, string) {
			return http.StatusNotFound, "<html>404 Not Found</html>"
		},
		"fresh.example": func() (int, string) {
			return http.StatusOK, `{"code":200,"data":"` + aliveBody + `"}`
		},
	}}

	// 刷新服务:返回只含 fresh.example 的新池
	payload, err := encodeRespData(
		[]byte(`{"Server":["fresh.example"]}`), "", apiDomainServerSecret)
	if err != nil {
		t.Fatalf("构造密文失败: %v", err)
	}
	updateSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\ufeff\ufeff" + payload))
	}))
	t.Cleanup(updateSrv.Close)
	origServers := domainUpdateServers
	domainUpdateServers = []string{updateSrv.URL}
	t.Cleanup(func() { domainUpdateServers = origServers })

	// 旧池只有一个死域名;上次刷新在很久以前,允许强制刷新
	domainMu.Lock()
	domainUpdated = []string{"stale.example"}
	domainTriedAt = time.Now().Add(-apiDomainRetryAfter - time.Minute)
	domainRetryAt = time.Now().Add(apiDomainRefreshTTL)
	domainMu.Unlock()

	c := &Client{
		http:       up.asClient(updateSrv),
		cookies:    map[string]string{},
		appVersion: defaultAppVersion,
	}
	c.ts.Store(1700000000)
	c.initOnce.Do(func() {})

	data, err := c.reqAPI(context.Background(), http.MethodGet, "/promote?page=0&lang=CN", nil)
	if err != nil {
		t.Fatalf("刷新后仍失败: %v", err)
	}
	if !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("返回内容不符: %s", data)
	}
	if got := apiDomainsWithUpdated(); len(got) != 1 || got[0] != "fresh.example" {
		t.Fatalf("池未刷新: %v", got)
	}
}
