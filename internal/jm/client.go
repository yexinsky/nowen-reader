package jm

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const apiProt = "https://"

// Client 是单个上游 API 客户端:绑定代理与 cookies(匿名客户端 cookies 为空)。
// 请求头带 token/tokenparam;响应做 AES 解密;域名失败轮换(重试 upstreamRetryTimes 次)。
type Client struct {
	http     *http.Client
	cookies  map[string]string
	initOnce sync.Once

	// SDK FLAG_USE_FIX_TIMESTAMP 语义:进程级固定 ts(首个请求时刻),
	// 常规请求的 token 头与响应解密共用它;/chapter_view_template 用新鲜 ts。
	ts atomic.Int64

	mu         sync.Mutex
	appVersion string
}

var (
	globalTS    atomic.Int64
	sharedProxy atomic.Value // string:当前上游代理(空=直连);变更时各 Backend 重建客户端
)

func newHTTPClient(proxy string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil && pu.Scheme != "" {
			transport.Proxy = http.ProxyURL(pu)
		}
	} else {
		transport.Proxy = nil
	}
	return &http.Client{
		Transport: transport,
		Timeout:   upstreamTimeoutSecs * time.Second,
		// 跟随重定向(curl_cffi 默认行为;上游域名 301 到新域名时继续请求)
	}
}

// NewClient 构建绑定 proxy(空=直连)与 cookies(可空)的上游客户端。
// 传入 map 一律拷贝:调用方(会话表)持有的同一 map 会被 save() 遍历,
// 而客户端 bootstrap 会经 setCookie 写入,共享引用会构成并发读写。
func NewClient(proxy string, cookies map[string]string) *Client {
	owned := make(map[string]string, len(cookies))
	for k, v := range cookies {
		owned[k] = v
	}
	c := &Client{
		http:       newHTTPClient(proxy),
		cookies:    owned,
		appVersion: defaultAppVersion,
	}
	// 进程级固定 ts(SDK FLAG_USE_FIX_TIMESTAMP):首个客户端创建时确定,全程复用
	globalTS.CompareAndSwap(0, timeStamp())
	c.ts.Store(globalTS.Load())
	return c
}

// apiDomains 生效的 API 域名池(env 覆盖 > 动态更新 > 静态默认,见 domains.go)。
func (c *Client) apiDomains() []string {
	return apiDomainsWithUpdated()
}

func (c *Client) currentAppVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appVersion
}

// ensureInit 客户端首次请求前的引导(SDK after_init 语义),每客户端仅一次:
// ① 域名动态更新(进程级一次,失败标记 done 防重复执行);
// ② cookies 引导:cookies 为空时调 /setting 抓取 Set-Cookie(移动端必须携带,
//    否则上游跳转"禁漫娘"页);失败静默,由后续请求按上游错误暴露。
func (c *Client) ensureInit(ctx context.Context) {
	c.initOnce.Do(func() {
		updateAPIDomains(ctx, c.http)
		if len(c.cookies) == 0 {
			c.bootstrapCookies(ctx)
		}
	})
}

