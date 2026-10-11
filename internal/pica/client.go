package pica

// PicACG 上游客户端。
//
// 关键约束(实测,API 文档 §1.3/§2.1):
//   - 上游按 TLS 指纹风控:非 okhttp 指纹会得到 HTTP 200 + {"code":200,"message":"success"}
//     的假响应(无 data 字段)。必须用 TLS 指纹模拟客户端(bogdanfinn/tls-client,Chrome
//     profile + HTTP/2),等价 picacg-qt 的 curl_cffi 方案;图片下载同理(chrome110 语义)。
//   - 签名:HMAC-SHA256(secret, lowercase(path+time+nonce+method+apiKey)),path 含
//     query 串、不含域名;authorization 头直接放 JWT(无 Bearer 前缀)。
//
// 哔咔会话是无状态 Bearer JWT,与 JM 的 cookie 会话不同:本包不维护会话表,
// 携带 token 与否由每次请求决定(登出即前端丢弃 token)。

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// parseURLLoose 宽松 URL 解析(白名单/诊断用)。
func parseURLLoose(raw string) (*url.URL, error) {
	return url.Parse(strings.TrimSpace(raw))
}

// newFhttpRequest 构建 fhttp 请求(tls-client 的 Do 接收 fhttp.Request;
// 与 net/http.Request 同构,由 reqAPI 与图片管线共用)。
func newFhttpRequest(ctx context.Context, method, endpoint string, body io.Reader) (*fhttp.Request, error) {
	return fhttp.NewRequestWithContext(ctx, strings.ToUpper(method), endpoint, body)
}

const upstreamTimeoutSecs = 30

// Client 单个上游客户端:绑定代理与 image-quality(变更由 Service 侧重建客户端)。
// 并发安全(tls-client 的 Do 与 http.Client 同语义)。
type Client struct {
	hc           tls_client.HttpClient
	proxyKey     string
	imageQuality string
}

// NewClient 构建绑定 proxy(空=直连)与画质(空=original)的上游客户端。
func NewClient(proxy, imageQuality string) (*Client, error) {
	hc, err := newTLSHTTPClient(proxy)
	if err != nil {
		return nil, err
	}
	if !imageQualityValues[imageQuality] {
		imageQuality = "original"
	}
	return &Client{hc: hc, proxyKey: proxy, imageQuality: imageQuality}, nil
}

// newTLSHTTPClient Chrome 指纹 + HTTP/2 + 代理(空=直连)的 HTTP 客户端。
// 默认跟随重定向(图片 URL 含 /static/tobeimg 时上游 301 到 img.* 域名)。
func newTLSHTTPClient(proxy string) (tls_client.HttpClient, error) {
	opts := []tls_client.HttpClientOption{
		tls_client.WithClientProfile(profiles.Chrome_133),
		tls_client.WithTimeoutSeconds(upstreamTimeoutSecs),
	}
	if proxy != "" {
		opts = append(opts, tls_client.WithProxyUrl(proxy))
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
}

// ProxyKey 当前绑定的代理(诊断用)。
func (c *Client) ProxyKey() string { return c.proxyKey }

// HTTPClient 暴露底层 tls-client(图片管线直用)。
func (c *Client) HTTPClient() tls_client.HttpClient { return c.hc }

/* ── 签名与请求头 ── */

// signature 签名原文 = path+time+nonce+method+apiKey 整体转小写后 HMAC-SHA256。
// path 含 query(如 "comics/leaderboard?tt=D7&ct=VC"),不含域名(picacg-qt
// ToolUtil.__ConFromNative 跳过首元素;api_test.py 实测口径)。
func signature(path, now, nonce, method string) string {
	src := strings.ToLower(path + now + nonce + strings.ToUpper(method) + apiKey)
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(src))
	return hex.EncodeToString(mac.Sum(nil))
}

func newNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 系统熵源失败:退化为时间戳派生(仅影响 nonce 唯一性强度,不签名冲突即可)
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// buildHeaders 组装哔咔 API 通用请求头(picacg-qt ToolUtil.GetHeader 同款集合)。
// path 为含 query 的请求路径;token 空(登录)时不带 authorization 头。
// 返回 fhttp.Header(tls-client 的 Do 接收 fhttp.Request;Header 为 map[string][]string,
// 与 net/http.Header 同构)。
func buildHeaders(path, method, token, imageQuality string) fhttp.Header {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := newNonce()
	h := fhttp.Header{}
	h.Set("api-key", apiKey)
	h.Set("accept", acceptValue)
	h.Set("app-channel", appChannel)
	h.Set("time", now)
	h.Set("app-uuid", appUUID)
	h.Set("nonce", nonce)
	h.Set("signature", signature(path, now, nonce, method))
	h.Set("app-version", appVersion)
	h.Set("image-quality", imageQuality)
	h.Set("app-platform", appPlatform)
	h.Set("app-build-version", buildVer)
	h.Set("user-agent", userAgent)
	if token != "" {
		h.Set("authorization", token) // 无 Bearer 前缀(哔咔口径)
	}
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut:
		h.Set("Content-Type", "application/json; charset=UTF-8")
	}
	return h
}

