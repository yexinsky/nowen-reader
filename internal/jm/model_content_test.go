package jm

// 内容端点契约映射单测(MOBILE_API.md #6-#15 / §2.1)。
// 重点覆盖:hasNext 判定、mainCategory/sort 归一化、promote 区块过滤、
// categories CID 主键、详情 episodes、章节 photos 顺序判定、书籍列表映射。

import (
	"encoding/json"
	"testing"
)

func contentFixtureJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

func TestHasNextMaxPage(t *testing.T) {
	cases := []struct {
		name    string
		page    int
		total   any
		pageLen int
		size    int
		maxPage int
		want    bool
	}{
		{"空页", 1, nil, 0, 80, 0, false},
		{"不满一页", 3, nil, 79, 80, 0, false},
		{"满页无total", 1, nil, 80, 80, 0, true},
		{"total边界内", 1, 160, 80, 80, 0, true},
		{"total边界命中", 2, 160, 80, 80, 0, false},
		{"total字符串", 2, "160", 80, 80, 0, false},
		{"total封顶10000", 124, 10000, 80, 80, 120, false},
		{"maxPage封顶", 120, nil, 80, 80, 120, false},
		{"maxPage前一页", 119, 10000, 80, 80, 120, true},
		{"serialization页大小40", 1, 100, 40, 40, 0, true},
		{"serialization末页", 3, 100, 40, 40, 0, false},
	}
	for _, tc := range cases {
		if got := hasNextMaxPage(tc.page, tc.total, tc.pageLen, tc.size, tc.maxPage); got != tc.want {
			t.Errorf("%s: hasNextMaxPage(%d,%v,%d,%d,%d)=%v want %v",
				tc.name, tc.page, tc.total, tc.pageLen, tc.size, tc.maxPage, got, tc.want)
		}
	}
}

