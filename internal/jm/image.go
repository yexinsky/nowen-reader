package jm

// JM 图片代理管线(契约:MOBILE_API.md #16 GET /api/image,live 语义):
//
//	path 白名单 → 缓存命中 → 域名池下载 → 解码 → 乱序还原 → 统一 JPEG 编码 → 落盘(LRU)
//
// 乱序口径:live.py _calc_num(SDK JmImageResp.transfer_to 同源),scramble="0" 不分割;
// photo_id 优先从 path 的 media/photos/{photo_id}/… 解析,兜底 aid 参数;
// getNum 文件名取 basename 且不带扩展名(SDK of_file_name(url, True) / img_file_name)。

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif" // image.Decode 注册 gif(动图取首帧)
	"image/jpeg"
	_ "image/png" // image.Decode 注册 png
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp" // image.Decode 注册 webp(x/image,首帧)
)

/* ── path 白名单(契约 #16 备注,mock/live 双模式生效) ── */

var (
	imagePathRegex = regexp.MustCompile(`(?i)^[A-Za-z0-9_\-./]+\.(jpg|jpeg|png|webp|gif)$`)
	photoIDRegex   = regexp.MustCompile(`^media/photos/(\d+)/`)
)

const imageMaxPathLen = 300

// ValidateImagePath 校验图片路径白名单:
// 正则 ^[A-Za-z0-9_\-./]+\.(jpg|jpeg|png|webp|gif)$(忽略大小写,长度≤300),
// 并拒绝绝对 URL(含 ://,防盲 SSRF)、.. 穿越、前导 /、反斜杠。
// 不合法返回 *APIError{3001, "资源不存在", data.msg=原 path 截断 80 字符}。
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

// truncateRunes 按字符数截断(契约:data.msg 含原 path 截断 80 字符)。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

/* ── 磁盘 LRU 缓存 ── */

// ImageCache 落盘缓存:键 md5("{path}|{scramble}|{quality}"),文件 {key}.jpg。
// 命中即刷新 mtime(真 LRU);写入为 .tmp 原子替换 + 写前二次命中检查(并发同 key 复用);
// 按文件数 LRU 淘汰(超限删最旧 mtime,契约 JM_IMAGE_CACHE_LIMIT=500)。
type ImageCache struct {
	dir   string
	limit int
	mu    sync.Mutex
}

// NewImageCache 构造缓存;dir 不存在时在写入时惰性创建。limit 为文件数上限。
func NewImageCache(dir string, limit int) *ImageCache {
	if limit < 1 {
		limit = 1
	}
	return &ImageCache{dir: dir, limit: limit}
}

func (c *ImageCache) keyPath(key string) string {
	return filepath.Join(c.dir, key+".jpg")
}

// Get 读取缓存;命中刷新 mtime。
func (c *ImageCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.keyPath(key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	_ = os.Chtimes(p, time.Now(), time.Now())
	return data, true
}

// Put 写入缓存:.tmp 原子 os.Rename + 写前二次命中检查(并发同 key 复用先写入者)。
// 返回实际生效的字节与是否复用既有文件;写入失败返回错误(契约:缓存写入失败 → 2001)。
func (c *ImageCache) Put(key string, data []byte) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return nil, false, err
	}
	p := c.keyPath(key)
	// 写前二次命中检查:并发同 key 时另一个请求可能已落盘,直接复用
	if exist, err := os.ReadFile(p); err == nil {
		_ = os.Chtimes(p, time.Now(), time.Now())
		return exist, true, nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return nil, false, err
	}
	c.evictLocked()
	return data, false, nil
}

// evictLocked 按文件数 LRU 淘汰:扫描 *.jpg,超限删最旧 mtime。调用方需持有 mu。
func (c *ImageCache) evictLocked() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type item struct {
		path string
		mod  time.Time
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
			continue // 排除 .tmp 中转与目录
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{filepath.Join(c.dir, e.Name()), info.ModTime()})
	}
	if len(items) <= c.limit {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	for i := 0; i < len(items)-c.limit; i++ {
		_ = os.Remove(items[i].path)
	}
}

/* ── 下载与解码 ── */

const (
	imageDownloadTimeout = 30 * time.Second
	imageMaxBytes        = 64 << 20 // 单图体积保护上限
)

// NewImageHTTPClient 图片下载专用 HTTP 客户端(30s 超时;proxy 空=直连)。
func NewImageHTTPClient(proxy string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil && pu.Scheme != "" {
			transport.Proxy = http.ProxyURL(pu)
		}
	} else {
		transport.Proxy = nil
	}
	return &http.Client{Transport: transport, Timeout: imageDownloadTimeout}
}

// imageReferer 图片请求 Referer(live 口径:API 域名池第 1 个)。
func imageReferer() string {
	if d := apiDomainsOverride(); len(d) > 0 {
		return "https://" + d[0]
	}
	return "https://" + defaultAPIDomains[0]
}