// bootstrapCookies 调 /setting 抓 Set-Cookie(SDK get_cookies 语义)。
func (c *Client) bootstrapCookies(ctx context.Context) {
	ts := c.ts.Load()
	token, tokenparam := tokenAndTokenparam(ts, c.currentAppVersion(), appTokenSecret)
	req, err := http.NewRequestWithContext(ctx, "GET", apiProt+c.apiDomains()[0]+"/setting", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", appUserAgent)
	req.Header.Set("token", token)
	req.Header.Set("tokenparam", tokenparam)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	for _, ck := range resp.Cookies() {
		c.setCookie(ck.Name, ck.Value)
	}
}

// setAppVersion 由 /setting 的 jm3_version 更新(SDK 动态版本语义)。
func (c *Client) setAppVersion(v string) {
	if v == "" {
		return
	}
	c.mu.Lock()
	c.appVersion = v
	c.mu.Unlock()
}

// setCookie 合并设置 cookies。
func (c *Client) setCookie(name, value string) {
	if name == "" || value == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cookies == nil {
		c.cookies = make(map[string]string)
	}
	c.cookies[name] = value
}

// cookiesSnapshot 当前 cookies 的副本(登录合并用,避免锁内做 map 字面量)。
func (c *Client) cookiesSnapshot() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

// mergeLoginCookies 合并登录后的会话 cookies,顺序即优先级:
// ① 登录客户端累计 jar(/setting 引导:__cflb/ipcountry/ipm5/theme 等)——
//    参考实现(SDK curl_cffi Session + live.py 复用登录客户端)的会员请求
//    携带这份完整 jar;若只回传登录响应 cookies,会话客户端将裸带 AVS 出网,
//    上游按未登录拒绝(HTTP 401「請先登入會員」);
// ② 登录响应 Set-Cookie(服务端会话/刷新的负载均衡 cookie);
// ③ AVS = 登录返回值 s 字段(SDK login 同款,最后写入保证覆盖)。
func mergeLoginCookies(jar map[string]string, respCookies []*http.Cookie, avs string) map[string]string {
	cookies := make(map[string]string, len(jar)+2)
	for k, v := range jar {
		cookies[k] = v
	}
	for _, ck := range respCookies {
		if ck == nil || ck.Name == "" || ck.Value == "" {
			continue
		}
		cookies[ck.Name] = ck.Value
	}
	if avs != "" {
		cookies["AVS"] = avs
	}
	return cookies
}

// reqAPI 调用上游移动端 API:GET/POST 表单,响应 AES 解密为 JSON。
// 返回 decoded 的原始 JSON 字节(通常为 {"code":200,...} 或业务对象)。
// 业务失败(code!=200)返回 *APIError(2001/1001/1003);网络失败轮换域名重试后返回 2002。
func (c *Client) reqAPI(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	// SDK after_init 语义:首次请求前完成域名动态更新 + cookies 引导(均惰性、进程/客户端级一次)
	c.ensureInit(ctx)
	domains := c.apiDomains()
	var lastErr error
	for attempt := 0; attempt <= upstreamRetryTimes; attempt++ {
		domain := domains[attempt%len(domains)]
		endpoint := apiProt + domain + path
		data, err := c.doAPIRequest(ctx, method, endpoint, form, "")
		if err == nil {
			return data, nil
		}
		if apiErr, ok := err.(*APIError); ok {
			// 业务错误不重试(上游明确拒绝)
			return nil, apiErr
		}
		lastErr = err
	}
	return nil, classifyUpstreamError(lastErr, "")
}

// apiEnvelope 上游响应外层结构。业务错误信息可能在 msg/errorMsg/message 任一字段
// (401 类用 errorMsg,如 {"code":401,"errorMsg":"請先登入會員"};live.py 同序读取)。
type apiEnvelope struct {
	Code     int             `json:"code"`
	Msg      json.RawMessage `json:"msg,omitempty"`
	ErrorMsg json.RawMessage `json:"errorMsg,omitempty"`
	Message  json.RawMessage `json:"message,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// message 取第一个非空错误信息字段(msg → errorMsg → message)。
func (e *apiEnvelope) message() string {
	for _, raw := range []json.RawMessage{e.Msg, e.ErrorMsg, e.Message} {
		if len(raw) == 0 {
			continue
		}
		if s := strings.Trim(strings.TrimSpace(string(raw)), `"`); s != "" && s != "null" {
			return s
		}
	}
	return ""
}

