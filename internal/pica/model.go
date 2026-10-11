package pica

// 响应契约映射层:上游 JSON → /api/pica 契约字段。
// 口径:与 JM 流的列表/详情结构对齐(id/title/author/coverUrl/tags/likes/views/
// imageCount/updateAt + {page,total,hasNext,list}),前端卡片与阅读器可最大化复用模式;
// 哔咔特有字段(categories 数组、epsCount、finished、chineseTeam)单独透出。

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// fieldStr 取字符串字段(缺失/null → "";数值转字符串)。
func fieldStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return stringify(v)
}

// fieldStrOr 取字符串字段,空值回退默认。
func fieldStrOr(m map[string]any, key, def string) string {
	if s := fieldStr(m, key); s != "" && s != "<nil>" {
		return s
	}
	return def
}

// stringify 任意标量 → 字符串(float64 整数避免科学计数法)。
func stringify(v any) string {
	switch t := v.(type) {
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%f", t)
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// parseCount 数值解析(nil → 0;上游计数字段为 JSON number 或 null)。
func parseCount(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var n int
		for _, r := range t {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int(r-'0')
		}
		return n
	default:
		return 0
	}
}

func boolOf(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// fieldMap 取对象字段。
func fieldMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// fieldArray 取数组字段(缺失 → nil)。
func fieldArray(m map[string]any, key string) []any {
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

// stringArray 上游字符串数组 → []string(nil → 空切片)。
func stringArray(raw []any) []string {
	out := make([]string, 0, len(raw))
	for _, it := range raw {
		if s := strings.TrimSpace(stringify(it)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

/* ── 图片代理地址 ── */

// imageProxyURL 上游媒体(fileServer + path)→ 站内图片代理地址。
// path 白名单与域名白名单由 handler/image 管线校验,这里只负责拼装。
func imageProxyURL(fileServer, path string) string {
	return "/api/pica/image?fs=" + urlQueryEscape(fileServer) + "&path=" + urlQueryEscape(path)
}

func urlQueryEscape(s string) string {
	// url.QueryEscape 把空格编成 "+",路径里空格需为 %20(path 与签名一致)
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// marshalForLog 诊断日志用 JSON 摘要(截断)。
func marshalForLog(v any, n int) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return truncate(string(b), n)
}

/* ── 分页信封(上游统一 docs/total/pages/limit/page) ── */

// pageEnvelope 上游分页容器(嵌在 data.comics / data.eps / data.pages / data.comments)。
type pageEnvelope struct {
	Docs  []any `json:"docs"`
	Total int   `json:"total"`
	Pages int   `json:"pages"`
	Limit int   `json:"limit"`
	Page  int   `json:"page"`
}

// decodePage 解析分页容器(字段缺失按空处理;total 缺失时回退 docs 长度)。
func decodePage(data map[string]any, key string) (*pageEnvelope, bool) {
	raw, ok := data[key].(map[string]any)
	if !ok {
		return nil, false
	}
	env := &pageEnvelope{Total: parseCount(raw["total"]), Pages: parseCount(raw["pages"]), Limit: parseCount(raw["limit"]), Page: parseCount(raw["page"])}
	env.Docs = fieldArray(raw, "docs")
	if env.Total == 0 {
		env.Total = len(env.Docs)
	}
	return env, true
}

// listPage 契约列表响应({page,total,hasNext,list})。page 从 1 起。
func listPage(page int, env *pageEnvelope, mapped []any) map[string]any {
	total := env.Total
	hasNext := page < env.Pages && env.Pages > 0
	if env.Pages == 0 {
		hasNext = false
	}
	return map[string]any{
		"page":    page,
		"total":   total,
		"hasNext": hasNext,
		"list":    mapped,
	}
}

/* ── 漫画卡片/详情 ── */

// thumbProxyURL 上游 thumb(fileServer+path)→ 站内封面代理地址;空 thumb 返回 ""。
func thumbProxyURL(raw map[string]any) string {
	thumb := fieldMap(raw, "thumb")
	if thumb == nil {
		return ""
	}
	return imageProxyURL(fieldStr(thumb, "fileServer"), fieldStr(thumb, "path"))
}

// comicItemFromUpstream 上游漫画卡片 → 契约 ComicItem。
func comicItemFromUpstream(raw map[string]any) map[string]any {
	likes := parseCount(raw["likesCount"])
	if likes == 0 {
		likes = parseCount(raw["totalLikes"])
	}
	return map[string]any{
		"id":         fieldStr(raw, "_id"),
		"title":      fieldStr(raw, "title"),
		"author":     fieldStr(raw, "author"),
		"coverUrl":   thumbProxyURL(raw),
		"categories": stringArray(fieldArray(raw, "categories")),
		"tags":       stringArray(fieldArray(raw, "tags")),
		"likes":      likes,
		"views":      parseCount(raw["totalViews"]),
		"imageCount": parseCount(raw["pagesCount"]),
		"epsCount":   parseCount(raw["epsCount"]),
		"finished":   boolOf(raw["finished"]),
		"updateAt":   fieldStrOr(raw, "updated_at", fieldStrOr(raw, "created_at", "")),
	}
}

// comicItemsFromUpstream 上游卡片数组 → 契约数组。
func comicItemsFromUpstream(docs []any) []any {
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		if m, ok := d.(map[string]any); ok {
			out = append(out, comicItemFromUpstream(m))
		}
	}
	return out
}

// comicDetailFromUpstream 上游详情(data.comic)→ 契约详情(卡片字段 + 简介/汉化组)。
func comicDetailFromUpstream(raw map[string]any) map[string]any {
	out := comicItemFromUpstream(raw)
	out["description"] = fieldStr(raw, "description")
	out["chineseTeam"] = fieldStr(raw, "chineseTeam")
	out["createdAt"] = fieldStr(raw, "created_at")
	out["allowDownload"] = boolOf(raw["allowDownload"])
	return out
}
