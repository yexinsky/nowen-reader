package jm

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// cstZone 东八区(live.py _epoch_to_str 口径:epoch → UTC+8)。
var cstZone = time.FixedZone("UTC+8", 8*3600)

func formatDongba(sec int64, withTime bool) string {
	t := time.Unix(sec, 0).In(cstZone)
	if withTime {
		return t.Format("2006-01-02 15:04")
	}
	return t.Format("2006-01-02")
}

// 响应契约映射层:上游 decoded JSON → MOBILE_API.md 契约字段。
// 口径逐条对齐 mobile/server/core/live.py(东八区时间、计数解析、HTML 剥离、URL 代理化)。

// parseCount 计数字符串解析(live.py _parse_count):
// "918"/"[1K]"/"40K"/int → int(K/M 换算);解析失败 → 0。
func parseCount(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		s := strings.TrimSpace(t)
		s = strings.Trim(s, "[]")
		if s == "" {
			return 0
		}
		mult := 1
		upper := strings.ToUpper(s)
		switch {
		case strings.HasSuffix(upper, "K"):
			mult = 1000
			s = strings.TrimSuffix(upper, "K")
		case strings.HasSuffix(upper, "M"):
			mult = 1000000
			s = strings.TrimSuffix(upper, "M")
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return 0
		}
		return int(n * float64(mult))
	default:
		return 0
	}
}

// imageProxyURL 把上游媒体相对路径包装为站内 /api/image 代理地址(带 query)。
// path 白名单校验由 handler 的 image 端点执行,这里只负责拼装。
func imageProxyURL(path, scramble, aid string) string {
	if scramble == "" {
		scramble = "0"
	}
	q := url.Values{}
	q.Set("path", path)
	q.Set("scramble", scramble)
	if aid != "" {
		q.Set("aid", aid)
	}
	return "/api/image?" + q.Encode()
}

// avatarURL 头像映射(live.py _avatar_url):
// 绝对 URL 只保留路径部分(防 SSRF);nopic → null;否则 media/users/{photo}。
func avatarURL(photo any) any {
	if photo == nil {
		return nil
	}
	p := fmt.Sprint(photo)
	if p == "" || p == "<nil>" {
		return nil
	}
	if strings.HasPrefix(p, "http") {
		inner := strings.TrimPrefix(p, "://")
		if idx := strings.Index(inner, "/"); idx >= 0 {
			inner = inner[idx+1:]
		} else {
			return nil
		}
		if inner == "" {
			return nil
		}
		return imageProxyURL(inner, "0", "")
	}
	if strings.HasPrefix(p, "nopic") {
		return nil
	}
	return imageProxyURL("media/users/"+p, "0", "")
}

// coverPath 默认封面路径(live.py _cover_path)。
func coverPath(aid string) string {
	return fmt.Sprintf("media/albums/%s_3x4.jpg", aid)
}

// coverURL 封面代理地址(live.py _cover_url)。
func coverURL(aid string) string {
	return imageProxyURL(coverPath(aid), "0", aid)
}

// UserInfoMap 上游登录 decoded 快照 → 契约 UserInfo(live.py _user_info 直译)。
func UserInfoMap(u map[string]any) map[string]any {
	return userInfoMap(u)
}

// userInfoMap 上游登录 decoded 快照 → 契约 UserInfo(live.py _user_info 直译)。
func userInfoMap(u map[string]any) map[string]any {
	return map[string]any{
		"userId":       fmt.Sprint(fieldStr(u, "uid")),
		"username":     fieldStr(u, "username"),
		"email":        fieldStrOr(u, "email", ""),
		"avatarUrl":    avatarURL(u["photo"]),
		"levelName":    nullable(u["level_name"]),
		"level":        parseCount(orDefault(u["level"], float64(0))),
		"gender":       fieldStrOr(u, "gender", ""),
		"coin":         parseCount(u["coin"]),
		"soulCoin":     parseCount(orDefault(u["soul_coin"], float64(0))),
		"exp":          parseCount(orDefault(u["exp"], float64(0))),
		"nextLevelExp": parseCount(orDefault(u["nextLevelExp"], float64(0))),
		"favorites":    parseCount(orDefault(u["album_favorites"], float64(0))),
		"canFavorites": parseCount(orDefault(u["album_favorites_max"], float64(0))),
		"vip":          boolOf(u["vip"]),
		"vipExpire":    nullable(u["vip_expire"]),
	}
}

// stringify 任意标量 → 字符串;数值型避免科学计数法(fmt.Sprint(float64) 会输出
// 1.477132e+06 这类形态,ID 字段必须为整数串)。
func stringify(v any) string {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		f := float64(t)
		if f == math.Trunc(f) && math.Abs(f) < 1e15 {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	case int, int64, int32, uint, uint64, uint32:
		return fmt.Sprint(t)
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

// fieldStr 取字符串字段(缺失 → "");数值等类型转整数串语义(str() 语义)。
func fieldStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return stringify(v)
}

func fieldStrOr(m map[string]any, key, def string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	if s := stringify(v); s != "" && s != "<nil>" {
		return s
	}
	return def
}

func orDefault(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}

// nullable:非空原样返回,空值(null/空串)→ nil(JSON null)。
func nullable(v any) any {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	return v
}

func boolOf(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	default:
		return false
	}
}

// stripHTML 剥离 HTML(live.py _strip_html,用于详情简介与评论内容):
// <br> → 换行 → 剥全部标签 → HTML 实体还原 → 连续空行压缩为最多 2 个换行 → 首尾去空白。
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"<BR>", "\n", "<Br>", "\n",
	)
	s = replacer.Replace(s)
	// 剥全部标签
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	s = htmlUnescape(b.String())
	// 连续空行压缩为最多 2 个换行
	lines := strings.Split(s, "\n")
	var out []string
	blankRun := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun <= 2 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// htmlUnescape 常见 HTML 实体还原(html.unescape 的常用子集)。
func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"",
		"&#39;", "'", "&apos;", "'", "&nbsp;", " ", "&#x27;", "'",
		"&mdash;", "—", "&ndash;", "–", "&hellip;", "…",
		"&laquo;", "«", "&raquo;", "»", "&times;", "×",
	)
	return r.Replace(s)
}

// epochToDongba epoch 秒 → 东八区格式(live.py _epoch_to_str):
// ≥9 位纯数字 → UTC+8 的 "%Y-%m-%d %H:%M"(含秒参)或 "%Y-%m-%d";
// 非纯数字字符串原样透传;纯数字不足 9 位或空 → ""。
func epochToDongba(v any, withTime bool) string {
	switch t := v.(type) {
	case float64:
		sec := int64(t)
		if sec < 100000000 {
			return ""
		}
		return formatDongba(sec, withTime)
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return ""
		}
		if isAllDigits(s) {
			if len(s) < 9 {
				return ""
			}
			sec, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return ""
			}
			return formatDongba(sec, withTime)
		}
		return s
	default:
		return ""
	}
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
