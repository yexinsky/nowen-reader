package jm

// JM 内容端点契约映射(MOBILE_API.md #6-#15)。
// 口径逐条对齐 mobile/server/core/live.py:_map_categories / _book_items /
// _has_next / promote / latest / serialization / week_categories / week_filter /
// detail / photos。ComicItem 映射复用 model_comicitem.go 的 ComicItemFromUpstream;
// UserInfo/CommentItem 分别见 model.go / api_account.go。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

/* ── 通用小工具 ── */

// truthyAny Python truthy 语义:nil/空串/0/false → false,其余 true。
func truthyAny(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return true
	}
}

// firstTruthyStr 依次取第一个 truthy 字段的字符串形式(Python `a or b or ""` 语义)。
func firstTruthyStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; truthyAny(v) {
			return stringify(v)
		}
	}
	return ""
}

// decodeLooseDict 解码顶层对象:非 JSON → 2001(live.py _req_decoded 的
// json.loads 失败分支);JSON 但非对象 → 空 dict(Python
// `data if isinstance(data, dict) else {}`)。
func decodeLooseDict(data []byte) (map[string]any, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errUpstream("响应数据解密失败", truncate(err.Error(), 300))
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return map[string]any{}, nil
}

// hasNextMaxPage hasNext 判定(live.py _has_next 完整版,适配上游 total 封顶
// 10000、页码封顶 120 的行为):
// ① 本页不满一页(含空页)→ false(覆盖 total 缺失/为 0 的兜底);
// ② total>0 且 page*页大小 >= total → false;
// ③ maxPage>0 且 page >= maxPage → false(index/search 传 120,防止越过
// 上游页码上限后重复拉到冻结页);否则 true。
// total 兼容缺失/字符串/数字(live.py int(total) 语义,失败按 0)。
func hasNextMaxPage(page int, total any, pageLen, pageSize, maxPage int) bool {
	if pageLen < pageSize {
		return false
	}
	if totalI := parseCount(total); totalI > 0 && page*pageSize >= totalI {
		return false
	}
	if maxPage > 0 && page >= maxPage {
		return false
	}
	return true
}

// bookList 上游书籍数组 → ComicItem 契约列表(live.py _book_items 直译):
// 非 dict 项跳过;aid 取 id 字段(缺失 → "")。
func bookList(books any) []any {
	items := []any{}
	for _, b := range listOf(books) {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, ComicItemFromUpstream(fieldStr(m, "id"), m))
	}
	return items
}

/* ── #6 分类树 ── */

// mapCategories 上游 /categories → 契约 #6(live.py _map_categories 直译):
// 顶层键 id → str;子分类主键 CID(部分上游版本兼容 id)→ str。
func mapCategories(data []byte) (any, error) {
	raw, ok := decodeMap(data)
	if !ok {
		return nil, errUpstream("获取分类失败", truncate(string(data), 300))
	}
	result := []any{}
	for _, v := range listOf(raw["categories"]) {
		d, ok := v.(map[string]any)
		if !ok {
			continue
		}
		children := []any{}
		for _, sv := range listOf(d["sub_categories"]) {
			sd, ok := sv.(map[string]any)
			if !ok {
				continue
			}
			children = append(children, map[string]any{
				"id":   firstTruthyStr(sd, "CID", "id"),
				"name": fieldStr(sd, "name"),
			})
		}
		result = append(result, map[string]any{
			"id":       fieldStr(d, "id"),
			"name":     fieldStr(d, "name"),
			"children": children,
		})
	}
	return result, nil
}

/* ── #8 首页推荐区块 ── */

// mapPromote 上游 /promote(data 顶层区块数组)→ 契约 #8(live.py promote 直译):
// 过滤 type in (novels, library);首个保留区块固定 kind="serialization" 无 list
// (桌面端该 tab 走 /serialization 连载逻辑,不使用区块自带书单);其余区块
// kind="static" 携带 content 书单映射的 list;key 为 s1..sN 顺序编号。
func mapPromote(data []byte) (any, error) {
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, errUpstream("响应数据解密失败", truncate(err.Error(), 300))
	}
	sections := []any{}
	for _, v := range arr {
		sec, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if promoteFilterTypes[fieldStr(sec, "type")] {
			continue
		}
		if len(sections) == 0 {
			sections = append(sections, map[string]any{
				"key":   "s1",
				"title": fieldStr(sec, "title"),
				"kind":  "serialization",
			})
			continue
		}
		sections = append(sections, map[string]any{
			"key":   fmt.Sprintf("s%d", len(sections)+1),
			"title": fieldStr(sec, "title"),
			"kind":  "static",
			"list":  bookList(sec["content"]),
		})
	}
	return map[string]any{"sections": sections}, nil
}

/* ── #9 最近更新 ── */

// mapLatestBooks /latest 顶层数组 → ComicItem 列表(live.py latest:
// _book_items(data);非 JSON → 2001,null/非数组 → 空列表)。
func mapLatestBooks(data []byte) ([]any, error) {
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, errUpstream("响应数据解密失败", truncate(err.Error(), 300))
	}
	return bookList(arr), nil
}

