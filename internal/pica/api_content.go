package pica

// 哔咔内容端点上游封装(API 文档 §4.2):分类/浏览/搜索/热词/随机/详情/章节/图片列表/
// 排行榜/神魔推荐/相关推荐/评论。
//
// 关键口径:
//   - GET 参数拼 URL query,签名 path 含 query 串(reqAPI 统一处理);
//   - 分类浏览 c=分类名(中文,URL 编码);s ∈ dd(新→旧)/da(旧→新)/ld(爱心)/vd(观看);
//   - order/{n}/pages 的 n 是章节序号 order(非 _id,用 _id 上游 500);
//   - 分页统一 docs/total/pages/limit/page,hasNext 由 page<pages 判定。

import (
	"context"
	"net/url"
	"sort"
	"strconv"
)

// Categories 分类列表(GET categories):data.categories。
// 实测为 JSON 数组(非 docs 分页容器);兼容对象 {docs:[...]} 形态。
func (c *Client) Categories(ctx context.Context, token string) (any, error) {
	data, err := c.reqAPI(ctx, "GET", "categories", token, nil)
	if err != nil {
		return nil, err
	}
	docs := fieldArray(data, "categories")
	if docs == nil {
		if cats := fieldMap(data, "categories"); cats != nil {
			docs = fieldArray(cats, "docs")
		}
	}
	if docs == nil {
		return nil, errUpstream("分类响应缺少 categories", marshalForLog(data, 300))
	}
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"title":       fieldStr(m, "title"),
			"description": fieldStr(m, "description"),
			"coverUrl":    thumbProxyURL(m),
			"isWeb":       boolOf(m["isWeb"]),
			"active":      boolOf(m["active"]),
			"link":        fieldStr(m, "link"),
		})
	}
	return map[string]any{"list": out, "total": len(out)}, nil
}

