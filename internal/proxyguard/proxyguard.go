// Package proxyguard 落实「在线源必须经代理访问」这条规则。
//
// 适用模块:JM(在线漫画)与哔咔(PicACG)。这两个模块的上游都不允许直连:
//   - 直连会把服务器公网 IP 直接暴露给源站风控,命中即被封;
//   - 一旦「部分请求走代理、部分漏成直连」,故障表现为接口时好时坏,
//     排查成本远高于干脆不让它连。
//
// 本包只负责判定与文案,拦截点在各模块自己的 HTTP 客户端里:
//   - JM(net/http):transport.Proxy 恒返回错误 —— 没有代理时根本连不出网;
//   - 哔咔(tls-client):没有可用代理就不构建客户端。
//
// 注意:空代理不再是「直连」的同义词。设置里留空会回退到环境变量与默认值,
// 仍然为空则视为未配置,相关请求直接失败并给出可操作的提示。
package proxyguard

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrMissing 未配置代理。作为 http.Transport.Proxy 的返回值使用,
// 会被 net/http 包进 *url.Error,调用方用 errors.Is 判定。
var ErrMissing = errors.New("上游代理未配置:在线漫画与哔咔必须经代理访问,不支持直连,请在设置页填写代理地址")

// ErrInvalid 代理地址不可用(缺 scheme / 缺 host / 无法解析)。
var ErrInvalid = errors.New("上游代理地址不可用:需要形如 http://127.0.0.1:10809 或 socks5://127.0.0.1:1080 的地址")

// DefaultAddress 两个在线源共用的兜底代理地址(沿用 JM 历史默认值)。
// 它只在「落盘值与环境变量都为空」时生效;真要改就改设置页或环境变量。
const DefaultAddress = "http://127.0.0.1:10809"

// IsBlocked 判断错误是否来自「代理缺失/不可用」的强制拦截。
func IsBlocked(err error) bool {
	return errors.Is(err, ErrMissing) || errors.Is(err, ErrInvalid)
}

// Message 返回给终端用户的提示文案(IsBlocked 为真时有意义)。
func Message(err error) string {
	switch {
	case errors.Is(err, ErrInvalid):
		return ErrInvalid.Error()
	case errors.Is(err, ErrMissing):
		return ErrMissing.Error()
	default:
		return ""
	}
}

// proxySchemes 允许的代理协议。socks5h 的 DNS 也在代理侧解析,避免本地解析泄漏。
var proxySchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

// Validate 校验代理地址是否可用;空串返回 ErrMissing,格式不对返回 ErrInvalid。
func Validate(proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return ErrMissing
	}
	pu, err := url.Parse(proxy)
	if err != nil || pu.Scheme == "" || pu.Host == "" {
		return fmt.Errorf("%w(%q)", ErrInvalid, proxy)
	}
	if !proxySchemes[strings.ToLower(pu.Scheme)] {
		return fmt.Errorf("%w(不支持的协议 %q)", ErrInvalid, pu.Scheme)
	}
	return nil
}

// Resolve 按「落盘值 > 环境变量 > 默认值」的优先级得出最终代理地址。
// 空串(含纯空白)一律视为「没配置」并继续回退,绝不返回空串表示直连。
func Resolve(saved, envProxy, fallback string) string {
	for _, candidate := range []string{saved, envProxy, fallback} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return ""
}
