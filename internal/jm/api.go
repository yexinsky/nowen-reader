package jm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// 上游 API 封装:每个方法对应一个上游调用,返回 decoded JSON 原始字节或 *APIError。
// 端点参数与映射依据:jm_client_impl.py:597-1100 + mobile/server/core/live.py(MOBILE_API.md 契约)。

// webHeaders 验证码/网页请求头(live.py _web_headers:对齐桌面端 GetCaptchaReq)。
func webHeaders(domain string) map[string]string {
	return map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"Accept":          "image/avif,image/webp,image/apng,image/*,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Referer":         apiProt + domain + "/signup",
	}
}

const jmRedirectURL = "https://jm365.work/3YeBdF" // 永久网域(jm_config.py:112),重定向解析网页端域名

var (
	htmlDomainMu      sync.Mutex
	htmlDomainCache   string
	htmlDomainCacheOK bool
)

// htmlDomainCached 解析网页端域名(验证码回退用):请求 JM_REDIRECT_URL 捕获重定向 Location。
// 进程内缓存,失败返回 ""。
func (c *Client) htmlDomainCached(ctx context.Context) string {
	htmlDomainMu.Lock()
	defer htmlDomainMu.Unlock()
	if htmlDomainCacheOK {
		return htmlDomainCache
	}
	req, err := http.NewRequestWithContext(ctx, "GET", jmRedirectURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", webUserAgent)
	// 跟随完整重定向链(jm365.work → … → 最终网页域名;可能多跳),
	// 终点页即使 403(风控)也不影响:resp.Request.URL 即最终落点
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if host := resp.Request.URL.Hostname(); host != "" {
		htmlDomainCache = host
		htmlDomainCacheOK = true
	}
	return htmlDomainCache
}

func hostOf(raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return ""
}

const webUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Setting 调上游 /setting:返回 decoded 原始 JSON,并更新动态 APP_VERSION
// (jm3_version;SDK 版本动态语义)。
func (c *Client) Setting(ctx context.Context) ([]byte, error) {
	data, err := c.reqAPI(ctx, "GET", "/setting", nil)
	if err == nil {
		var m map[string]any
		if json.Unmarshal(data, &m) == nil {
			if v, ok := m["jm3_version"].(string); ok {
				c.setAppVersion(v)
			}
		}
	}
	return data, err
}

// CaptchaBytes 代理上游验证码图片(live.py captcha_bytes):
// 优先 API 域名池第 1 个,失败回退网页端域名;返回 (bytes, contentType)。
func (c *Client) CaptchaBytes(ctx context.Context) ([]byte, string, error) {
	candidates := []string{}
	if domains := c.apiDomains(); len(domains) > 0 {
		candidates = append(candidates, domains[0])
	}
	if html := c.htmlDomainCached(ctx); html != "" {
		candidates = append(candidates, html)
	}
	if len(candidates) == 0 {
		return nil, "", errNetwork("无可用上游域名(验证码)")
	}
	var lastErr error
	for _, domain := range candidates {
		endpoint := apiProt + domain + "/captcha"
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range webHeaders(domain) {
			req.Header.Set(k, v)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		isImage := len(data) >= 3 && (data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF ||
			data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E) || strings.HasPrefix(ct, "image/")
		if resp.StatusCode == http.StatusOK && isImage && len(data) > 0 {
			media := "image/jpeg"
			if strings.HasPrefix(ct, "image/") {
				media = strings.Split(ct, ";")[0]
			}
			return data, media, nil
		}
		lastErr = fmt.Errorf("上游验证码响应异常: %d %s", resp.StatusCode, ct)
	}
	return nil, "", errUpstream("获取验证码失败", truncate(fmt.Sprint(lastErr), 300))
}

// outerEnvelope 上游响应外层结构。业务错误信息在 errorMsg/message 字段(live.py:898)。
type outerEnvelope struct {
	Code     int             `json:"code"`
	ErrorMsg json.RawMessage `json:"errorMsg,omitempty"`
	Message  json.RawMessage `json:"message,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	s := ""
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// mapUpstreamBusinessError:live.py login 分支同款——验证码 → 1003;其余 → 1001(凭据)语义
// 仅适用于登录;其他端点统一 2001。由 Login 特化处理,通用路径走 2001。
func (c *Client) loginBusinessError(envelope outerEnvelope) error {
	msg := rawString(envelope.ErrorMsg) + rawString(envelope.Message)
	if strings.Contains(msg, "验证码") || strings.Contains(strings.ToLower(msg), "captcha") {
		return errCaptchaRequired()
	}
	return errBadCredentials()
}

// Login 移动端登录(live.py login 直译):
// 成功返回 (userInfo, cookies 含 AVS);失败按 1001/1003 分类。
func (c *Client) Login(ctx context.Context, username, password, captcha string) (map[string]any, map[string]string, error) {
	form := url.Values{
		"username":     {username},
		"password":     {password},
		"submit_login": {""},
	}
	if captcha != "" {
		form.Set("captcha", captcha)
	}
	// 单请求封装:构建 → 解密;业务 code!=200 走 loginBusinessError
	c.ensureInit(ctx)
	endpoint := apiProt + c.apiDomains()[0] + "/login"
	ts := c.ts.Load()
	token, tokenparam := tokenAndTokenparam(ts, c.currentAppVersion(), appTokenSecret)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, classifyUpstreamError(err, "登录请求失败")
	}
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", appUserAgent)
	req.Header.Set("token", token)
	req.Header.Set("tokenparam", tokenparam)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, classifyUpstreamError(err, "登录请求失败")
	}
	defer resp.Body.Close()
	raw, err := readHTTPBody(resp)
	if err != nil {
		return nil, nil, classifyUpstreamError(err, "登录请求失败")
	}
	// 上游可能以 HTTP 401 + {"code":401,"errorMsg":"无效的用户名和/或密码!"} 表达业务失败,
	// 因此先按 JSON 解析再分类(live.py 同款:只看 body 的 code/errorMsg,不看 HTTP 状态)。
	var envelope outerEnvelope
	if jsonErr := json.Unmarshal(raw, &envelope); jsonErr != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, nil, classifyUpstreamError(
				fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200)), "登录请求失败")
		}
		return nil, nil, errUpstream("登录响应解析失败", truncate(string(raw), 300))
	}
	if envelope.Code != 200 {
		return nil, nil, c.loginBusinessError(envelope)
	}
	var dataB64 string
	if err := json.Unmarshal(envelope.Data, &dataB64); err != nil {
		return nil, nil, errUpstream("登录数据解密失败", truncate(string(raw), 300))
	}
	decoded, err := decodeRespData(dataB64, strconv.FormatInt(ts, 10), "")
	if err != nil {
		return nil, nil, errUpstream("登录数据解密失败", err.Error())
	}
	var userInfo map[string]any
	if err := json.Unmarshal(decoded, &userInfo); err != nil {
		return nil, nil, errUpstream("登录数据解析失败", err.Error())
	}
	// 会话 cookies:登录客户端累计 jar(/setting 引导)+ 登录响应 Set-Cookie
	// + AVS(= s 字段)。参考实现的会员请求携带完整 cookie jar,只存响应 cookies
	// 会让会话客户端裸带 AVS 出网,上游按未登录拒绝(401「請先登入會員」)。
	avs, _ := userInfo["s"].(string)
	cookies := mergeLoginCookies(c.cookiesSnapshot(), resp.Cookies(), avs)
	return userInfo, cookies, nil
}
