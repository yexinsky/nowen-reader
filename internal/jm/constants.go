// Package jm 是 JM 移动端 API 的 Go 原生客户端与服务端实现。
//
// 协议依据:jmcomic 2.6.17 SDK(jm_config.py / jm_toolkit.py / jm_client_impl.py)
// 与 JMComic-qt mobile/server/core/live.py(上游实测对照)。
// 对外 REST 契约见 docs/MOBILE_API.md(30 端点,{code,msg,data} 包装)。
//
// 资源约束:启动零外呼;所有上游连接在首次请求时惰性建立;无内部轮询。
package jm

// 上游协议常量(jmcomic/jm_config.py:100-105,一字不差)
const (
	appTokenSecret    = "18comicAPP"        // 常规请求 token 密钥
	appTokenSecret2   = "18comicAPPContent" // /chapter_view_template 专用
	appDataSecret     = "185Hcomic3PAPP7R"  // 响应 AES 解密密钥前缀
	apiDomainServerSecret = "diosfjckwpqpdfjkvnqQjsik"
	defaultAppVersion = "2.0.19"
)

// 乱序分割数阈值(jm_config.py:96-97;jm_toolkit.py:905-926)
const (
	scrambleDefaultID       = 220980 // 上游未下发 scramble_id 时的默认阈值
	scrambleThreshold268850 = 268850
	scrambleThreshold421926 = 421926
)

// 默认域名池(jm_config.py:145-168 的内置池;SDK 内置池会经动态更新服务替换,
// 此处静态值已更新为实测可用域名作为更新失败时的兜底,顺序即优先级、失败轮换)。
// 可用环境变量 JM_API_DOMAINS / JM_IMAGE_DOMAINS 覆盖(逗号分隔)。
var (
	defaultAPIDomains = []string{
		"www.cdnhjk.net",
		"www.cdngwc.cc",
		"www.cdngwc.net",
		"www.cdngwc.club",
	}
	defaultImageDomains = []string{
		"cdn-msp.jmapiproxy1.cc",
		"cdn-msp.jmapiproxy2.cc",
		"cdn-msp2.jmapiproxy2.cc",
		"cdn-msp3.jmapiproxy2.cc",
		"cdn-msp.jmapinodeudzn.net",
		"cdn-msp3.jmapinodeudzn.net",
	}
	// 域名动态更新服务(jm_config.py:165-168),内容经 apiDomainServerSecret 解码
	domainUpdateServers = []string{
		"https://rup4a04-c01.tos-ap-southeast-1.bytepluses.com/newsvr-2025.txt",
		"https://rup4a04-c02.tos-cn-hongkong.bytepluses.com/newsvr-2025.txt",
	}
)

// 请求头模板(jm_config.py:170-181)
const (
	appUserAgent = "Mozilla/5.0 (Linux; Android 9; V1938CT Build/PQ3A.190705.11211812; wv) AppleWebKit/537.36 (KHTML, " +
		"like Gecko) Version/4.0 Chrome/91.0.4472.114 Safari/537.36"
	imageAccept = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"
	// 图片请求的 X-Requested-With
	appXRequestedWith = "com.JMComic3.app"
)

// 上游请求超时与重试(对齐 live.py _build_option:timeout 25 / retry_times 1)
const (
	upstreamTimeoutSecs = 25
	upstreamRetryTimes  = 1 // 失败时换下一个域名再试一次
)

// 上游错误分类的网络关键字(live.py _map_live_error,一字不差)
var networkMarkers = []string{
	"RequestRetryAllFail", "timed out", "timeout", "Timeout",
	"Connection", "connect", "Failed to", "Proxy", "SSL", "DNS",
	"ConnectionError", "ReadTimeout", "NetworkError",
	"refused", "reset", "unreachable", "getaddrinfo", "curl",
	"RequestException",
}
