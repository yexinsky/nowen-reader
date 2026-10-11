package pica

import (
	"strconv"
	"strings"
)

// Pica 错误码(沿用 JM 契约语义,前端同一套 UX:1002→清会话跳登录、2002→提示配代理)。
const (
	CodeOK             = 0
	CodeBadCredentials = 1001 // 邮箱或密码错误(上游 1004)
	CodeUnauthorized   = 1002 // token 无效/过期(上游 1002 / HTTP 401)
	CodeUpstream       = 2001 // 哔咔服务端返回错误(data.upstream)
	CodeNetwork        = 2002 // 网络不可达/超时(data.reason)
	CodeNotFound       = 3001 // 资源不存在
	CodeInternal       = 4000 // 内部错误
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
	return &APIError{Code: CodeBadCredentials, Msg: "邮箱或密码错误"}
}

func errNotFound(msg string) *APIError {
	if msg == "" {
		msg = "资源不存在"
	}
	return &APIError{Code: CodeNotFound, Msg: msg}
}

func errUpstream(defaultMsg string, upstream string) *APIError {
	if defaultMsg == "" {
		defaultMsg = "哔咔服务端返回错误"
	}
	return &APIError{Code: CodeUpstream, Msg: defaultMsg, Data: map[string]any{"upstream": upstream}}
}

func errNetwork(reason string) *APIError {
	return &APIError{Code: CodeNetwork, Msg: "网络不可达,请检查代理设置", Data: map[string]any{"reason": reason}}
}

func errUnauthorized() *APIError {
	return &APIError{Code: CodeUnauthorized, Msg: "登录已失效,请重新登录"}
}

// errSuspiciousFakeResponse 上游静默投喂假响应的显式报错(绝不把假数据当真数据用):
// 实测上游对非 okhttp TLS 指纹返回 {"code":200,"message":"success"} 且无 data 字段。
func errSuspiciousFakeResponse() *APIError {
	return &APIError{
		Code: CodeUpstream,
		Msg:  "上游返回疑似伪造响应(TLS 指纹被识别),请检查代理/客户端配置",
		Data: map[string]any{"upstream": "code=200 but no data field (fake response pattern)"},
	}
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

// classifyUpstreamError 将任意错误映射为 APIError:
// *APIError 直通 → 登录语义 → 网络关键字 → 上游兜底。
func classifyUpstreamError(err error, defaultMsg string) *APIError {
	if err == nil {
		return nil
	}
	if apiErr, ok := err.(*APIError); ok {
		return apiErr
	}
	text := err.Error()
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

// networkMarkers 网络层错误关键字(小写匹配)。
var networkMarkers = []string{
	"connection refused", "connection reset", "timeout", "deadline exceeded",
	"no such host", "network is unreachable", "tls:", "x509", "proxy",
	"eof", "broken pipe", "proxyconnect",
}

// loginMarkers 上游「登录态无效」语义关键字(错误 1002 的 message 等)。
var loginMarkers = []string{"未登录", "請先登入", "请先登录", "无效的令牌", "invalid token", "unauthorized"}

// hasLoginSemantics 判断上游错误文本是否表达「需要登录/登录态无效」。
func hasLoginSemantics(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range loginMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// isCredentialErrorText 判断错误文本是否表达「凭据错误」(登录失败)。
func isCredentialErrorText(msg string) bool {
	lower := strings.ToLower(msg)
	for _, marker := range []string{"invalid email", "invalid password", "email or password", "incorrect password", "wrong password"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// mapUpstreamCode 上游业务 code → 本契约错误码(错误码表见 API 文档 §5)。
// 上游 code 可能为字符串("1004")或数字,调用方先归一为 int。
// 实测备注:错误凭据在部分形态下以 code=400 + "invalid email or password"
// 表达(文档错误码表的 1004 为另一形态),此处一并兼容。
func mapUpstreamCode(code int, msg string) *APIError {
	switch code {
	case 200:
		return nil
	case 1002, 401:
		// 认证有问题(token 失效等)
		return errUnauthorized()
	case 1004:
		// 用户名或密码错误
		return errBadCredentials()
	case 1007:
		return errNotFound("404 找不到")
	case 400:
		if isCredentialErrorText(msg) {
			return errBadCredentials()
		}
		return errUpstream(msg, upstreamDetailText(code, msg))
	default:
		// 1006 未激活/1014 审核中/1017 收藏已满/1019 等级不足/1023 限流/500 服务器错误等
		// → 2001 带上游原文,前端原样展示 message
		text := msg
		if text == "" {
			text = codeErrMsg(code)
		}
		return errUpstream(text, upstreamDetailText(code, msg))
	}
}

func codeErrMsg(code int) string {
	switch code {
	case 1006:
		return "账户未激活,请查收邀请函邮件"
	case 1014:
		return "漫画正在审核中"
	case 1017:
		return "收藏已满"
	case 1019:
		return "等级不够或未认证(等级 2 才能留言)"
	case 1023:
		return "请求太频繁,请稍后再试"
	case 1029:
		return "时间线区域限制"
	case 500:
		return "服务器内部错误"
	default:
		return "哔咔服务端返回错误"
	}
}

func upstreamDetailText(code int, msg string) string {
	return "upstream code=" + strconv.Itoa(code) + " msg=" + msg
}
