package jm

// detail_meta.go 从已映射的契约响应(#13 搜索条目 / #14 mapAlbumDetail)提取
// 标签补全(jm_backfill)所需字段,供 handler 层复用;
// 避免 handler 直接触碰包内未导出的解析工具(dataMap/fieldStr/normalizeJmTags)。

// ComicItemMeta 搜索结果条目中补标签关心的字段。
type ComicItemMeta struct {
	Aid      string
	Title    string
	Author   string
	Tags     []string // normalizeJmTags 口径:trim/去重/上限 maxDownloadTags
	CoverURL string   // 站内 /api/image 代理路径(#13 条目经 ComicItemFromUpstream 映射)
}

// ExtractComicItemMeta 从 #13 搜索结果 list 元素提取字段;
// 非 map 或缺 aid(异常条目)返回 false。
func ExtractComicItemMeta(item any) (ComicItemMeta, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return ComicItemMeta{}, false
	}
	aid := fieldStr(m, "aid")
	if aid == "" {
		return ComicItemMeta{}, false
	}
	return ComicItemMeta{
		Aid:      aid,
		Title:    fieldStr(m, "title"),
		Author:   fieldStr(m, "author"),
		Tags:     normalizeJmTags(listOf(m["tags"])),
		CoverURL: fieldStr(m, "coverUrl"),
	}, true
}

// ExtractDetailMeta 从 #14 详情响应(mapAlbumDetail 映射结果)提取标签与作者。
// 响应缺失/形态异常返回零值,调用方按"上游无标签"处理。
func ExtractDetailMeta(data any) (tags []string, author string) {
	m := dataMap(data)
	if m == nil {
		return nil, ""
	}
	return normalizeJmTags(listOf(m["tags"])), fieldStr(m, "author")
}
