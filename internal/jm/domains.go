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

// API 域名动态更新(SDK after_init → fetch_latest_api_domain_for_module):
// 进程级一次;失败置 done 空切片防重复执行,静态池(已更新为验证可用域名)兜底。
var (
	domainUpdateOnce   sync.Once
	domainUpdated      atomicValueDomains
	domainUpdateFailed atomic.Bool
)

type atomicValueDomains struct {
	mu  sync.RWMutex
	val []string
}

func (a *atomicValueDomains) load() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.val
}

func (a *atomicValueDomains) store(v []string) {
	a.mu.Lock()
	a.val = v
	a.mu.Unlock()
}

// updateAPIDomains 尝试从更新服务拉取最新 API 域名(SDK req_api_domain_server 语义):
// GET 文本 → 去开头非 ASCII 字符 → AES 解密(key = md5(仅 apiDomainServerSecret),ts 前缀为空)
// → JSON {"Server": [...]}。成功替换进程级域名池;任何失败标记 done(不重试)。
func updateAPIDomains(ctx context.Context, httpClient *http.Client) {
	domainUpdateOnce.Do(func() {
		for _, server := range domainUpdateServers {
			req, err := http.NewRequestWithContext(ctx, "GET", server, nil)
			if err != nil {
				continue
			}
			req.Header.Set("User-Agent", appUserAgent)
			resp, err := httpClient.Do(req)
			if err != nil {
				continue
			}
			text, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				continue
			}
			// 去掉开头非 ASCII 字符(SDK req_api_domain_server)
			body := string(text)
			for len(body) > 0 && body[0] > 127 {
				body = body[1:]
			}
			decoded, err := decodeRespData(strings.TrimSpace(body), "", apiDomainServerSecret)
			if err != nil {
				continue
			}
			var parsed struct {
				Server []string `json:"Server"`
			}
			if json.Unmarshal(decoded, &parsed) != nil || len(parsed.Server) == 0 {
				continue
			}
			domainUpdated.store(parsed.Server)
			return
		}
		domainUpdateFailed.Store(true)
	})
}

// apiDomainsWithUpdated 返回生效的 API 域名池(优先级:env 覆盖 > 动态更新 > 静态默认)。
func apiDomainsWithUpdated() []string {
	if override := apiDomainsOverride(); len(override) > 0 {
		return override
	}
	if updated := domainUpdated.load(); len(updated) > 0 {
		return updated
	}
	return defaultAPIDomains
}
