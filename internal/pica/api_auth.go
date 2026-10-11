package pica

// 哔咔认证与账户端点(API 文档 §4.1)。

import (
	"context"
)

// SignIn 登录(POST auth/sign-in):返回上游 JWT 与用户资料。
// 上游签名机制下,错误密码本应返回 1004;若被指纹风控投喂假响应,reqAPI 已显式报错。
func (c *Client) SignIn(ctx context.Context, email, password string) (token string, userInfo map[string]any, err error) {
	data, err := c.reqAPI(ctx, "POST", "auth/sign-in", "", map[string]any{
		"email": email, "password": password,
	})
	if err != nil {
		return "", nil, err
	}
	token = fieldStr(data, "token")
	if token == "" {
		return "", nil, errUpstream("登录响应缺少 token", marshalForLog(data, 300))
	}
	user := profileFromData(data)
	if user == nil {
		// 登录响应未带用户资料时实时拉取(失败不影响登录结果)
		if u, perr := c.Profile(ctx, token); perr == nil {
			user = u
		}
	}
	return token, user, nil
}

// Profile 用户资料(GET users/profile):data.user。
func (c *Client) Profile(ctx context.Context, token string) (map[string]any, error) {
	data, err := c.reqAPI(ctx, "GET", "users/profile", token, nil)
	if err != nil {
		return nil, err
	}
	user := profileFromData(data)
	if user == nil {
		return nil, errUpstream("用户资料响应缺少 user 字段", marshalForLog(data, 300))
	}
	return user, nil
}

// profileFromData 从 data 中提取用户资料(兼容 data.user 与 data 直挂两种形态)。
func profileFromData(data map[string]any) map[string]any {
	if u := fieldMap(data, "user"); u != nil {
		return userMap(u)
	}
	if fieldStr(data, "name") != "" || fieldStr(data, "email") != "" {
		return userMap(data)
	}
	return nil
}

// userMap 上游用户对象 → 契约 UserInfo(字段口径对齐 picacg-qt tools/user.py)。
func userMap(u map[string]any) map[string]any {
	avatar := ""
	if a := fieldMap(u, "avatar"); a != nil {
		avatar = imageProxyURL(fieldStr(a, "fileServer"), fieldStr(a, "path"))
	}
	return map[string]any{
		"email":     fieldStr(u, "email"),
		"name":      fieldStr(u, "name"),
		"level":     parseCount(u["level"]),
		"exp":       parseCount(u["exp"]),
		"gender":    fieldStr(u, "gender"),
		"title":     fieldStr(u, "title"),
		"avatarUrl": avatar,
		"isPunched": boolOf(u["isPunched"]),
		"createdAt": fieldStr(u, "created_at"),
	}
}

// PunchIn 每日打卡(POST users/punch-in)。
func (c *Client) PunchIn(ctx context.Context, token string) (map[string]any, error) {
	return c.reqAPI(ctx, "POST", "users/punch-in", token, nil)
}
