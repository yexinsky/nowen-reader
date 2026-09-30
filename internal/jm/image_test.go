package jm

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

/* ── path 白名单(契约 #16 备注) ── */

func TestValidateImagePath(t *testing.T) {
	valid := []string{
		"media/photos/422866/00001.jpg",
		"media/albums/422866_3x4.jpg",
		"media/users/abc.webp",
		"a/b/c.PNG", // 扩展名忽略大小写
		"media/photos/x/y.jpeg",
		"m_1-2.3/4/5.gif",
	}
	for _, p := range valid {
		if err := ValidateImagePath(p); err != nil {
			t.Fatalf("合法路径被拒绝: %q → %v", p, err)
		}
	}
	invalid := []string{
		"",                           // 空
		strings.Repeat("a", 301),     // 超 300
		"https://evil.com/a.jpg",     // 绝对 URL(SSRF)
		"ftp://x/a.jpg",              // 其他 scheme
		"/media/albums/1.jpg",        // 前导斜杠
		`media\albums\1.jpg`,         // 反斜杠
		"media/albums/../secret.jpg", // .. 穿越
		"..",                         // 纯穿越
		"media/albums/1.jpg?u=time",  // query(白名单字符外)
		"media/albums/1.txt",         // 扩展名不在白名单
		"media/albums/1",             // 无扩展名
		"media/albums/<id>.jpg",      // 白名单字符外
		" media/albums/1.jpg",        // 前导空格
		"http:/a.jpg",                // 冒号
	}
	for _, p := range invalid {
		err := ValidateImagePath(p)
		if err == nil {
			t.Fatalf("非法路径未被拒绝: %q", p)
		}
		if err.Code != CodeNotFound {
			t.Fatalf("非法路径应映射 3001,%q → code=%d", p, err.Code)
		}
		if err.Msg != "资源不存在" {
			t.Fatalf("3001 msg 应为 资源不存在, got %q", err.Msg)
		}
	}
	// data.msg 含原 path 截断 80 字符(超长路径 >300 拒绝)
	long := strings.Repeat("x", 301) + ".jpg"
	err := ValidateImagePath(long)
	if err == nil {
		t.Fatal("超长路径应被拒绝")
	}
	if msg, _ := err.Data["msg"].(string); len([]rune(msg)) != 80 {
		t.Fatalf("data.msg 应截断 80 字符, got %d", len([]rune(msg)))
	}
	if msg, _ := ValidateImagePath("bad/path").Data["msg"].(string); msg != "bad/path" {
		t.Fatalf("data.msg 应含原 path, got %q", msg)
	}
}

/* ── 乱序还原(合成小图逐行验证) ── */

// rowImage 生成 w×h 图像,每行填充分配的纯色,便于按行验证重排。
func rowImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := color.RGBA{R: uint8(y*7 + 3), G: uint8(255 - y*5), B: uint8(y*11 + 1), A: 255}
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func rowColor(y int) color.RGBA {
	return color.RGBA{R: uint8(y*7 + 3), G: uint8(255 - y*5), B: uint8(y*11 + 1), A: 255}
}

func TestDescrambleImageNum5(t *testing.T) {
	// 4×10,num=5 → move=2,over=0:dst 行序 = src[8,9,6,7,4,5,2,3,0,1](段序倒置)
	const w, h, num = 4, 10, 5
	dst := descrambleImage(rowImage(w, h), num)
	want := []int{8, 9, 6, 7, 4, 5, 2, 3, 0, 1}
	for y := 0; y < h; y++ {
		got := dst.At(0, y).(color.RGBA)
		if got != rowColor(want[y]) {
			t.Fatalf("num=5 行 %d = %v, want 源行 %d 的 %v", y, got, want[y], rowColor(want[y]))
		}
	}
}

func TestDescrambleImageNum3Over(t *testing.T) {
	// 4×10,num=3 → move=3,over=1:
	// i=0: dst[0..3]   = src[6..9];i=1: dst[4..6] = src[3..5];i=2: dst[7..9] = src[0..2]
	const w, h, num = 4, 10, 3
	dst := descrambleImage(rowImage(w, h), num)
	want := []int{6, 7, 8, 9, 3, 4, 5, 0, 1, 2}
	for y := 0; y < h; y++ {
		got := dst.At(0, y).(color.RGBA)
		if got != rowColor(want[y]) {
			t.Fatalf("num=3 行 %d = %v, want 源行 %d 的 %v", y, got, want[y], rowColor(want[y]))
		}
	}
}