// ComicsByCategory 分类浏览(GET comics?page&c&s):data.comics。
// c 为分类名(URL 编码);s ∈ dd|da|ld|vd(默认 dd)。
func (c *Client) ComicsByCategory(ctx context.Context, token, category, sort string, page int) (any, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("c", category)
	q.Set("s", sort)
	data, err := c.reqAPI(ctx, "GET", "comics?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	env, ok := decodePage(data, "comics")
	if !ok {
		return nil, errUpstream("分类列表响应缺少 comics", marshalForLog(data, 300))
	}
	return listPage(page, env, comicItemsFromUpstream(env.Docs)), nil
}

// SearchParams 高级搜索入参(handler 完成校验后传入)。
type SearchParams struct {
	Keyword    string
	Categories []string // 分类过滤(分类名数组,空 = 全部)
	Sort       string   // dd|da|ld|vd,空 = dd
}

// AdvancedSearch 高级搜索(POST comics/advanced-search?page=):
// body {categories:[], keyword, sort};data.comics。
func (c *Client) AdvancedSearch(ctx context.Context, token string, p SearchParams, page int) (any, error) {
	if p.Sort == "" {
		p.Sort = "dd"
	}
	body := map[string]any{
		"categories": p.Categories,
		"keyword":    p.Keyword,
		"sort":       p.Sort,
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	data, err := c.reqAPI(ctx, "POST", "comics/advanced-search?"+q.Encode(), token, body)
	if err != nil {
		return nil, err
	}
	env, ok := decodePage(data, "comics")
	if !ok {
		return nil, errUpstream("搜索响应缺少 comics", marshalForLog(data, 300))
	}
	return listPage(page, env, comicItemsFromUpstream(env.Docs)), nil
}

// Keywords 搜索热词(GET keywords):data.keywords。
func (c *Client) Keywords(ctx context.Context, token string) (any, error) {
	data, err := c.reqAPI(ctx, "GET", "keywords", token, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": stringArray(fieldArray(data, "keywords"))}, nil
}

// Random 随机本子(GET comics/random):data.comics 为数组(非分页容器)。
func (c *Client) Random(ctx context.Context, token string) (any, error) {
	data, err := c.reqAPI(ctx, "GET", "comics/random", token, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": comicItemsFromUpstream(fieldArray(data, "comics"))}, nil
}

// Leaderboard 排行榜(GET comics/leaderboard?tt=&ct=VC):data.comics 数组(20 条)。
// tt ∈ H24(日榜)/D7(周榜)/D30(月榜)。
func (c *Client) Leaderboard(ctx context.Context, token, tt string) (any, error) {
	q := url.Values{}
	q.Set("tt", tt)
	q.Set("ct", "VC")
	data, err := c.reqAPI(ctx, "GET", "comics/leaderboard?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": comicItemsFromUpstream(fieldArray(data, "comics"))}, nil
}

// Collections 神魔推荐(GET collections):data.collections 数组,
// 每项 {title, description, thumb, comics:{docs,...}}。
func (c *Client) Collections(ctx context.Context, token string) (any, error) {
	data, err := c.reqAPI(ctx, "GET", "collections", token, nil)
	if err != nil {
		return nil, err
	}
	raws := fieldArray(data, "collections")
	out := make([]any, 0, len(raws))
	for _, r := range raws {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		section := map[string]any{
			"title":       fieldStr(m, "title"),
			"description": fieldStr(m, "description"),
			"thumbUrl":    thumbProxyURL(m),
		}
		if comics, ok := m["comics"].(map[string]any); ok {
			docs := fieldArray(comics, "docs")
			section["list"] = comicItemsFromUpstream(docs)
		} else {
			section["list"] = []any{}
		}
		out = append(out, section)
	}
	return map[string]any{"list": out}, nil
}

// ComicDetail 漫画详情(GET comics/{id}):data.comic。
func (c *Client) ComicDetail(ctx context.Context, token, id string) (any, error) {
	data, err := c.reqAPI(ctx, "GET", "comics/"+url.PathEscape(id), token, nil)
	if err != nil {
		return nil, err
	}
	comic := fieldMap(data, "comic")
	if comic == nil {
		return nil, errNotFound("漫画不存在: " + id)
	}
	out := comicDetailFromUpstream(comic)
	// 详情响应的 likesCount 为点赞数,liked 表达当前用户是否已点
	out["liked"] = boolOf(comic["likes"]) // likes 为布尔(是否已点爱心)
	out["likes"] = parseCount(comic["likesCount"])
	if out["likes"].(int) == 0 {
		out["likes"] = parseCount(comic["totalLikes"])
	}
	out["commentsCount"] = parseCount(comic["commentsCount"])
	out["isFavourite"] = boolOf(comic["isFavourite"])
	return out, nil
}

// Episode 章节条目(契约)。
type Episode struct {
	Order     int    `json:"order"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
}

// Eps 章节列表(GET comics/{id}/eps?page=):data.eps。
// 上游 docs 排列与 order 相反,按 order 升序重排(picacg-qt 同款)。
func (c *Client) Eps(ctx context.Context, token, id string, page int) (any, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	data, err := c.reqAPI(ctx, "GET", "comics/"+url.PathEscape(id)+"/eps?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	env, ok := decodePage(data, "eps")
	if !ok {
		return nil, errUpstream("章节响应缺少 eps", marshalForLog(data, 300))
	}
	episodes := make([]Episode, 0, len(env.Docs))
	for _, d := range env.Docs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		episodes = append(episodes, Episode{
			Order:     parseCount(m["order"]),
			ID:        fieldStr(m, "_id"),
			Title:     fieldStr(m, "title"),
			UpdatedAt: fieldStr(m, "updated_at"),
		})
	}
	sort.SliceStable(episodes, func(i, j int) bool { return episodes[i].Order < episodes[j].Order })
	list := make([]any, 0, len(episodes))
	for _, e := range episodes {
		list = append(list, map[string]any{
			"order": e.Order, "id": e.ID, "title": e.Title, "updatedAt": e.UpdatedAt,
		})
	}
	return listPage(page, env, list), nil
}

// PageImage 单页图片(契约):站内代理地址 + 上游原始引用(下载器直用,免二次解析)。
type PageImage struct {
	URL        string `json:"url"`
	FileServer string `json:"fileServer"`
	Path       string `json:"path"`
}

// EpsPages 章节图片列表(GET comics/{id}/order/{order}/pages?page=):data.pages。
// ⚠️ order 为章节序号(eps 文档的 order 字段,从 1 开始),非 _id。
func (c *Client) EpsPages(ctx context.Context, token, id string, order, page int) (any, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	path := "comics/" + url.PathEscape(id) + "/order/" + strconv.Itoa(order) + "/pages?" + q.Encode()
	data, err := c.reqAPI(ctx, "GET", path, token, nil)
	if err != nil {
		return nil, err
	}
	env, ok := decodePage(data, "pages")
	if !ok {
		return nil, errUpstream("图片列表响应缺少 pages", marshalForLog(data, 300))
	}
	images := make([]PageImage, 0, len(env.Docs))
	for _, d := range env.Docs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		media := fieldMap(m, "media")
		if media == nil {
			continue
		}
		fs := fieldStr(media, "fileServer")
		path := fieldStr(media, "path")
		if fs == "" || path == "" {
			continue
		}
		images = append(images, PageImage{URL: imageProxyURL(fs, path), FileServer: fs, Path: path})
	}
	list := make([]any, 0, len(images))
	for _, img := range images {
		list = append(list, img)
	}
	return listPage(page, env, list), nil
}

// Comments 评论列表(GET comics/{id}/comments?page=):data.comments.docs,
// 每项 {_id, content, user:{...}, likesCount, totalComments, created_at}。
// 只读(发表评论需等级 ≥2,v1 不做)。
func (c *Client) Comments(ctx context.Context, token, id string, page int) (any, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	data, err := c.reqAPI(ctx, "GET", "comics/"+url.PathEscape(id)+"/comments?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	env, ok := decodePage(data, "comments")
	if !ok {
		return nil, errUpstream("评论响应缺少 comments", marshalForLog(data, 300))
	}
	list := make([]any, 0, len(env.Docs))
	for _, d := range env.Docs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		user := fieldMap(m, "user")
		avatar := ""
		userName := ""
		if user != nil {
			userName = fieldStr(user, "name")
			if a := fieldMap(user, "avatar"); a != nil {
				avatar = imageProxyURL(fieldStr(a, "fileServer"), fieldStr(a, "path"))
			}
		}
		list = append(list, map[string]any{
			"id":        fieldStr(m, "_id"),
			"content":   fieldStr(m, "content"),
			"userName":  userName,
			"avatarUrl": avatar,
			"likes":     parseCount(m["likesCount"]),
			"createdAt": fieldStr(m, "created_at"),
		})
	}
	return listPage(page, env, list), nil
}

// Recommendation 相关推荐(GET comics/{id}/recommendation):data.comics 数组。
func (c *Client) Recommendation(ctx context.Context, token, id string) (any, error) {
	data, err := c.reqAPI(ctx, "GET", "comics/"+url.PathEscape(id)+"/recommendation", token, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": comicItemsFromUpstream(fieldArray(data, "comics"))}, nil
}