/* ── #11 每周必看期数 ── */

// mapWeekCategories 上游 /week → 契约 #11(live.py week_categories 直译):
// data={categories:[{id,title,time}],type};契约 title 取上游 time 字段
// (fallback title,桌面端下拉框展示 time),丢弃上游 type 字段。
func mapWeekCategories(data []byte) (any, error) {
	raw, err := decodeLooseDict(data)
	if err != nil {
		return nil, err
	}
	categories := []any{}
	for _, v := range listOf(raw["categories"]) {
		d, ok := v.(map[string]any)
		if !ok {
			continue
		}
		categories = append(categories, map[string]any{
			"id":    fieldStr(d, "id"),
			"title": firstTruthyStr(d, "time", "title"),
		})
	}
	return map[string]any{"categories": categories}, nil
}

/* ── #14 漫画详情 ── */

// mapAlbumDetail 上游 /album → 契约 #14(live.py detail 直译):
//   - 上游 name 为空 → 3001;
//   - author 取上游 author 数组首值(字符串直接用),缺失 → "default_author";
//   - category 恒 {"id":"0","name":"全部","sub":null}(上游专辑接口未返回分类);
//   - likes=likes、views=total_views;imageCount 恒 0(真实数量见 photos);
//   - updateAt=update_at(epoch → 东八区日期)或 addtime,可能为空串;
//   - episodes:series 按 sort 排序(sort 缺失/0 按序号兜底,title 缺失 → 第{n}话),
//     无 series → 单默认章(pid=aid,title=标题,order=1);
//     响应不含 episodes[].imageCount(与 mock 的差异,契约 #5 差异表 #3)。
func mapAlbumDetail(data []byte, aid string) (any, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, errUpstream("详情响应解析失败", truncate(string(data), 300))
	}
	if fieldStr(raw, "name") == "" {
		return nil, errNotFound(fmt.Sprintf("漫画不存在: %s", aid))
	}
	aidResolved := aid
	if v, exists := raw["id"]; exists && v != nil {
		aidResolved = stringify(v)
	}
	author := "default_author"
	switch v := raw["author"].(type) {
	case string:
		if v != "" {
			author = v
		}
	case []any:
		if len(v) > 0 {
			author = stringify(v[0])
		}
	}
	authors := albumAuthors(raw["author"])
	// update_at 优先,addtime 兜底(Python `or` 语义)
	var ts any
	if truthyAny(raw["update_at"]) {
		ts = raw["update_at"]
	} else {
		ts = raw["addtime"]
	}
	return map[string]any{
		"aid":         aidResolved,
		"title":       fieldStr(raw, "name"),
		"author":      author,
		"authors":     authors,
		"coverUrl":    coverURL(aidResolved),
		"description": stripHTML(fieldStr(raw, "description")),
		"tags":        append([]any{}, listOf(raw["tags"])...),
		"category":    map[string]any{"id": "0", "name": "全部", "sub": nil},
		"likes":       parseCount(raw["likes"]),
		"views":       parseCount(raw["total_views"]),
		"imageCount":  0,
		"epCount":     len(albumEpisodeList(raw, aidResolved)),
		"updateAt":    epochToDongba(ts, false),
		"liked":       boolOf(raw["liked"]),
		"favorited":   boolOf(raw["is_favorite"]),
		"episodes":    albumEpisodeList(raw, aidResolved),
	}, nil
}

