package jm

// JM 内容端点上游调用封装(MOBILE_API.md #6-#15)。
// 口径逐条对齐 mobile/server/core/live.py 与 jmcomic SDK(jm_client_impl.py:597-1100):
// GET 参数拼 URL query(SDK append_params_to_url 语义),reqAPI 负责 token 头/
// 域名轮换/AES 解密与外层 code!=200 → *APIError(2001)。
//
// 每个导出方法对应一个契约端点,返回映射好的契约结构(handler 直接 jmOK 下发)。
// 资源约束:每请求 1 次上游调用(详情/章节/搜车号伴随 /album 时为 2 次),
// 无并发批量拉取,不缓存上游 JSON。

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// 上游排序/筛选常量(jmcomic/jm_config.py JmMagicConstants;orderByLatest 已在
// api_account.go 定义)。
const (
	orderByView         = "mv"
	orderByMonthRanking = "mv_m"
	orderByWeekRanking  = "mv_w"
	orderByDayRanking   = "mv_t"
	orderByPicture      = "mp"
	orderByLike         = "tf"
	timeAll             = "a"  // TIME_ALL
	categoryAll         = "0"  // CATEGORY_ALL(全部分类)
	category3D          = "3D" // CATEGORY_3D(大小写敏感)
)

// 页大小与页码上限(live.py 模块常量)。
const (
	pageSizeSearch        = 80  // comics/index、comics/search(含 /categories/filter 路径)
	pageSizeLatest        = 80  // comics/latest(上游 0 基页码,无 total)
	pageSizeSerialization = 40  // comics/serialization
	jmListMaxPage         = 120 // 上游页码封顶:第 121 页起返回与第 120 页相同的冻结数据
)

// promoteFilterTypes 首页剔除的区块类型(桌面端 index_view.filterTypes:小说/书库)。
var promoteFilterTypes = map[string]bool{"novels": true, "library": true}

// categoryRankingSorts 月/周/日排行:仅上游 /categories/filter 支持(/search 不支持)。
var categoryRankingSorts = map[string]bool{
	orderByMonthRanking: true,
	orderByWeekRanking:  true,
	orderByDayRanking:   true,
}

// SearchParams 契约 #13 搜索参数(handler 完成枚举/范围校验后传入)。
type SearchParams struct {
	Keyword      string
	Page         int
	Sort         string // ""|mr|mv|mv_m|mv_w|mv_t|mp|tf
	SearchType   string // ""|site|work|author|tag|character(site 等价缺省全站)
	Year         int    // 1..9999;0=不过滤
	Month        int    // 1..12;0=不过滤;仅与 Year 同传
	MainCategory string // 原始串(数值 id 或 slug),归一化见 mainCategorySlug
}

/* ── #13 搜索参数归一化 ── */

// mainCategorySlug 契约 #13 mainCategory 归一化(live.py _main_category_slug,
// 与 mockdata.main_category_slug 共用映射):
// 数值 id 1..8 → doujin/single/short/another/hanman/meiman/doujin_cosplay/3d;
// "0"/空/"all" → ""(不过滤);'3d' 修正为上游大小写 '3D'(SDK CATEGORY_3D);
// 'doujin_cosplay'(桌面端历史 slug,上游已与 doujin 同返回)→ 'another_cosplay'
// (2026-09 实测当前 Cosplay 真实 slug);其余(含子分类 slug)小写后原样透传。
func mainCategorySlug(mainCategory string) string {
	slug := strings.ToLower(strings.TrimSpace(mainCategory))
	if slug == "" || slug == "0" || slug == "all" {
		return ""
	}
	switch slug {
	case "1":
		slug = "doujin"
	case "2":
		slug = "single"
	case "3":
		slug = "short"
	case "4":
		slug = "another"
	case "5":
		slug = "hanman"
	case "6":
		slug = "meiman"
	case "7":
		slug = "doujin_cosplay"
	case "8":
		slug = "3d"
	}
	if slug == "3d" {
		return category3D
	}
	if slug == "doujin_cosplay" {
		return "another_cosplay"
	}
	return slug
}

// searchOrderBy 契约 #13/#7 sort → 上游 o(SDK JmMagicConstants):
// mr/mv/mv_m/mv_w/mv_t/mp/tf 原样;未知/"" → 最新 mr。
func searchOrderBy(sort string) string {
	switch sort {
	case orderByLatest, orderByView, orderByMonthRanking, orderByWeekRanking,
		orderByDayRanking, orderByPicture, orderByLike:
		return sort
	default:
		return orderByLatest
	}
}