// downloadImage 依次尝试图片域名池(前 3 个,live 口径),首个成功即返回。
// URL 只由后端构建(前端不可指定域名)。全部失败按 live 语义分类:网络关键字 → 2002,其余 → 2001。
func downloadImage(ctx context.Context, httpClient *http.Client, imagePath string) ([]byte, error) {
	if httpClient == nil {
		httpClient = NewImageHTTPClient("")
	}
	var lastErr error
	for _, domain := range imageDomains() {
		attemptCtx, cancel := context.WithTimeout(ctx, imageDownloadTimeout)
		endpoint := "https://" + domain + "/" + imagePath
		data, err := httpGetImage(attemptCtx, httpClient, endpoint)
		cancel()
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, classifyUpstreamError(lastErr, "图片下载失败")
}

// httpGetImage 单次图片下载:请求头对齐 JM app(imageAccept/X-Requested-With/Referer)。
func httpGetImage(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", appUserAgent)
	req.Header.Set("Accept", imageAccept)
	req.Header.Set("X-Requested-With", appXRequestedWith)
	req.Header.Set("Referer", imageReferer())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return io.ReadAll(io.LimitReader(resp.Body, imageMaxBytes))
}

// decodeAnyImage 解码任意已注册格式(jpeg/png/gif/webp;gif/webp 动图即首帧)。
// 解码失败 → 2001(契约:图片解码失败)。
func decodeAnyImage(raw []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errUpstream("图片解码失败", err.Error())
	}
	return img, nil
}

/* ── 分割数与画质 ── */

// qualityJPEG 画质映射(契约 #16 备注):high/medium/low → 90/75/60;空值/未知按 high。
func qualityJPEG(quality string) int {
	switch quality {
	case "medium":
		return 75
	case "low":
		return 60
	default:
		return 90
	}
}

// calcNum 计算分割数(live.py _calc_num 口径):
// 乱序仅作用于正文页图(path 含 media/photos/{pid}/…);封面(media/albums/…)、
// 头像(media/users/…)等非正文图上游不做乱序,直接返回 0。
// 正文图:上游 /chapter 响应不含 scramble_id 字段,SDK 缺省使用 SCRAMBLE_220980
// ——aid < 220980 的老作品不分割,否则按文件名 md5 推导;参数不可解析时不分割。
func calcNum(imagePath, scramble, aid string) int {
	photoID := photoIDFromPath(imagePath)
	if photoID == "" {
		return 0
	}
	if scramble == "" {
		scramble = "0"
	}
	scrambleID, err := strconv.ParseInt(scramble, 10, 64)
	if err != nil || scrambleID <= 0 {
		// 上游未提供 scramble_id → SDK 缺省阈值 220980
		scrambleID = scrambleDefaultID
	}
	aidNum, err := strconv.ParseInt(photoID, 10, 64)
	if err != nil {
		return 0
	}
	return getNum(scrambleID, aidNum, imageFilename(imagePath))
}

// photoIDFromPath 从 path 的 media/photos/{photo_id}/… 段解析 photo_id(无 → "")。
func photoIDFromPath(p string) string {
	m := photoIDRegex.FindStringSubmatch(p)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// imageFilename getNum 使用的文件名:basename 去扩展名(SDK img_file_name "without suffix")。
func imageFilename(p string) string {
	base := p
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	return base
}

/* ── 管线入口 ── */

// DownloadAndDecode 图片管线:白名单校验 → 缓存 → 下载 → 解码 → 乱序还原 → 统一 JPEG 编码 → 落盘。
// quality 为 high|medium|low(空按 high);cache 可为 nil(不缓存)。
// 返回 JPEG 字节与是否命中缓存(落盘命中与并发写入复用均算 hit)。
// 错误:*APIError(3001 path 非法 / 2001 下载·解码·缓存写入失败 / 2002 网络)。
func DownloadAndDecode(ctx context.Context, httpClient *http.Client, cache *ImageCache, imagePath, scramble, aid, quality string) ([]byte, bool, error) {
	if err := ValidateImagePath(imagePath); err != nil {
		return nil, false, err
	}
	if scramble == "" {
		scramble = "0"
	}
	if quality == "" {
		quality = "high"
	}

	// 缓存键:md5("{path}|{scramble}|{quality}|{num}")——num 为推导出的分割数,
	// 入键以保证还原算法/默认阈值调整后旧缓存(可能未正确还原)自动失效
	num := calcNum(imagePath, scramble, aid)
	key := md5Hex(imagePath + "|" + scramble + "|" + quality + "|" + strconv.Itoa(num))
	if cache != nil {
		if data, ok := cache.Get(key); ok {
			return data, true, nil
		}
	}

	raw, err := downloadImage(ctx, httpClient, imagePath)
	if err != nil {
		return nil, false, err
	}

	src, err := decodeAnyImage(raw)
	if err != nil {
		return nil, false, err
	}

	if num > 0 {
		src = descrambleImage(src, num)
	}

	// 统一 JPEG 输出;单次编码即目标画质(等价 Python"75 直存,否则无损中转重编码一次"的成品语义)。
	// 灰度源(image.Gray)由编码器保留为灰度 JPEG;动图仅首帧(解码即首帧)。
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: qualityJPEG(quality)}); err != nil {
		return nil, false, errUpstream("图片编码失败", err.Error())
	}
	data := buf.Bytes()

	if cache != nil {
		stored, _, err := cache.Put(key, data)
		if err != nil {
			return nil, false, errUpstream("缓存写入失败", err.Error())
		}
		data = stored
	}
	return data, false, nil
}