// albumAuthors 上游 author 字段(string 或数组)→ 归一作者名数组:trim、剔空、
// 按序去重;上游缺失/为空 → 空数组(非 nil——nil 会序列化成 JSON null,前端
// 契约要求恒为数组,缺失 → [])。作者标签与作者搜索(searchType=author)使用;
// 探针口径:aid=1475046 实测上游 author=["N/A"](数组)。
func albumAuthors(v any) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	switch t := v.(type) {
	case string:
		if s := strings.TrimSpace(t); s != "" {
			out = append(out, s)
		}
	case []any:
		for _, it := range t {
			s := strings.TrimSpace(stringify(it))
			if s == "" {
				continue
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	default:
		if v != nil {
			if s := strings.TrimSpace(stringify(v)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// albumEpisodeList 契约 episodes(live.py detail):series 各章按 sort 排序
// (sort 缺失/0 按已收章节序号兜底;title 缺失 → "第{n}话");无 series →
// 单默认章(pid=aid,title=标题,order=1)。
func albumEpisodeList(raw map[string]any, aid string) []any {
	series := listOf(raw["series"])
	if len(series) == 0 {
		return []any{map[string]any{"pid": aid, "title": fieldStr(raw, "name"), "order": 1}}
	}
	type episode struct {
		pid   string
		title string
		order int
	}
	eps := make([]episode, 0, len(series))
	for _, v := range series {
		s, ok := v.(map[string]any)
		if !ok {
			continue
		}
		order := parseCount(s["sort"])
		if order == 0 {
			order = len(eps) + 1
		}
		title := fieldStr(s, "name")
		if title == "" {
			title = fmt.Sprintf("第%d话", order)
		}
		eps = append(eps, episode{pid: fieldStr(s, "id"), title: title, order: order})
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].order < eps[j].order })
	out := make([]any, 0, len(eps))
	for _, e := range eps {
		out = append(out, map[string]any{"pid": e.pid, "title": e.title, "order": e.order})
	}
	return out
}

/* ── #15 章节 ── */

// chapterAlbumID 章节所属专辑 id:优先 album_id,兼容 series_id;两者缺失/为 0
// 视为单章本子(SDK is_single_album),专辑即章节自身。
func chapterAlbumID(chapter map[string]any, pid string) string {
	for _, key := range []string{"album_id", "series_id"} {
		if v := fieldStr(chapter, key); v != "" && v != "0" {
			return v
		}
	}
	return pid
}

// chapterPageArr 章节图片文件名列表:上游字段 images(SDK field_adapter 将其
// 映射为 page_arr),兼容 JSON 字符串编码(SDK 构造器同款兼容)与
// {page,image} 对象形态(桌面端 ParseBookEps 口径:取 image 字段文件名)。
func chapterPageArr(chapter map[string]any) []string {
	raw, ok := chapter["images"]
	if !ok || raw == nil {
		raw = chapter["page_arr"]
	}
	var list []any
	switch t := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(t), &list)
	case []any:
		list = t
	}
	names := make([]string, 0, len(list))
	for _, it := range list {
		switch t := it.(type) {
		case string:
			if t != "" {
				names = append(names, t)
			}
		case map[string]any:
			img := fieldStr(t, "image")
			if img == "" {
				continue
			}
			if idx := strings.Index(img, "media/photos/"); idx >= 0 {
				img = img[idx+len("media/photos/"):]
				if slash := strings.Index(img, "/"); slash >= 0 {
					img = img[slash+1:]
				}
			} else if slash := strings.LastIndex(img, "/"); slash >= 0 {
				img = img[slash+1:]
			}
			if q := strings.Index(img, "?"); q >= 0 {
				img = img[:q]
			}
			if img != "" {
				names = append(names, img)
			}
		}
	}
	return names
}

// mapChapterPhotos 章节 → 契约 #15(live.py photos 直译):
//   - images 从 page_arr 逐张拼装 media/photos/{pid}/{文件名},width/height 恒 0;
//   - epIndex = max(album_index-1, 0),album_index 优先取所属专辑 series 中的
//     位置(1 基,SDK post_adapt_photo 同语义);未出现时回退章节自身 sort
//     (缺省 1;单章本子 sort=2 视为 1,SDK album_index 同款修正);
//   - scramble = 上游 scramble_id,缺省 "0";
//   - hasNext/nextPid 由所属专辑 series 顺序判定(未找到/无 series → false/null)。
func mapChapterPhotos(chapter, album map[string]any) map[string]any {
	pid := fieldStr(chapter, "id")
	if pid == "" {
		pid = fieldStr(chapter, "album_id")
	}
	eps := make([]string, 0, len(listOf(album["series"])))
	for _, v := range listOf(album["series"]) {
		if m, ok := v.(map[string]any); ok {
			eps = append(eps, fieldStr(m, "id"))
		}
	}
	albumIndex := 0
	hasNext, nextPid := false, any(nil)
	for i, epID := range eps {
		if epID == pid {
			albumIndex = i + 1
			if i+1 < len(eps) {
				hasNext, nextPid = true, eps[i+1]
			}
			break
		}
	}
	if albumIndex == 0 {
		sortNo := parseCount(chapter["sort"])
		if sortNo == 0 {
			sortNo = 1
		}
		if len(eps) == 0 && sortNo == 2 {
			// SDK:单章本子 JM 给的 sort 为 2,按语义修正为 1
			sortNo = 1
		}
		albumIndex = sortNo
	}
	epIndex := albumIndex - 1
	if epIndex < 0 {
		epIndex = 0
	}
	images := []any{}
	for i, fname := range chapterPageArr(chapter) {
		images = append(images, map[string]any{
			"index":  i + 1,
			"path":   fmt.Sprintf("media/photos/%s/%s", pid, fname),
			"width":  0,
			"height": 0,
		})
	}
	// 上游 /chapter 响应不含 scramble_id 字段 → SDK 缺省值 220980
	// (SCRAMBLE_220980;calcNum 侧对老作品 aid<220980 自然返回 0)
	scramble := fieldStr(chapter, "scramble_id")
	if scramble == "" || scramble == "0" {
		scramble = "220980"
	}
	return map[string]any{
		"pid":      pid,
		"aid":      chapterAlbumID(chapter, pid),
		"title":    fieldStr(chapter, "name"),
		"epIndex":  epIndex,
		"scramble": scramble,
		"hasNext":  hasNext,
		"nextPid":  nextPid,
		"images":   images,
	}
}