func TestMainCategorySlugMapping(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"0":              "",
		"all":            "",
		"ALL":            "",
		"1":              "doujin",
		"2":              "single",
		"3":              "short",
		"4":              "another",
		"5":              "hanman",
		"6":              "meiman",
		"7":              "another_cosplay", // doujin_cosplay → 当前真实 slug
		"8":              "3D",
		"3d":             "3D",
		"doujin_cosplay": "another_cosplay",
		"Doujin":         "doujin",
		"doujin_chinese": "doujin_chinese", // 子分类 slug 原样透传
	}
	for in, want := range cases {
		if got := mainCategorySlug(in); got != want {
			t.Errorf("mainCategorySlug(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSearchOrderByMapping(t *testing.T) {
	cases := map[string]string{
		"": "mr", "unknown": "mr",
		"mr": "mr", "mv": "mv", "mv_m": "mv_m", "mv_w": "mv_w",
		"mv_t": "mv_t", "mp": "mp", "tf": "tf",
	}
	for in, want := range cases {
		if got := searchOrderBy(in); got != want {
			t.Errorf("searchOrderBy(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMapCategoriesContract(t *testing.T) {
	raw := contentFixtureJSON(t, map[string]any{"categories": []any{
		map[string]any{
			"id":   0,
			"name": "全部",
			"sub_categories": []any{
				map[string]any{"CID": 1, "id": 5, "name": "同人志"},
				map[string]any{"id": 9, "name": "无CID兼容"},
			},
		},
	}})
	data, err := mapCategories(raw)
	if err != nil {
		t.Fatalf("mapCategories: %v", err)
	}
	list, ok := data.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("data 应为长度 1 的数组,得到 %#v", data)
	}
	top := list[0].(map[string]any)
	if top["id"] != "0" || top["name"] != "全部" {
		t.Errorf("顶层 id/name: %#v", top)
	}
	children := top["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("children 长度: %d", len(children))
	}
	if c := children[0].(map[string]any); c["id"] != "1" || c["name"] != "同人志" {
		t.Errorf("子分类应取 CID 主键: %#v", c)
	}
	if c := children[1].(map[string]any); c["id"] != "9" {
		t.Errorf("无 CID 时应兼容 id: %#v", c)
	}
}

func TestMapPromoteSections(t *testing.T) {
	raw := contentFixtureJSON(t, []any{
		map[string]any{"title": "小说区", "type": "novels", "content": []any{map[string]any{"id": "1"}}},
		map[string]any{"title": "连载更新", "type": "serialization", "content": []any{}},
		map[string]any{"title": "热门推荐", "type": "other", "content": []any{
			map[string]any{"id": "3", "name": "B"},
		}},
		map[string]any{"title": "书库", "type": "library", "content": []any{}},
	})
	data, err := mapPromote(raw)
	if err != nil {
		t.Fatalf("mapPromote: %v", err)
	}
	sections := data.(map[string]any)["sections"].([]any)
	if len(sections) != 2 {
		t.Fatalf("novels/library 应被过滤,sections=%#v", sections)
	}
	first := sections[0].(map[string]any)
	if first["key"] != "s1" || first["kind"] != "serialization" || first["title"] != "连载更新" {
		t.Errorf("首个保留区块应为 serialization 且无 list: %#v", first)
	}
	if _, hasList := first["list"]; hasList {
		t.Errorf("serialization 区块不应携带 list")
	}
	second := sections[1].(map[string]any)
	if second["key"] != "s2" || second["kind"] != "static" {
		t.Errorf("第二区块 key/kind: %#v", second)
	}
	list := second["list"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["aid"] != "3" {
		t.Errorf("static 区块书单映射: %#v", list)
	}
}

func TestMapWeekCategoriesTitle(t *testing.T) {
	raw := contentFixtureJSON(t, map[string]any{
		"categories": []any{
			map[string]any{"id": "259", "title": "原始标题", "time": "2026第258期09.25 - 09.18"},
			map[string]any{"id": "2", "title": "仅title"},
			map[string]any{"id": 3},
		},
	})
	data, err := mapWeekCategories(raw)
	if err != nil {
		t.Fatalf("mapWeekCategories: %v", err)
	}
	cats := data.(map[string]any)["categories"].([]any)
	if len(cats) != 3 {
		t.Fatalf("categories 长度: %d", len(cats))
	}
	if c := cats[0].(map[string]any); c["title"] != "2026第258期09.25 - 09.18" {
		t.Errorf("title 应取上游 time 字段: %#v", c)
	}
	if c := cats[1].(map[string]any); c["title"] != "仅title" {
		t.Errorf("time 缺失应回退 title: %#v", c)
	}
	if c := cats[2].(map[string]any); c["id"] != "3" || c["title"] != "" {
		t.Errorf("id 应 str 化且 title 允许空: %#v", c)
	}
}

func TestMapAlbumDetailContract(t *testing.T) {
	raw := contentFixtureJSON(t, map[string]any{
		"id":          "439695",
		"name":        "测试漫画",
		"author":      []any{"甲", "乙"},
		"description": "<br>x<br/><b>y</b>",
		"tags":        []any{"tag1"},
		"likes":       "[1K]",
		"total_views": "40K",
		"series": []any{
			map[string]any{"id": "439696", "name": "第2话", "sort": 2},
			map[string]any{"id": "439699", "name": "第1话", "sort": 1},
			map[string]any{"id": "439700", "sort": 0}, // sort 缺失 → 序号兜底 3
		},
		"update_at":   1700000000,
		"liked":       true,
		"is_favorite": false,
	})
	data, err := mapAlbumDetail(raw, "439695")
	if err != nil {
		t.Fatalf("mapAlbumDetail: %v", err)
	}
	d := data.(map[string]any)
	if d["aid"] != "439695" || d["title"] != "测试漫画" {
		t.Errorf("aid/title: %#v", d)
	}
	if d["author"] != "甲" {
		t.Errorf("author 应取数组首值: %#v", d["author"])
	}
	if d["description"] != "x\ny" {
		t.Errorf("description 应剥 HTML: %#v", d["description"])
	}
	if d["likes"] != 1000 || d["views"] != 40000 {
		t.Errorf("likes/views 计数解析: %#v/%#v", d["likes"], d["views"])
	}
	if d["imageCount"] != 0 {
		t.Errorf("imageCount 恒 0: %#v", d["imageCount"])
	}
	if d["updateAt"] != "2023-11-15" {
		t.Errorf("updateAt 应为东八区日期: %#v", d["updateAt"])
	}
	if d["liked"] != true || d["favorited"] != false {
		t.Errorf("liked/favorited 透传: %#v/%#v", d["liked"], d["favorited"])
	}
	cat := d["category"].(map[string]any)
	if cat["id"] != "0" || cat["name"] != "全部" || cat["sub"] != nil {
		t.Errorf("category 恒为 全部: %#v", cat)
	}
	eps := d["episodes"].([]any)
	if d["epCount"] != 3 || len(eps) != 3 {
		t.Fatalf("episodes 长度: %#v", eps)
	}
	e0 := eps[0].(map[string]any)
	if e0["pid"] != "439699" || e0["order"] != 1 {
		t.Errorf("episodes 应按 sort 排序: %#v", e0)
	}
	e2 := eps[2].(map[string]any)
	if e2["pid"] != "439700" || e2["order"] != 3 || e2["title"] != "第3话" {
		t.Errorf("sort 缺失按序号兜底且 title 兜底: %#v", e2)
	}
	if _, has := e0["imageCount"]; has {
		t.Errorf("episodes 不应含 imageCount 字段")
	}
}

func TestMapAlbumDetailDefaultEpisodeAndNotFound(t *testing.T) {
	// 无 series → 单默认章
	raw := contentFixtureJSON(t, map[string]any{"id": "123", "name": "单章本子", "addtime": "2024-01-02"})
	data, err := mapAlbumDetail(raw, "123")
	if err != nil {
		t.Fatalf("mapAlbumDetail: %v", err)
	}
	d := data.(map[string]any)
	eps := d["episodes"].([]any)
	if len(eps) != 1 {
		t.Fatalf("无 series 应生成单默认章: %#v", eps)
	}
	ep := eps[0].(map[string]any)
	if ep["pid"] != "123" || ep["title"] != "单章本子" || ep["order"] != 1 {
		t.Errorf("默认章字段: %#v", ep)
	}
	if d["updateAt"] != "2024-01-02" {
		t.Errorf("update_at 缺失应回退 addtime: %#v", d["updateAt"])
	}
	// author 缺失 → default_author
	if d["author"] != "default_author" {
		t.Errorf("author 缺失回退: %#v", d["author"])
	}
	// author 为字符串形态
	raw2 := contentFixtureJSON(t, map[string]any{"id": "124", "name": "x", "author": " solo "})
	d2, err := mapAlbumDetail(raw2, "124")
	if err != nil {
		t.Fatalf("mapAlbumDetail: %v", err)
	}
	if got := d2.(map[string]any)["author"]; got != " solo " {
		t.Errorf("author 字符串应原样: %#v", got)
	}
	// name 为空 → 3001
	raw3 := contentFixtureJSON(t, map[string]any{"id": "125"})
	_, err = mapAlbumDetail(raw3, "125")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != CodeNotFound {
		t.Fatalf("name 为空应返回 3001,得到 %v", err)
	}
}

func TestMapChapterPhotosContract(t *testing.T) {
	chapter := contentFixtureJSON(t, map[string]any{
		"id": "439699", "album_id": "439695", "name": "第1话",
		"scramble_id": 220980,
		"images":      []any{"00001.webp", "00002.jpg"},
	})
	album := contentFixtureJSON(t, map[string]any{"series": []any{
		map[string]any{"id": "439699", "name": "第1话", "sort": 1},
		map[string]any{"id": "439700", "name": "第2话", "sort": 2},
	}})
	albumMap, _ := decodeMap(album)
	chapterMap, _ := decodeMap(chapter)
	d := mapChapterPhotos(chapterMap, albumMap)

	if d["pid"] != "439699" || d["aid"] != "439695" || d["title"] != "第1话" {
		t.Errorf("pid/aid/title: %#v", d)
	}
	if d["epIndex"] != 0 {
		t.Errorf("第一章 epIndex 应为 0: %#v", d["epIndex"])
	}
	if d["scramble"] != "220980" {
		t.Errorf("scramble 透传: %#v", d["scramble"])
	}
	if d["hasNext"] != true || d["nextPid"] != "439700" {
		t.Errorf("hasNext/nextPid 由专辑 series 顺序判定: %#v/%#v", d["hasNext"], d["nextPid"])
	}
	images := d["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("images 长度: %d", len(images))
	}
	img := images[0].(map[string]any)
	if img["index"] != 1 || img["path"] != "media/photos/439699/00001.webp" ||
		img["width"] != 0 || img["height"] != 0 {
		t.Errorf("images 拼装: %#v", img)
	}

	// 最后一章:hasNext=false,nextPid=null,epIndex=1
	chapter2 := map[string]any{"id": "439700", "name": "第2话"}
	d2 := mapChapterPhotos(chapter2, albumMap)
	if d2["epIndex"] != 1 || d2["hasNext"] != false || d2["nextPid"] != nil {
		t.Errorf("末章判定: %#v/%#v/%#v", d2["epIndex"], d2["hasNext"], d2["nextPid"])
	}

	// 单章本子:无 series,sort=2 → 视为 1(epIndex 0);scramble 缺省 → SDK 默认 220980
	chapter3 := map[string]any{"id": "500", "album_id": "0", "series_id": "0", "name": "单章", "sort": 2}
	d3 := mapChapterPhotos(chapter3, nil)
	if d3["aid"] != "500" || d3["epIndex"] != 0 || d3["hasNext"] != false ||
		d3["scramble"] != "220980" || d3["nextPid"] != nil {
		t.Errorf("单章本子: %#v", d3)
	}

	// images 为 JSON 字符串编码 + 缺 scramble
	chapter4 := map[string]any{"id": "600", "name": "x", "images": "[\"a.jpg\"]"}
	d4 := mapChapterPhotos(chapter4, nil)
	if imgs := d4["images"].([]any); len(imgs) != 1 ||
		imgs[0].(map[string]any)["path"] != "media/photos/600/a.jpg" {
		t.Errorf("images JSON 字符串兼容: %#v", d4["images"])
	}
}

func TestBookListMapping(t *testing.T) {
	books := []any{
		map[string]any{
			"id": "123", "name": "书名", "author": "作者",
			"image":        "https://up.example.com/media/albums/123_3x4.jpg?u=9",
			"category":     map[string]any{"id": "1", "title": "同人"},
			"category_sub": map[string]any{"title": "中文"},
			"tags":         []any{"t1"},
			"likes":        "12", "views": "40K", "images_count": "5",
			"adddate": "2025-10-04",
		},
		"非dict跳过",
		map[string]any{"id": "456", "name": "无封面"},
	}
	items := bookList(books)
	if len(items) != 2 {
		t.Fatalf("非 dict 项应跳过: %d", len(items))
	}
	first := items[0].(map[string]any)
	// 绝对 URL 只留路径、剥 ?query、反斜杠归一
	wantCover := imageProxyURL("media/albums/123_3x4.jpg", "0", "123")
	if first["coverUrl"] != wantCover {
		t.Errorf("coverUrl 应剥 query 并代理化: %#v", first["coverUrl"])
	}
	if first["category"] != "同人" || first["categorySub"] != "中文" {
		t.Errorf("category/categorySub: %#v/%#v", first["category"], first["categorySub"])
	}
	if first["likes"] != 12 || first["views"] != 40000 || first["imageCount"] != 5 {
		t.Errorf("计数解析: %#v/%#v/%#v", first["likes"], first["views"], first["imageCount"])
	}
	if first["updateAt"] != "2025-10-04" {
		t.Errorf("updateAt 优先 adddate: %#v", first["updateAt"])
	}
	second := items[1].(map[string]any)
	// 无 image → coverPath 回退;tags/计数缺失 → []/0
	if second["coverUrl"] != coverURL("456") {
		t.Errorf("无 image 应回退默认封面: %#v", second["coverUrl"])
	}
	if tags := second["tags"].([]any); len(tags) != 0 {
		t.Errorf("tags 缺失应为空数组: %#v", second["tags"])
	}
	if second["likes"] != 0 || second["updateAt"] != "" {
		t.Errorf("计数/时间缺失口径: %#v/%#v", second["likes"], second["updateAt"])
	}
}

// ── albumAuthors:上游 author(string/数组)→ 归一作者名数组 ──

func TestAlbumAuthors(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want []string
	}{
		{"数组形态(1475046 实测)", []any{"N/A"}, []string{"N/A"}},
		{"多作者去重", []any{" A ", "B", "A", "", " B "}, []string{"A", "B"}},
		{"字符串形态", "某作者", []string{"某作者"}},
		{"空白字符串", "   ", []string{}},
		{"缺失", nil, []string{}},
		{"数值作者名", float64(123), []string{"123"}},
	}
	for _, c := range cases {
		got := albumAuthors(c.raw)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}