func TestDescrambleImageZero(t *testing.T) {
	src := rowImage(4, 10)
	if descrambleImage(src, 0) != image.Image(src) {
		t.Fatal("num=0 应原样返回")
	}
	if descrambleImage(src, -1) != image.Image(src) {
		t.Fatal("num<0 应原样返回")
	}
}

/* ── 分割数计算(live.py _calc_num 口径) ── */

func TestCalcNum(t *testing.T) {
	// 封面(media/albums/…)是非正文图,上游不做乱序 → 恒 0
	// (此前误用默认阈值导致封面被错误还原,即用户报告的封面重叠 bug)
	if got := calcNum("media/albums/422866_3x4.jpg", "0", "422866"); got != 0 {
		t.Fatalf("封面不应乱序, got %d", got)
	}
	if got := calcNum("media/albums/422866_3x4.jpg", "", "422866"); got != 0 {
		t.Fatalf("封面不应乱序, got %d", got)
	}
	// 正文图:上游 /chapter 不含 scramble_id → 缺省阈值 220980;1476678 必须还原
	if got := calcNum("media/photos/1476678/00001.webp", "0", "1476678"); got <= 0 {
		t.Fatalf("1476678 应需要乱序还原, got %d", got)
	}
	// photo_id 优先从 media/photos/{pid}/… 解析:pid=250000 < 268850 → 10
	if got := calcNum("media/photos/250000/00001.jpg", "220980", "999999"); got != 10 {
		t.Fatalf("photo_id 解析失败, got %d", got)
	}
	// 正文图无 photos 段时兜底 aid(422866 ≥ 421926 → x=8 公式):用 photos 路径验证
	want := getNum(220980, 422866, "00001") // 文件名口径:basename 去扩展名
	if got := calcNum("media/photos/422866/00001.jpg", "220980", "422866"); got != want {
		t.Fatalf("aid 兜底失败, got %d want %d", got, want)
	}
	// photos 段优先级高于 aid 参数:pid=100 < scramble → 0
	if got := calcNum("media/photos/100/00001.jpg", "220980", "422866"); got != 0 {
		t.Fatalf("photo_id 应优先于 aid, got %d", got)
	}
	// 非法 scramble → 回退默认阈值(pid=250000 < 268850 → 10)
	if got := calcNum("media/photos/250000/00001.jpg", "abc", ""); got != 10 {
		t.Fatalf("scramble 非法应回退默认阈值, got %d", got)
	}
	// 文件名口径:basename 去扩展名(SDK of_file_name(_, True))
	if got := imageFilename("media/photos/1/00001.webp"); got != "00001" {
		t.Fatalf("imageFilename = %q, want 00001", got)
	}
	if got := imageFilename("cover.jpg"); got != "cover" {
		t.Fatalf("imageFilename = %q, want cover", got)
	}
}

/* ── 画质映射 ── */

func TestQualityJPEG(t *testing.T) {
	cases := map[string]int{"high": 90, "medium": 75, "low": 60, "": 90, "original": 90}
	for q, want := range cases {
		if got := qualityJPEG(q); got != want {
			t.Fatalf("qualityJPEG(%q) = %d, want %d", q, got, want)
		}
	}
}

/* ── 磁盘 LRU 缓存 ── */

func TestImageCachePutGet(t *testing.T) {
	cache := NewImageCache(filepath.Join(t.TempDir(), "cache"), 5) // 惰性建目录
	if _, ok := cache.Get("k1"); ok {
		t.Fatal("空缓存不应命中")
	}
	if _, reused, err := cache.Put("k1", []byte("hello")); err != nil || reused {
		t.Fatalf("首次写入: err=%v reused=%v", err, reused)
	}
	got, ok := cache.Get("k1")
	if !ok || string(got) != "hello" {
		t.Fatalf("命中失败: ok=%v data=%q", ok, got)
	}
	// 写前二次命中检查:同 key 再写复用既有内容
	got, reused, err := cache.Put("k1", []byte("other"))
	if err != nil || !reused || string(got) != "hello" {
		t.Fatalf("二次写入应复用: err=%v reused=%v data=%q", err, reused, got)
	}
}

