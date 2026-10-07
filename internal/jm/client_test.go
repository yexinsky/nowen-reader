package jm

// 会话 cookies 合并与上游错误分类单测:
// - mergeLoginCookies:登录后完整 cookie jar 的合并优先级(SDK/live.py 语义);
// - apiEnvelope.message:msg/errorMsg/message 三字段读取顺序(上游 401 用 errorMsg);
// - mapUpstreamBusinessError:上游 401/未登录文案 → 1002,验证码 → 1003,其余 → 2001。

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// ── mergeLoginCookies:登录客户端累计 jar → 响应 Set-Cookie → AVS(s 字段) ──

func TestMergeLoginCookies(t *testing.T) {
	jar := map[string]string{
		"AVS":       "guest-session", // /setting 引导种下的游客会话
		"__cflb":    "origin-1",
		"ipcountry": "CN",
		"ipm5":      "abc123",
		"theme":     "light",
	}
	respCookies := []*http.Cookie{
		{Name: "AVS", Value: "server-session"}, // 登录响应刷新的会话
		{Name: "__cflb", Value: "origin-2"},    // 登录响应刷新的节点粘性
		{Name: "EMPTY", Value: ""},             // 空值 cookie 必须跳过
		{Name: "", Value: "noname"},
	}

	got := mergeLoginCookies(jar, respCookies, "s-field-avs")

	// 引导 cookies 保留(jar 完整性:会员端点裸 AVS 会被上游按未登录拒绝)
	if got["ipm5"] != "abc123" || got["ipcountry"] != "CN" || got["theme"] != "light" {
		t.Fatalf("引导 cookies 丢失: %#v", got)
	}
	// 响应 Set-Cookie 覆盖同名引导 cookie
	if got["__cflb"] != "origin-2" {
		t.Fatalf("__cflb 应被登录响应覆盖: %#v", got)
	}
	// AVS 优先级:返回值 s 字段最后写入
	if got["AVS"] != "s-field-avs" {
		t.Fatalf("AVS 应取 s 字段: %#v", got)
	}
	// 空值 cookie 不入库
	if _, ok := got["EMPTY"]; ok {
		t.Fatalf("空值 cookie 不应入库: %#v", got)
	}
	// 无 s 字段时回退响应 Set-Cookie 的 AVS
	got2 := mergeLoginCookies(jar, respCookies, "")
	if got2["AVS"] != "server-session" {
		t.Fatalf("无 s 字段应保留响应 AVS: %#v", got2)
	}
	// 入参 jar 不被修改
	if jar["AVS"] != "guest-session" || jar["__cflb"] != "origin-1" {
		t.Fatalf("入参 jar 被修改: %#v", jar)
	}
}

// ── apiEnvelope.message:msg → errorMsg → message,忽略空串与 null ──

func TestEnvelopeMessage(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"code":401,"data":"x","errorMsg":"請先登入會員"}`, "請先登入會員"},
		{`{"code":401,"message":"login required"}`, "login required"},
		{`{"code":400,"msg":"参数错误"}`, "参数错误"},
		{`{"code":400,"msg":"null","errorMsg":"降级信息"}`, "降级信息"},
		{`{"code":400,"msg":"","errorMsg":""}`, ""},
		{`{"code":200,"data":"x"}`, ""},
	}
	for _, c := range cases {
		var env apiEnvelope
		if err := json.Unmarshal([]byte(c.body), &env); err != nil {
			t.Fatalf("unmarshal %s: %v", c.body, err)
		}
		if got := env.message(); got != c.want {
			t.Fatalf("message(%s) = %q, want %q", c.body, got, c.want)
		}
	}
}

// ── mapUpstreamBusinessError:401/未登录 → 1002;验证码 → 1003;其余 → 2001 ──

func TestMapUpstreamBusinessError(t *testing.T) {
	cl := &Client{}

	// code=401 → 1002(登录态无效,前端据此提示重新登录)
	if err := cl.mapUpstreamBusinessError(401, "", []byte(`{"code":401}`)); err.(*APIError).Code != CodeUnauthorized {
		t.Fatalf("code 401 应映射 1002: %#v", err)
	}
	// 未登录文案(繁体)→ 1002
	if err := cl.mapUpstreamBusinessError(400, "請先登入會員", nil); err.(*APIError).Code != CodeUnauthorized {
		t.Fatalf("未登录文案应映射 1002: %#v", err)
	}
	// 验证码 → 1003
	if err := cl.mapUpstreamBusinessError(400, "需要验证码", nil); err.(*APIError).Code != CodeCaptchaRequired {
		t.Fatalf("验证码应映射 1003: %#v", err)
	}
	// 点赞类业务失败(HTTP 400 + code 400「評價失敗!」)→ 2001,Msg 保留上游文案
	err := cl.mapUpstreamBusinessError(400, "評價失敗!", []byte(`{"code":400}`)).(*APIError)
	if err.Code != CodeUpstream || err.Msg != "評價失敗!" {
		t.Fatalf("400 評價失敗 应为 2001 带上游文案: %#v", err)
	}
	// 其余 → 2001,upstream 带原文
	err = cl.mapUpstreamBusinessError(500, "服务维护中", []byte(`{"code":500}`)).(*APIError)
	if err.Code != CodeUpstream || err.Msg != "服务维护中" {
		t.Fatalf("业务错误应映射 2001: %#v", err)
	}
}

// ── readHTTPBody:上游 JSON 响应开头的 UTF-8 BOM 必须剥离(2026-10 上游实测,
// /setting、/login 响应均带 BOM;Python json.loads 自动剥离,Go 不会) ──

func TestReadHTTPBodyStripsBOM(t *testing.T) {
	build := func(t *testing.T, payload []byte, gzipIt bool) *http.Response {
		t.Helper()
		var r io.Reader = bytes.NewReader(payload)
		if gzipIt {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			if _, err := gz.Write(payload); err != nil {
				t.Fatalf("gzip write: %v", err)
			}
			gz.Close()
			r = &buf
		}
		return &http.Response{
			Body:   io.NopCloser(r),
			Header: http.Header{},
		}
	}

	cases := []struct {
		name string
		body []byte
		gz   bool
	}{
		{"BOM+JSON", append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"code":200,"data":[]}`)...), false},
		{"BOM+gzip JSON", append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"code":200,"data":[]}`)...), true},
		{"plain JSON", []byte(`{"code":200,"data":[]}`), false},
	}
	for _, c := range cases {
		resp := build(t, c.body, c.gz)
		if c.gz {
			resp.Header.Set("Content-Encoding", "gzip")
		}
		raw, err := readHTTPBody(resp)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var env apiEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("%s: 剥 BOM 后仍解析失败: %v (raw=%q)", c.name, err, raw)
		}
		if env.Code != 200 {
			t.Fatalf("%s: code = %d, want 200", c.name, env.Code)
		}
	}
}