/* ── #6 分类树 ── */

// CategoriesList #6 分类树:GET /categories?lang=CN。
// 顶层键 id → str,子分类主键 CID(部分上游版本兼容 id)→ str。
func (c *Client) CategoriesList(ctx context.Context) (any, error) {
	data, err := reqGETParams(ctx, c, "/categories", url.Values{"lang": {"CN"}})
	if err != nil {
		return nil, err
	}
	return mapCategories(data)
}

/* ── #7 首页/全部漫画列表 ── */

// ComicsIndex #7:GET /categories/filter(SDK categories_filter 生成 query:
// page、order=空串、c=0、o=mr|mv,无 t 键;1 基页码);页大小 80;
// hasNext 受 max_page=120;契约不返回 total。
func (c *Client) ComicsIndex(ctx context.Context, page int, order string) (any, error) {
	o := orderByLatest
	if order == "View" {
		o = orderByView
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("order", "") // SDK 固定空串
	q.Set("c", categoryAll)
	q.Set("o", o)
	data, err := reqGETParams(ctx, c, "/categories/filter", q)
	if err != nil {
		return nil, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, errUpstream("获取列表失败", truncate(string(data), 300))
	}
	items := bookList(raw["content"])
	total := parseCount(raw["total"])
	return map[string]any{
		"page":    page,
		"hasNext": hasNextMaxPage(page, total, len(items), pageSizeSearch, jmListMaxPage),
		"list":    items,
	}, nil
}

/* ── #8 首页推荐区块 ── */

// ComicsPromote #8:GET /promote?page=0&lang=CN。data 顶层为 JSON 数组(超出
// SDK model_data 的 dict 约束,直接解密后解析);过滤 type in (novels, library);
// 首个保留区块固定 kind="serialization" 无 list;其余从 content 书单映射。
func (c *Client) ComicsPromote(ctx context.Context) (any, error) {
	data, err := reqGETParams(ctx, c, "/promote", url.Values{"page": {"0"}, "lang": {"CN"}})
	if err != nil {
		return nil, err
	}
	return mapPromote(data)
}

/* ── #9 最近更新 ── */

// ComicsLatest #9:GET /latest?page=<page-1>&lang=CN(上游 0 基页码)。
// data 为纯书籍数组、无 total,hasNext 按"本页满 80 条"判定。
func (c *Client) ComicsLatest(ctx context.Context, page int) (any, error) {
	data, err := reqGETParams(ctx, c, "/latest", url.Values{
		"page": {strconv.Itoa(page - 1)},
		"lang": {"CN"},
	})
	if err != nil {
		return nil, err
	}
	items, err := mapLatestBooks(data)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"page":    page,
		"hasNext": hasNextMaxPage(page, nil, len(items), pageSizeLatest, 0),
		"list":    items,
	}, nil
}

/* ── #10 每周连载 ── */

// ComicsSerialization #10:GET /serialization?type&date=<day>&page&lang=CN
// (day 1=周一..7=周日、type all|manga|hanman 原样透传;上游 1 基页码,
// 页大小 40,data={list,total},hasNext 按 total 判定,无 max_page)。
func (c *Client) ComicsSerialization(ctx context.Context, day int, serialType string, page int) (any, error) {
	data, err := reqGETParams(ctx, c, "/serialization", url.Values{
		"type": {serialType},
		"date": {strconv.Itoa(day)},
		"page": {strconv.Itoa(page)},
		"lang": {"CN"},
	})
	if err != nil {
		return nil, err
	}
	raw, err := decodeLooseDict(data)
	if err != nil {
		return nil, err
	}
	items := bookList(raw["list"])
	return map[string]any{
		"page":    page,
		"day":     day,
		"type":    serialType,
		"hasNext": hasNextMaxPage(page, parseCount(raw["total"]), len(items), pageSizeSerialization, 0),
		"list":    items,
	}, nil
}

/* ── #11/#12 每周必看 ── */

// ComicsWeek #11 每周必看期数:GET /week?page=0&lang=CN。
// 契约 title 取上游 time 字段(fallback title)。
func (c *Client) ComicsWeek(ctx context.Context) (any, error) {
	data, err := reqGETParams(ctx, c, "/week", url.Values{"page": {"0"}, "lang": {"CN"}})
	if err != nil {
		return nil, err
	}
	return mapWeekCategories(data)
}