// doAPIRequest 执行单次请求并解密。specialSecret 非 nil 时用新鲜 ts + 指定密钥
// (/chapter_view_template 语义)。
func (c *Client) doAPIRequest(ctx context.Context, method, endpoint string, form url.Values, specialSecret string) ([]byte, error) {
	ts := c.ts.Load()
	secret := appTokenSecret
	if specialSecret != "" {
		ts = timeStamp()
		secret = specialSecret
	}
	token, tokenparam := tokenAndTokenparam(ts, c.currentAppVersion(), secret)

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", appUserAgent)
	req.Header.Set("token", token)
	req.Header.Set("tokenparam", tokenparam)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := readHTTPBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// 上游业务失败常以非 200 HTTP + JSON 信封表达(live.py 口径:只看 body 的
		// code/errorMsg,不看 HTTP 状态),如实测:未登录 /favorite → 401「請先登入會員」、
		// /like 失败 → 400「評價失敗!」。先按信封解析出业务错误;body 非 JSON 信封
		// (风控页等)才按裸 HTTP 错误处理。
		var envBody apiEnvelope
		if json.Unmarshal(raw, &envBody) == nil && envBody.Code != 0 && envBody.Code != 200 {
			return nil, c.mapUpstreamBusinessError(envBody.Code, envBody.message(), raw)
		}
		// 上游以 HTTP 401 表达登录态无效(AVS 过期/缺失),契约映射 1002,
		// 让前端提示重新登录而非笼统的「JM 服务端返回错误」。
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, errUnauthorized()
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	// 响应包装:{"code":200,"data":"<base64(AES)>"};部分端点(如 /setting)响应同为加密包装
	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// 非 JSON 响应(可能被上游风控拦截):按上游错误处理
		return nil, fmt.Errorf("响应非 JSON: %s", truncate(string(raw), 200))
	}
	if envelope.Code != 200 {
		return nil, c.mapUpstreamBusinessError(envelope.Code, envelope.message(), raw)
	}
	// data 为加密字符串;个别端点(未知)可能直接给 JSON 对象,做兼容
	if len(envelope.Data) > 0 && envelope.Data[0] == '{' {
		return envelope.Data, nil
	}
	var dataB64 string
	if err := json.Unmarshal(envelope.Data, &dataB64); err != nil {
		return nil, fmt.Errorf("data 字段非字符串: %w", err)
	}
	decoded, err := decodeRespData(dataB64, strconv.FormatInt(ts, 10), "")
	if err != nil {
		return nil, fmt.Errorf("响应解密失败: %w", err)
	}
	return decoded, nil
}

// mapUpstreamBusinessError 把上游 code!=200 映射为业务错误(live.py 登录分支同款):
// code=401 或 msg 表明未登录 → 1002;msg 含"验证码/captcha" → 1003;其余 → 2001(带 upstream 摘要)。
func (c *Client) mapUpstreamBusinessError(code int, msg string, raw []byte) error {
	// 上游 code=401(无论 HTTP 状态):登录态无效(AVS 过期/缺失)→ 1002
	if code == http.StatusUnauthorized {
		return errUnauthorized()
	}
	lower := strings.ToLower(msg)
	if strings.Contains(msg, "驗證碼") || strings.Contains(msg, "验证码") || strings.Contains(lower, "captcha") {
		return errCaptchaRequired()
	}
	// 上游会员态校验失败的典型文案(繁/简):「請先登入會員」等 → 1002
	if hasLoginSemantics(msg) {
		return errUnauthorized()
	}
	detail := fmt.Sprintf("上游 code=%d msg=%s body=%s", code, msg, truncate(string(raw), 150))
	_ = detail
	return errUpstream(msg, truncate(string(raw), 300))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// readHTTPBody 读取响应体并按 Content-Encoding 显式解压。
// 请求头手动声明了 Accept-Encoding: gzip(SDK APP_HEADERS_TEMPLATE),
// 此时 net/http 不自动解压,必须显式处理(/login、通用 API 请求共用)。
func readHTTPBody(resp *http.Response) ([]byte, error) {
	var body io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		body = gz
	}
	return io.ReadAll(body)
}
