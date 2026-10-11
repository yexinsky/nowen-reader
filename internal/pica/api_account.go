package pica

// 哔咔账号互动端点:收藏列表/收藏切换/爱心切换(API 文档 §3.2、§4.2)。

import (
	"context"
	"net/url"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

// urlValues/urlPathEscape 局部简写(与 api_content.go 口径一致)。
func urlValues() url.Values { return url.Values{} }

func urlPathEscape(s string) string { return url.PathEscape(s) }

// FavouriteList 收藏列表(GET users/favourite?s&page):data.comics。
// s ∈ dd(新→旧)/da(旧→新),应用默认 da。
func (c *Client) FavouriteList(ctx context.Context, token, sort string, page int) (any, error) {
	q := urlValues()
	q.Set("page", itoa(page))
	q.Set("s", sort)
	data, err := c.reqAPI(ctx, "GET", "users/favourite?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	env, ok := decodePage(data, "comics")
	if !ok {
		return nil, errUpstream("收藏列表响应缺少 comics", marshalForLog(data, 300))
	}
	return listPage(page, env, comicItemsFromUpstream(env.Docs)), nil
}

// ToggleFavourite 收藏/取消收藏(POST comics/{id}/favourite,切换式):
// data.action = "favourite"(已收藏)/ "un_favourite"(已取消)。
// 收藏已满返回上游 1017 → 2001「收藏已满」。
func (c *Client) ToggleFavourite(ctx context.Context, token, id string) (map[string]any, error) {
	data, err := c.reqAPI(ctx, "POST", "comics/"+urlPathEscape(id)+"/favourite", token, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"action": fieldStrOr(data, "action", "favourite"),
	}, nil
}

// ToggleLike 爱心点赞(POST comics/{id}/like,切换式):
// data.action = "like"/"unlike"。
func (c *Client) ToggleLike(ctx context.Context, token, id string) (map[string]any, error) {
	data, err := c.reqAPI(ctx, "POST", "comics/"+urlPathEscape(id)+"/like", token, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"action": fieldStrOr(data, "action", "like"),
	}, nil
}
