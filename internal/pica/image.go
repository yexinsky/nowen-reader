package pica

// PicACG 图片代理管线(站内 GET /api/pica/image?fs=&path=):
//
//	fs/path 白名单 → 缓存命中 → 候选 URL(tobeimg 预跳转 + 域名轮换)下载 → 原样字节返回
//
// 与 JM 管线的差异:哔咔图片无乱序(无 descramble),无需解码重编码——
// 原样透传字节并保留上游 Content-Type(封面 webp/图片 jpg 均保持原始格式)。
// 图片下载无需签名头/authorization,但必须浏览器 TLS 指纹(实测 chrome 语义,tls-client 已统一)。

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"

	tls_client "github.com/bogdanfinn/tls-client"

	"github.com/nowen-reader/nowen-reader/internal/imgcache"
)

var imagePathRegex = regexp.MustCompile(`^[A-Za-z0-9_\-./:]+$`)

const (
	imageMaxPathLen  = 400
	imageDownloadMax = 64 << 20 // 单图体积保护上限
)

// ValidateImagePath 图片相对路径白名单:允许的字符集 + 拒绝 .. 穿越/前导 /、反斜杠。
// 哔咔 path 可能带 imgproxy 裁剪后缀(:300:400),字符白名单覆盖之。
// 不合法返回 *APIError{3001}。
func ValidateImagePath(p string) *APIError {
	invalid := func() *APIError {
		return &APIError{
			Code: CodeNotFound,
			Msg:  "资源不存在",
			Data: map[string]any{"msg": truncateRunes(p, 80)},
		}
	}
	switch {
	case p == "", len(p) > imageMaxPathLen:
		return invalid()
	case strings.Contains(p, "://"):
		return invalid()
	case strings.Contains(p, `\`):
		return invalid()
	case strings.HasPrefix(p, "/"):
		return invalid()
	case strings.Contains(p, ".."):
		return invalid()
	case !imagePathRegex.MatchString(p):
		return invalid()
	}
	return nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ValidateImageFS fileServer 白名单:必须是已知哔咔存储/跳转域的 http(s) URL(防 SSRF)。
func ValidateImageFS(fs string) *APIError {
	u, err := parseURLLoose(fs)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &APIError{Code: CodeNotFound, Msg: "资源不存在", Data: map[string]any{"msg": truncateRunes(fs, 80)}}
	}
	if !isKnownImageHost(strings.ToLower(u.Hostname())) {
		return &APIError{Code: CodeNotFound, Msg: "资源不存在", Data: map[string]any{"msg": truncateRunes(fs, 80)}}
	}
	return nil
}

// BuildImageURL 完整图片 URL(picacg-qt ToolUtil.GetRealUrl 同款):
// fileServer 不以 "static/" 结尾 → 补 "/static/" + path;否则直接拼 path;
// 结果里的 "static/static" 归一为 "static"。
func BuildImageURL(fileServer, path string) string {
	base := strings.TrimRight(fileServer, "/")
	urlPath := strings.TrimLeft(path, "/")
	full := base
	if !strings.HasSuffix(base, "static/") {
		if strings.HasSuffix(base, "static") {
			full = base + "/" + urlPath
		} else {
			full = base + "/static/" + urlPath
		}
	} else {
		full = base + urlPath
	}
	return strings.Replace(full, "static/static", "static", 1)
}

// imageCandidates 候选下载 URL 列表(依次尝试):
// ① tobeimg 预跳转版(命中跳转映射时替换 host 并去掉 /static/tobeimg,绕过 301);
// ② 原始 URL(跟随 301 兜底);
// ③ 其余候选域名的同路径变体。
func imageCandidates(fileServer, path string) []string {
	primary := BuildImageURL(fileServer, path)
	host := hostOf(primary)
	candidates := make([]string, 0, 6)
	add := func(u string) {
		for _, exist := range candidates {
			if exist == u {
				return
			}
		}
		candidates = append(candidates, u)
	}
	if jumped, ok := storageJumpHosts[strings.ToLower(host)]; ok && strings.Contains(primary, "/static/tobeimg") {
		add(strings.Replace(strings.Replace(primary, host, jumped, 1), "/static/tobeimg", "", 1))
	}
	add(primary)
	for _, h := range imageCandidateHosts(host) {
		if equalFoldASCII(h, host) {
			continue
		}
		add(swapHost(primary, h))
	}
	return candidates
}

func hostOf(raw string) string {
	u, err := parseURLLoose(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func swapHost(raw, newHost string) string {
	u, err := parseURLLoose(raw)
	if err != nil {
		return raw
	}
	return strings.Replace(raw, u.Host, newHost, 1)
}

// cacheKey 图片缓存键:md5(fs|path|quality)——画质头可能影响上游返回的图片版本。
func cacheKey(fileServer, path, quality string) string {
	sum := md5.Sum([]byte(fileServer + "|" + path + "|" + quality))
	return hex.EncodeToString(sum[:])
}

// contentTypeOf 响应 Content-Type 归一:仅保留 image/*,扩展名兜底。
func contentTypeOf(ct, path string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch {
	case strings.HasPrefix(ct, "image/"):
		return ct
	}
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, ".png"):
		return "image/png"
	case strings.Contains(lower, ".webp"):
		return "image/webp"
	case strings.Contains(lower, ".gif"):
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

// sniffImageType 按魔数嗅探真实图片格式:
// 实测上游对 webp 字节会错误标注 Content-Type: image/jpeg,需以魔数为准。
func sniffImageType(data []byte) string {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && string(data[0:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 6 && (string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a"):
		return "image/gif"
	default:
		return ""
	}
}

// FetchImage 图片管线入口:白名单 → 缓存 → 候选 URL 下载 → 原样字节。
// 返回 (数据, Content-Type, 是否命中缓存, 错误)。cache 可为 nil(不缓存)。
func FetchImage(ctx context.Context, hc tls_client.HttpClient, cache *imgcache.Cache, fileServer, path, quality string) ([]byte, string, bool, error) {
	if err := ValidateImageFS(fileServer); err != nil {
		return nil, "", false, err
	}
	if err := ValidateImagePath(path); err != nil {
		return nil, "", false, err
	}
	if !imageQualityValues[quality] {
		quality = "original"
	}
	key := cacheKey(fileServer, path, quality)
	if cache != nil {
		if data, ok := cache.Get(key); ok {
			ct := sniffImageType(data)
			if ct == "" {
				ct = contentTypeOf("", path)
			}
			return data, ct, true, nil
		}
	}

	var lastErr error
	for _, endpoint := range imageCandidates(fileServer, path) {
		data, ct, err := httpGetImage(ctx, hc, endpoint)
		if err == nil && len(data) > 0 {
			// 上游 Content-Type 可能误标(webp 字节标 jpeg),魔数嗅探优先
			if sniffed := sniffImageType(data); sniffed != "" {
				ct = sniffed
			} else if ct == "" {
				ct = contentTypeOf("", path)
			}
			if cache != nil {
				stored, reused, err := cache.Put(key, data)
				if err == nil && !reused {
					data = stored
				}
				// 缓存写入失败不影响本次返回(与 JM「缓存写失败注错」不同:
				// 哔咔字节未重编码,重取成本可接受,不因磁盘问题放大为请求失败)
			}
			return data, ct, false, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("图片响应为空")
	}
	return nil, "", false, classifyUpstreamError(lastErr, "图片下载失败")
}

// httpGetImage 单次图片下载(无签名头;UA 对齐哔咔 app)。
func httpGetImage(ctx context.Context, hc tls_client.HttpClient, endpoint string) ([]byte, string, error) {
	req, err := newFhttpRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, imageDownloadMax))
	if err != nil {
		return nil, "", err
	}
	return data, strings.TrimSpace(resp.Header.Get("Content-Type")), nil
}
