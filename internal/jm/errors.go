package jm

import (
	"strings"
)

// JM 错误码(MOBILE_API.md §0.3,与 Python 版 core/errors.py 对齐)
const (
	CodeOK               = 0
	CodeBadCredentials   = 1001 // 用户名或密码错误
	CodeUnauthorized     = 1002 // token 无效/过期(HTTP 401)
	CodeCaptchaRequired  = 1003 // 需要验证码
	CodeUpstream         = 2001 // JM 服务端返回错误(data.upstream)
	CodeNetwork          = 2002 // 网络不可达/超时(data.reason)
	CodeNotFound         = 3001 // 资源不存在
	CodeInternal         = 4000 // 内部错误(data=null)
)

// APIError 是本包对外统一的业务错误,由 HTTP 层转成 {code,msg,data} 包装。
// 业务失败 HTTP 仍为 200;仅 1002 映射 HTTP 401(由 handler 处理)。
type APIError struct {
	Code int
	Msg  string
	Data map[string]any
}

func (e *APIError) Error() string { return e.Msg }

func errBadCredentials() *APIError {
	return &APIError{Code: CodeBadCredentials, Msg: "用户名或密码错误"}
}

func errCaptchaRequired() *APIError {
	return &APIError{Code: CodeCaptchaRequired, Msg: "需要验证码", Data: map[string]any{"captchaRequired": true}}
}

func errNotFound(msg string) *APIError {
	if msg == "" {
		msg = "资源不存在"
	}
	return &APIError{Code: CodeNotFound, Msg: msg}
}

func errUpstream(defaultMsg string, upstream string) *APIError {
	if defaultMsg == "" {
		defaultMsg = "JM 服务端返回错误"
	}
	return &APIError{Code: CodeUpstream, Msg: defaultMsg, Data: map[string]any{"upstream": upstream}}
}

func errNetwork(reason string) *APIError {
	return &APIError{Code: CodeNetwork, Msg: "网络不可达,请检查代理设置", Data: map[string]any{"reason": reason}}
}

func errUnauthorized() *APIError {
	return &APIError{Code: CodeUnauthorized, Msg: "登录已失效,请重新登录"}
}

// UpstreamDetail 提取上游错误详情(2001 的 data.upstream 字段),供服务端日志诊断;
// 非上游错误或无详情返回空串。
func UpstreamDetail(err error) string {
	if apiErr, ok := err.(*APIError); ok {
		if v, ok := apiErr.Data["upstream"].(string); ok {
			return v
		}
	}
	return ""
}

// ClassifyError 将任意错误映射为 APIError(handler 层入口)。
func ClassifyError(err error, defaultMsg string) *APIError {
	return classifyUpstreamError(err, defaultMsg)
}

// loginMarkers 上游「登录态无效」语义关键字(繁/简),client.go 信封路径与本分类器共用。
// 只匹配中文词:ASCII 的 login 会误伤含登录域名的网络错误(dial tcp login.xxx refused)。
var loginMarkers = []string{"登入", "登录"}

// hasLoginSemantics 判断上游错误文本是否表达「需要登录/登录态无效」。
func hasLoginSemantics(text string) bool {
	for _, marker := range loginMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// classifyUpstreamError 将任意错误映射为 APIError(live.py _map_live_error 同序):
// *APIError 直通 → 资源不存在语义 → 登录语义 → 上游错误 → 网络关键字 → 上游兜底。
func classifyUpstreamError(err error, defaultMsg string) *APIError {
	if err == nil {
		return nil
	}
	if apiErr, ok := err.(*APIError); ok {
		return apiErr
	}
	text := err.Error()
	if strings.Contains(text, "MissingAlbumPhoto") || strings.Contains(text, "album/photo 不存在") {
		return errNotFound("")
	}
	// 上游「未登录」语义(如 HTTP 403 + 「請先登入會員」裸文本,不经信封解析)
	// → 1002:让前端走「清会话→跳登录」闭环,而非笼统的「JM 服务端返回错误」。
	// 网络关键字刻意排在其后:登录语义优先级更高,且中文标记不会误伤 ASCII 的网络报错。
	if hasLoginSemantics(text) {
		return errUnauthorized()
	}
	for _, marker := range networkMarkers {
		if strings.Contains(strings.ToLower(text), strings.ToLower(marker)) {
			reason := text
			if len(reason) > 300 {
				reason = reason[:300]
			}
			return errNetwork(reason)
		}
	}
	upstream := text
	if len(upstream) > 500 {
		upstream = upstream[:500]
	}
	return errUpstream(defaultMsg, upstream)
}