func TestImageCacheLRUEviction(t *testing.T) {
	dir := t.TempDir()
	cache := NewImageCache(dir, 2)
	_, _, _ = cache.Put("a", []byte("1"))
	time.Sleep(20 * time.Millisecond)
	_, _, _ = cache.Put("b", []byte("2"))
	time.Sleep(20 * time.Millisecond)
	// 命中 a → 刷新 mtime,a 变为最新
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a 应命中")
	}
	time.Sleep(20 * time.Millisecond)
	_, _, _ = cache.Put("c", []byte("3")) // 超限 → 淘汰最旧 mtime 的 b
	if _, err := os.Stat(filepath.Join(dir, "b.jpg")); !os.IsNotExist(err) {
		t.Fatal("b 应被 LRU 淘汰")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jpg")); err != nil {
		t.Fatalf("命中过的 a 不应被淘汰: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.jpg")); err != nil {
		t.Fatalf("新写入的 c 不应被淘汰: %v", err)
	}
	// .tmp 中转不参与淘汰计数(写入后目录内只剩 2 个 .jpg)
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jpg") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("目录应剩 2 个 .jpg, got %d", n)
	}
}

/* ── 管线端到端(本地 TLS 上游,零外网) ── */

func servePNG(t *testing.T, src image.Image) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	pngBytes := buf.Bytes()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 图片请求头对齐 JM app(契约 #16)
		if r.Header.Get("User-Agent") != appUserAgent {
			t.Errorf("User-Agent 不符: %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Accept") != imageAccept {
			t.Errorf("Accept 不符: %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("X-Requested-With") != appXRequestedWith {
			t.Errorf("X-Requested-With 不符: %q", r.Header.Get("X-Requested-With"))
		}
		if r.Header.Get("Referer") != imageReferer() {
			t.Errorf("Referer 不符: %q", r.Header.Get("Referer"))
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadAndDecodePipeline(t *testing.T) {
	h := 40 // 行数足够大,保证推导出的 num(≤20)不超过图高
	target := rowImage(4, h)

	// 构造「乱序输入」:按还原映射的逆过程填充,使 DownloadAndDecode 还原后恰为 target
	path := "media/photos/250000/00001.jpg"
	num := calcNum(path, "0", "250000")
	if num > h {
		t.Skipf("推导 num=%d 超过测试图高 %d", num, h)
	}
	scrambled := image.NewRGBA(image.Rect(0, 0, 4, h))
	over := h % num
	move := h / num
	for i := 0; i < num; i++ {
		ySrc := h - move*(i+1) - over
		yDst := move * i
		m := move
		if i == 0 {
			m += over
		} else {
			yDst += over
		}
		for k := 0; k < m; k++ {
			for x := 0; x < 4; x++ {
				scrambled.Set(x, ySrc+k, target.At(x, yDst+k))
			}
		}
	}

	srv := servePNG(t, scrambled)
	t.Setenv("JM_IMAGE_DOMAINS", strings.TrimPrefix(srv.URL, "https://"))

	cache := NewImageCache(filepath.Join(t.TempDir(), "cache"), 5)

	data, hit, err := DownloadAndDecode(context.Background(), srv.Client(), cache, path, "0", "250000", "medium")
	if err != nil {
		t.Fatalf("DownloadAndDecode: %v", err)
	}
	if hit {
		t.Fatal("首次请求应未命中缓存")
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出应为合法 JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != h {
		t.Fatalf("输出尺寸 %dx%d, want 4x%d", b.Dx(), b.Dy(), h)
	}
	// 还原正确性:输出的每一行都应与 target 对应行一致(JPEG 有损,用最近邻行匹配)
	nearestRow := func(c color.Color) int {
		best, bestD := 0, 1<<30
		for y := 0; y < h; y++ {
			rc := rowColor(y)
			r, g, b, _ := c.RGBA()
			dr, dg, db := int(r>>8)-int(rc.R), int(g>>8)-int(rc.G), int(b>>8)-int(rc.B)
			d := dr*dr + dg*dg + db*db
			if d < bestD {
				best, bestD = y, d
			}
		}
		return best
	}
	// 相邻行颜色本就相近,JPEG 压缩在拼接边界会有 ±1 行的判定噪声;
	// 整段错位(偏移 >1 行)才代表还原失败
	for _, probe := range []int{0, h / 4, h / 2, 3 * h / 4, h - 1} {
		if got := nearestRow(img.At(0, probe)); (got-probe)*(got-probe) > 1 {
			t.Fatalf("还原后第 %d 行错位(最近邻为第 %d 行,偏移超过容差)", probe, got)
		}
	}
	// 落盘文件存在,键 = md5("{path}|{scramble}|{quality}|{num}")
	key := md5Hex(path + "|0|medium|" + strconv.Itoa(num))
	if _, err := os.Stat(filepath.Join(cache.dir, key+".jpg")); err != nil {
		t.Fatalf("缓存文件应存在: %v", err)
	}
	// 二次请求命中缓存且字节一致
	data2, hit2, err := DownloadAndDecode(context.Background(), srv.Client(), cache, path, "0", "250000", "medium")
	if err != nil || !hit2 {
		t.Fatalf("二次请求应命中: hit=%v err=%v", hit2, err)
	}
	if !bytes.Equal(data, data2) {
		t.Fatal("缓存命中应返回相同字节")
	}
	// 画质入键:换 quality 不命中旧缓存
	if _, hit3, _ := DownloadAndDecode(context.Background(), srv.Client(), cache, path, "0", "250000", "high"); hit3 {
		t.Fatal("不同画质不应命中旧缓存")
	}
}

func TestDownloadAndDecodeErrors(t *testing.T) {
	srv := servePNG(t, rowImage(4, 10))
	t.Setenv("JM_IMAGE_DOMAINS", strings.TrimPrefix(srv.URL, "https://"))
	cache := NewImageCache(filepath.Join(t.TempDir(), "cache"), 5)

	// path 白名单 → 3001(data.msg 含原 path)
	_, _, err := DownloadAndDecode(context.Background(), srv.Client(), cache, "/etc/passwd.jpg", "0", "", "high")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != CodeNotFound {
		t.Fatalf("非法 path 应返回 3001, got %v", err)
	}

	// path 含 .. → 3001(SSRF/穿越双保险)
	if _, _, err := DownloadAndDecode(context.Background(), srv.Client(), cache, "media/../x.jpg", "0", "", "high"); err == nil {
		t.Fatal(".. 穿越应被拒绝")
	}

	// 上游 404 → 2001 图片下载失败
	notFound := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()
	t.Setenv("JM_IMAGE_DOMAINS", strings.TrimPrefix(notFound.URL, "https://"))
	_, _, err = DownloadAndDecode(context.Background(), notFound.Client(), cache, "media/photos/1/1.jpg", "0", "", "high")
	if apiErr, ok := err.(*APIError); !ok || apiErr.Code != CodeUpstream {
		t.Fatalf("上游 404 应映射 2001, got %v", err)
	}

	// 响应非图片 → 2001 图片解码失败
	garbage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not an image"))
	}))
	defer garbage.Close()
	t.Setenv("JM_IMAGE_DOMAINS", strings.TrimPrefix(garbage.URL, "https://"))
	_, _, err = DownloadAndDecode(context.Background(), garbage.Client(), cache, "media/photos/1/1.jpg", "0", "", "high")
	if apiErr, ok := err.(*APIError); !ok || apiErr.Code != CodeUpstream || !strings.Contains(apiErr.Msg, "图片解码失败") {
		t.Fatalf("垃圾响应应映射 2001 图片解码失败, got %v", err)
	}

	// 上游不可达(域名不存在) → 2002 网络
	t.Setenv("JM_IMAGE_DOMAINS", "127.0.0.1:1") // 连接拒绝
	_, _, err = DownloadAndDecode(context.Background(), NewImageHTTPClient(""), cache, "media/photos/1/1.jpg", "0", "", "high")
	if apiErr, ok := err.(*APIError); !ok || apiErr.Code != CodeNetwork {
		t.Fatalf("连接拒绝应映射 2002, got %v", err)
	}
}
