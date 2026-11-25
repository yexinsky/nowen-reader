package jm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 域名池支持环境变量覆盖(逗号分隔):JM_API_DOMAINS / JM_IMAGE_DOMAINS。
func splitDomains(env string) []string {
	if env == "" {
		return nil
	}
	var out []string
	for _, d := range strings.Split(env, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

func apiDomainsOverride() []string {
	return splitDomains(os.Getenv("JM_API_DOMAINS"))
}

func imageDomains() []string {
	if override := splitDomains(os.Getenv("JM_IMAGE_DOMAINS")); len(override) != 0 {
		return override
	}
	// live.py 图片域名取池内前 3 个轮换
	return defaultImageDomains[:3]
}

// API 域名动态更新(SDK after_init → fetch_latest_api_domain_for_module)。
//
// 这里刻意不用 sync.Once:上游换域没有预告,死域名返回的是 nginx 404(不是网络错误),
// 而进程可能一跑就是几周。曾经用 Once 时,启动那一刻拉取失败(或域名在进程生命周期内
// 变更)就永久锁死在静态池上,表现为整站 404「JM 服务端返回错误」——
// 2026-11-23 20:55 启动,11-24 上游换域,服务一直错到 11-25 手动重启才恢复。
//
// 现在的语义:
//   - 首个请求触发一次刷新,之后超过 apiDomainRefreshTTL 再刷新;
//   - 失败只缩短下次重试间隔(apiDomainRetryAfter),不再永久钉死;
//   - 持锁期间不发起网络请求(并发请求退化为"用当前池",不会排队等代理超时)。
var (
	domainMu      sync.Mutex
	domainUpdated []string     // 最近一次动态更新拿到的池(空=从未成功)
	domainTriedAt time.Time    // 上次尝试刷新的时刻
	domainRetryAt time.Time    // 下次允许刷新的时刻(失败时提前,成功时按 TTL)
	domainErr     atomic.Value // string:最近一次刷新失败原因(成功时清空)
)

const (
	// apiDomainRefreshTTL 动态池的有效期。上游换域后最迟这么久生效。
	apiDomainRefreshTTL = 6 * time.Hour
	// apiDomainRetryAfter 刷新失败后的重试间隔,也是强制刷新(遇到死域名)的最小间隔。
	// 别把代理或更新服务打爆。
	apiDomainRetryAfter = 5 * time.Minute
	// apiDomainFetchTimeout 单次刷新(两个更新服务合计)的上限。
	// 代理挂掉时不能让首个请求卡 25s×2。
	apiDomainFetchTimeout = 8 * time.Second
)

// refreshAPIDomains 按需刷新动态池(TTL 门控)。
func refreshAPIDomains(ctx context.Context, httpClient *http.Client) {
	refreshAPIDomainsWithin(ctx, httpClient, false)
}

// refreshAPIDomainsForce 忽略 TTL 立即刷新,但仍受 apiDomainRetryAfter 的最小间隔保护:
// 整池都死时每个请求都会走到这里,不能每次都拉更新服务。
func refreshAPIDomainsForce(ctx context.Context, httpClient *http.Client) {
	refreshAPIDomainsWithin(ctx, httpClient, true)
}

// refreshAPIDomainsWithin 刷新主逻辑。force=true 时绕过 domainRetryAt(TTL/退避),
// 改为要求距上次尝试至少 apiDomainRetryAfter。
// 加锁只用于读写状态,网络请求在锁外进行:并发请求在这段时间里直接用当前池,
// 不会排队等代理超时。
func refreshAPIDomainsWithin(ctx context.Context, httpClient *http.Client, force bool) {
	now := time.Now()
	domainMu.Lock()
	if force {
		if !domainTriedAt.IsZero() && now.Sub(domainTriedAt) < apiDomainRetryAfter {
			domainMu.Unlock()
			return
		}
	} else if !domainRetryAt.IsZero() && now.Before(domainRetryAt) {
		domainMu.Unlock()
		return
	}
	// 先占位再放锁:并发请求这段时间直接用当前池。真实结果在下面按成败回写。
	domainTriedAt = now
	domainRetryAt = now.Add(apiDomainRefreshTTL)
	domainMu.Unlock()

	// 刷新是进程级共享状态,不跟随单个请求的取消:用户划走页面不该让池子卡在旧值。
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiDomainFetchTimeout)
	defer cancel()
	fetched, err := fetchAPIDomains(fetchCtx, httpClient)

	domainMu.Lock()
	domainTriedAt = time.Now()
	if err != nil || len(fetched) == 0 {
		domainRetryAt = time.Now().Add(apiDomainRetryAfter)
		if err != nil {
			domainErr.Store(err.Error())
		} else {
			domainErr.Store("域名更新服务未返回可用域名")
		}
		domainMu.Unlock()
		return
	}
	domainUpdated = fetched
	domainRetryAt = time.Now().Add(apiDomainRefreshTTL)
	domainErr.Store("")
	domainMu.Unlock()
}

// refreshAPIDomainsIfDeadDomain 遇到"域名级失效"信号时的补刷新(TTL 内也允许)。
func refreshAPIDomainsIfDeadDomain(ctx context.Context, httpClient *http.Client) {
	refreshAPIDomainsForce(ctx, httpClient)
}

// fetchAPIDomains 依次尝试更新服务(SDK req_api_domain_server 语义):
// GET 文本 → 去开头非 ASCII 字符 → AES 解密(key = md5(仅 apiDomainServerSecret),ts 前缀为空)
// → JSON {"Server": [...]}。
func fetchAPIDomains(ctx context.Context, httpClient *http.Client) ([]string, error) {
	var lastErr error
	for _, server := range domainUpdateServers {
		req, err := http.NewRequestWithContext(ctx, "GET", server, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", appUserAgent)
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		text, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		// 去掉开头非 ASCII 字符(SDK req_api_domain_server)
		body := string(text)
		for len(body) > 0 && body[0] > 127 {
			body = body[1:]
		}
		decoded, err := decodeRespData(strings.TrimSpace(body), "", apiDomainServerSecret)
		if err != nil {
			lastErr = err
			continue
		}
		var parsed struct {
			Server []string `json:"Server"`
		}
		if err := json.Unmarshal(decoded, &parsed); err != nil {
			lastErr = err
			continue
		}
		if len(parsed.Server) == 0 {
			lastErr = errUpstream("域名更新服务返回空池", "")
			continue
		}
		return parsed.Server, nil
	}
	if lastErr == nil {
		lastErr = errNetwork("域名更新服务不可达")
	}
	return nil, lastErr
}

// apiDomainsWithUpdated 返回生效的 API 域名池(优先级:env 覆盖 > 动态更新 > 静态默认)。
func apiDomainsWithUpdated() []string {
	if override := apiDomainsOverride(); len(override) > 0 {
		return override
	}
	domainMu.Lock()
	updated := append([]string(nil), domainUpdated...)
	domainMu.Unlock()
	if len(updated) > 0 {
		return updated
	}
	return defaultAPIDomains
}

// APIDomainStatus 域名池当前状态,供 /api/jm/health 观测
// (曾经更新失败被静默吞掉,排查时只能猜)。
type APIDomainStatus struct {
	// Active 当前生效的域名池(第一名即首选)。
	Active []string `json:"active"`
	// Source 生效来源:env / dynamic / static。
	Source string `json:"source"`
	// RefreshedAt 上次从更新服务拉取(无论成败)的时刻,零值表示还没试过。
	RefreshedAt time.Time `json:"refreshedAt"`
	// Stale 动态池已过期但刷新失败,当前仍在使用旧池或静态兜底。
	Stale bool `json:"stale"`
	// Error 最近一次刷新失败原因(成功时为空)。
	Error string `json:"error,omitempty"`
}

// APIDomainsStatus 返回域名池状态(零上游调用)。
func APIDomainsStatus() APIDomainStatus {
	source := "static"
	if len(apiDomainsOverride()) > 0 {
		source = "env"
	}
	domainMu.Lock()
	updated := append([]string(nil), domainUpdated...)
	triedAt := domainTriedAt
	retryAt := domainRetryAt
	domainMu.Unlock()
	if source != "env" && len(updated) > 0 {
		source = "dynamic"
	}
	msg, _ := domainErr.Load().(string)
	st := APIDomainStatus{
		Active:      apiDomainsWithUpdated(),
		Source:      source,
		RefreshedAt: triedAt,
		Error:       msg,
	}
	// 下次刷新点已过期 = 池已到保鲜期却没能续上,对外标 stale 便于告警。
	st.Stale = !retryAt.IsZero() && time.Now().After(retryAt)
	return st
}