// ComicsWeekFilter #12 每周必看单期列表:GET /week/filter?page=0&id&type&lang=CN
// (固定 page=0 对齐桌面端;data={total,list},仅回传 list,无翻页语义)。
func (c *Client) ComicsWeekFilter(ctx context.Context, weekID, weekType string) (any, error) {
	data, err := reqGETParams(ctx, c, "/week/filter", url.Values{
		"page": {"0"},
		"id":   {weekID},
		"type": {weekType},
		"lang": {"CN"},
	})
	if err != nil {
		return nil, err
	}
	raw, err := decodeLooseDict(data)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": bookList(raw["list"])}, nil
}

/* ── #13 搜索/分类/排行 ── */

// ComicsSearch #13 路由规则(live.py search 直译):
//   - mainCategory 归一化后有效、或 sort ∈ {mv_m,mv_w,mv_t}、或 keyword 为空/全空白
//     → 上游 /categories/filter(page、o;c 仅在分类有效时携带,"0"=全部不传;
//     对齐桌面端 GetSearchCategoryReq2:该端点忽略 search_query);
//   - 否则 → 上游 /search(search_query、page、o;searchType≠site 附加 search_type;
//     y>0 附加 y;m 仅与 y 同传附加):
//     无高级过滤走 SDK search_site 原路径(main_tag=0、t=a,保留搜车号 redirect_aid
//     → 单详情页包装);高级过滤响应含 redirect_aid 时也回退该路径;
//   - o 排序映射见 searchOrderBy;两条路径均回传 total,页大小 80,hasNext 受
//     max_page=120。
func (c *Client) ComicsSearch(ctx context.Context, p SearchParams) (any, error) {
	order := searchOrderBy(p.Sort)
	slug := mainCategorySlug(p.MainCategory)
	if slug != "" || categoryRankingSorts[p.Sort] || strings.TrimSpace(p.Keyword) == "" {
		// 对齐桌面端:分类/排行页无关键词一律走 /categories/filter,
		// 否则"全部分类+mr/mv/mp/tf"会落入空关键词 /search 导致无数据
		items, total, err := c.searchByFilter(ctx, p.Page, order, slug)
		if err != nil {
			return nil, err
		}
		return searchResult(p.Page, total, items), nil
	}
	items, total, err := c.searchByKeyword(ctx, p, order)
	if err != nil {
		return nil, err
	}
	return searchResult(p.Page, total, items), nil
}

// searchResult 契约 #13 响应体(page/total/hasNext/list,页大小 80,max_page=120)。
func searchResult(page, total int, items []any) map[string]any {
	return map[string]any{
		"page":    page,
		"total":   total,
		"hasNext": hasNextMaxPage(page, total, len(items), pageSizeSearch, jmListMaxPage),
		"list":    items,
	}
}

// searchByFilter 分类/排行路径:GET /categories/filter(page、o、c?;c 仅在
// 分类 slug 有效时携带)。
func (c *Client) searchByFilter(ctx context.Context, page int, order, slug string) ([]any, int, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("o", order)
	if slug != "" {
		q.Set("c", slug)
	}
	data, err := reqGETParams(ctx, c, "/categories/filter", q)
	if err != nil {
		return nil, 0, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, 0, errUpstream("搜索响应解析失败", truncate(string(data), 300))
	}
	return bookList(raw["content"]), parseCount(raw["total"]), nil
}

// searchByKeyword 关键字路径:无高级过滤走 search_site 原路径(SDK:main_tag=0、
// t=TIME_ALL,search 响应含 redirect_aid → 单详情页包装);有高级过滤走
// /search 精简参数(SDK search_site 不携带的 search_type/y/m),响应含
// redirect_aid 时回退 search_site 路径。
func (c *Client) searchByKeyword(ctx context.Context, p SearchParams, order string) ([]any, int, error) {
	hasAdvanced := (p.SearchType != "" && p.SearchType != "site") || p.Year > 0
	if !hasAdvanced {
		return c.searchBySite(ctx, p.Keyword, p.Page, order)
	}
	q := url.Values{}
	q.Set("search_query", p.Keyword)
	q.Set("page", strconv.Itoa(p.Page))
	q.Set("o", order)
	if p.SearchType != "" && p.SearchType != "site" {
		q.Set("search_type", p.SearchType)
	}
	if p.Year > 0 {
		q.Set("y", strconv.Itoa(p.Year))
		// m 仅与 y 同传(单独传或非法值被上游忽略)
		if p.Month > 0 && p.Month <= 12 {
			q.Set("m", strconv.Itoa(p.Month))
		}
	}
	data, err := reqGETParams(ctx, c, "/search", q)
	if err != nil {
		return nil, 0, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, 0, errUpstream("搜索响应解析失败", truncate(string(data), 300))
	}
	if aid := redirectAidOf(raw); aid != "" {
		// 高级过滤下搜中车号:与 SDK search() 一致,回退普通搜索取单详情页
		return c.searchBySite(ctx, p.Keyword, p.Page, order)
	}
	return bookList(raw["content"]), parseCount(raw["total"]), nil
}