/* ── 响应信封 ── */

// apiEnvelope 上游响应外层。code 可能为字符串("1004")或数字(错误码表备注);
// 业务数据在 data;真实响应带 Server-Time 响应头(假响应检测辅助特征)。
type apiEnvelope struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Error   json.RawMessage `json:"error,omitempty"`
	Detail  string          `json:"detail,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// codeInt 归一 code(string/number → int;解析失败 → -1)。
func (e *apiEnvelope) codeInt() int {
	s := strings.Trim(string(e.Code), `"`)
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return -1
}

func (e *apiEnvelope) hasData() bool {
	return len(e.Data) > 0 && string(e.Data) != "null"
}

/* ── 请求核心 ── */

// reqAPI 调用上游 API:JSON 编解码 + 签名头 + 假响应检测 + 错误分类。
// path 为含 query 的请求路径;body 非 nil 时 JSON 序列化为请求体(POST/PUT)。
// 返回 data 字段解码后的 map(data 缺失/为 null 时返回空 map)。
func (c *Client) reqAPI(ctx context.Context, method, path, token string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, errUpstream("请求体序列化失败", err.Error())
		}
		reader = bytes.NewReader(raw)
	}
	req, err := newFhttpRequest(ctx, method, apiBaseURL+path, reader)
	if err != nil {
		return nil, classifyUpstreamError(err, "请求构建失败")
	}
	for k, vs := range buildHeaders(path, method, token, c.imageQuality) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, classifyUpstreamError(err, "哔咔服务端请求失败")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, classifyUpstreamError(err, "响应读取失败")
	}

	var env apiEnvelope
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
		// 上游以 HTTP 状态表达失败(如 401 裸文本)或返回非 JSON(风控页)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, errUnauthorized()
		}
		if resp.StatusCode != http.StatusOK {
			return nil, classifyUpstreamError(
				fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200)), "哔咔服务端返回错误")
		}
		return nil, errUpstream("响应解析失败", truncate(string(raw), 300))
	}
	code := env.codeInt()
	if code != 200 {
		// HTTP 401 + 业务 code(如 1002 token 失效)均按业务映射
		return nil, mapUpstreamCode(code, env.Message)
	}
	// 假响应检测:真实成功响应带 Server-Time 头且业务数据在 data 字段。
	// code=200 但既无 data 又无 Server-Time → 上游对指纹不符客户端投喂的假响应,
	// 绝不把空结果当真数据透出(实测错误密码也如此,极易误判)。
	if !env.hasData() && resp.Header.Get("Server-Time") == "" {
		return nil, errSuspiciousFakeResponse()
	}
	out := map[string]any{}
	if env.hasData() {
		if jsonErr := json.Unmarshal(env.Data, &out); jsonErr != nil {
			return nil, errUpstream("响应数据解析失败", jsonErr.Error())
		}
	}
	return out, nil
}

// reqAPINoSign 图片等无需签名头的 GET(占位:哔咔仅图片下载,走 image.go 管线)。

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

/* ── 客户端池(代理/画质键控) ── */

// ClientPool 按键(proxy|quality)复用客户端,代理或画质变更后重建。
type ClientPool struct {
	mu    sync.Mutex
	cur   *Client
	key   string
	err   error
	build func() (*Client, error)
}

// NewClientPool 构建池;build 闭包负责按当前设置产出客户端。
func NewClientPool(build func() (*Client, error)) *ClientPool {
	return &ClientPool{build: build}
}

// Get 取当前键客户端;键变更后惰性重建。构建失败(如代理地址非法)缓存错误直至键变更。
func (p *ClientPool) Get(key string) (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil && p.key == key {
		return p.cur, nil
	}
	if p.cur == nil && p.err != nil && p.key == key {
		return nil, p.err
	}
	cl, err := p.build()
	if err != nil {
		p.cur, p.err, p.key = nil, err, key
		return nil, err
	}
	p.cur, p.err, p.key = cl, nil, key
	return cl, nil
}
