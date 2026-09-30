package jm

// ComicItem 契约映射(MOBILE_API.md §2.1;live.py _comic_item 直译)。
//
// 归属说明:本函数由账号互动部分(#21 收藏列表)引入;内容端点(#7-#13 列表)
// 复用同一实现,避免各文件重复映射造成口径漂移。

import (
	"net/url"
	"strings"
)

// ComicItemFromUpstream 上游专辑条目(decoded JSON 对象)→ 契约 ComicItem。
// 口径(live.py _comic_item):
//   - coverUrl:上游 image → 站内 /api/image 代理路径;绝对 URL 只保留路径部分
//     (防 SSRF);剥离 ?query(如 serialization 列表项的 ?u= 缓存参数,path 白名单
//     不含 query);反斜杠归一为 /;image 为空时回退 media/albums/{aid}_3x4.jpg。
//   - category:上游 category.title(非 dict 时 str());categorySub:category_sub.title,
//     缺失/空 → null。
//   - likes/views/imageCount:缺失 → 0(_parse_count)。
//   - updateAt:优先上游 adddate 字符串;否则 epoch update_at → 东八区 %Y-%m-%d;
//     均无 → ""。
func ComicItemFromUpstream(aid string, ainfo map[string]any) map[string]any {
	image := fieldStr(ainfo, "image")
	path := ""
	if image != "" {
		p := image
		if strings.HasPrefix(p, "http") {
			// 绝对 URL 只保留路径部分(urlsplit(image).path)
			if u, err := url.Parse(p); err == nil {
				p = u.Path
			}
		}
		// 剥离 ?query + 反斜杠归一 + 去首部 /
		if idx := strings.Index(p, "?"); idx >= 0 {
			p = p[:idx]
		}
		p = strings.ReplaceAll(p, "\\", "/")
		path = strings.TrimLeft(p, "/")
	}
	if path == "" {
		path = coverPath(aid)
	}

	category := ""
	if v := ainfo["category"]; v != nil {
		if m, ok := v.(map[string]any); ok {
			category = fieldStr(m, "title")
		} else if s := stringify(v); s != "" {
			category = s
		}
	}
	categorySub := any(nil)
	if m, ok := ainfo["category_sub"].(map[string]any); ok {
		if s := fieldStr(m, "title"); s != "" {
			categorySub = s
		}
	}

	tags := []any{}
	if arr, ok := ainfo["tags"].([]any); ok {
		tags = arr
	}

	// updateAt:adddate 优先(Python `or` 语义:None/""/0 视为缺失)
	updateAt := fieldStrOr(ainfo, "adddate", "")
	if updateAt == "" || updateAt == "0" {
		updateAt = epochToDongba(ainfo["update_at"], false)
	}

	return map[string]any{
		"aid":         aid,
		"title":       fieldStrOr(ainfo, "name", ""),
		"author":      fieldStrOr(ainfo, "author", ""),
		"coverUrl":    imageProxyURL(path, "0", aid),
		"tags":        tags,
		"category":    category,
		"categorySub": categorySub,
		"likes":       parseCount(ainfo["likes"]),
		"views":       parseCount(ainfo["views"]),
		"imageCount":  parseCount(ainfo["images_count"]),
		"updateAt":    updateAt,
	}
}