// searchBySite SDK search_site 原路径:GET /search?main_tag=0&search_query&page&o&t=a;
// 响应含 redirect_aid(搜车号)→ 伴随 GET /album 做单详情页包装(total=1)。
func (c *Client) searchBySite(ctx context.Context, keyword string, page int, order string) ([]any, int, error) {
	q := url.Values{}
	q.Set("main_tag", "0")
	q.Set("search_query", keyword)
	q.Set("page", strconv.Itoa(page))
	q.Set("o", order)
	q.Set("t", timeAll)
	data, err := reqGETParams(ctx, c, "/search", q)
	if err != nil {
		return nil, 0, err
	}
	raw, ok := decodeMap(data)
	if !ok {
		return nil, 0, errUpstream("搜索响应解析失败", truncate(string(data), 300))
	}
	if aid := redirectAidOf(raw); aid != "" {
		return c.wrapSingleAlbum(ctx, aid)
	}
	return bookList(raw["content"]), parseCount(raw["total"]), nil
}

// wrapSingleAlbum 搜中车号单详情页包装(SDK JmSearchPage.wrap_single_album):
// content=[(aid, {name, tags})],total=1 → 页不满页 hasNext 恒 false。
func (c *Client) wrapSingleAlbum(ctx context.Context, aid string) ([]any, int, error) {
	data, err := c.fetchAlbum(ctx, aid)
	if err != nil {
		return nil, 0, err
	}
	raw, ok := decodeMap(data)
	if !ok || fieldStr(raw, "name") == "" {
		// SDK fetch_detail_entity:缺 name → raise_missing → 3001
		return nil, 0, errNotFound(fmt.Sprintf("漫画不存在: %s", aid))
	}
	item := ComicItemFromUpstream(aid, map[string]any{"name": raw["name"], "tags": raw["tags"]})
	return []any{item}, 1, nil
}

// redirectAidOf /search 响应的搜车号重定向字段(缺失/空 → "")。
func redirectAidOf(raw map[string]any) string {
	if v, ok := raw["redirect_aid"]; ok && v != nil {
		if s := stringify(v); s != "" {
			return s
		}
	}
	return ""
}

/* ── #14 详情 / #15 章节 ── */

// fetchAlbum GET /album?id={aid}。
func (c *Client) fetchAlbum(ctx context.Context, aid string) ([]byte, error) {
	q := url.Values{}
	q.Set("id", aid)
	return reqGETParams(ctx, c, "/album", q)
}

// fetchChapter GET /chapter?id={pid}。
func (c *Client) fetchChapter(ctx context.Context, pid string) ([]byte, error) {
	q := url.Values{}
	q.Set("id", pid)
	return reqGETParams(ctx, c, "/chapter", q)
}

// ComicDetail #14 漫画详情:GET /album?id={aid}(上游 name 为空 → 3001)。
func (c *Client) ComicDetail(ctx context.Context, aid string) (any, error) {
	data, err := c.fetchAlbum(ctx, aid)
	if err != nil {
		return nil, err
	}
	return mapAlbumDetail(data, aid)
}

// PhotoDetail #15 章节:GET /chapter?id={pid} 拿图片数组与 scramble_id
// (上游 /chapter 已带 scramble_id,chapter_view_template 可跳过),伴随
// GET /album?id={album_id} 拿章节顺序(hasNext/nextPid/epIndex 来源)。
func (c *Client) PhotoDetail(ctx context.Context, pid string) (any, error) {
	chapterData, err := c.fetchChapter(ctx, pid)
	if err != nil {
		return nil, err
	}
	chapter, ok := decodeMap(chapterData)
	if !ok {
		return nil, errUpstream("章节响应解析失败", truncate(string(chapterData), 300))
	}
	if fieldStr(chapter, "name") == "" {
		// SDK fetch_detail_entity:缺 name → raise_missing → 3001
		return nil, errNotFound(fmt.Sprintf("章节不存在: %s", pid))
	}
	albumData, err := c.fetchAlbum(ctx, chapterAlbumID(chapter, pid))
	if err != nil {
		return nil, err
	}
	album, _ := decodeMap(albumData) // 解析失败按无章节序列处理(hasNext=false)
	return mapChapterPhotos(chapter, album), nil
}
