package pica

import "strings"

// PicACG(哔咔漫画)上游常量。
// 依据:D:\workspace\script\picacg-qt\picacg-qt-API文档.md(2026-10-11 实测整理)
// 与 picacg-qt 源码 src/config/config.py、src/tools/tool.py。

// 上游 API 基址(含尾斜杠)。
const apiBaseURL = "https://picaapi.picacomic.com/"

// 签名与请求头常量(picacg-qt config.py 同款)。
const (
	apiKey      = "C69BAF41DA5ABD1FFEDC6D2FEA56B"
	apiSecret   = "~d}$Q7$eIni=V)9\\RK/P.RM4;9[7|@/CA}b~OW!3?EV`:<>M7pddUBL5n|0/*Cn"
	acceptValue = "application/vnd.picacomic.com.v1+json"
	appVersion  = "2.2.1.3.3.4"
	buildVer    = "45"
	appPlatform = "android"
	appChannel  = "3"
	appUUID     = "defaultUuid"
	userAgent   = "okhttp/3.8.1"
)

// imageQualityValues 服务端 image-quality 设置的合法值(请求头 image-quality)。
var imageQualityValues = map[string]bool{
	"original": true, "low": true, "medium": true, "high": true,
}

// 图片存储域名(下载图片用,无需签名头;picacg-qt req.py jumpDomain 汇总)。
var storageDomains = []string{
	"storage-b.picacomic.com",
	"s3.picacomic.com",
	"storage1.picacomic.com",
	"storage.picacomic.com",
	"storage-b.diwodiwo.xyz",
	"s3.diwodiwo.xyz",
	"storage1.diwodiwo.xyz",
	"storage.diwodiwo.xyz",
	"storage.tipatipa.xyz",
	"storage-b.tipatipa.xyz",
	"s3.tipatipa.xyz",
	"storage1.tipatipa.xyz",
}

// imgHosts 图片跳转目标域(URL 含 /static/tobeimg 时上游 301 到此,可预替换直连)。
var imgHosts = []string{
	"img.picacomic.com",
	"img.diwodiwo.xyz",
	"img.tipatipa.xyz",
}

// storageJumpHosts fileServer → img 跳转映射(picacg-qt req.py jumpDomain 同款:
// 预替换后绕过 301;失败时仍可回退原始 fileServer 再试)。
var storageJumpHosts = map[string]string{
	"storage-b.picacomic.com": "img.picacomic.com",
	"s3.picacomic.com":        "img.picacomic.com",
	"storage1.picacomic.com":  "img.picacomic.com",
	"storage.picacomic.com":   "img.picacomic.com",
	"storage-b.diwodiwo.xyz":  "img.diwodiwo.xyz",
	"s3.diwodiwo.xyz":         "img.diwodiwo.xyz",
	"storage1.diwodiwo.xyz":   "img.diwodiwo.xyz",
	"storage.tipatipa.xyz":    "img.tipatipa.xyz",
	"storage-b.tipatipa.xyz":  "img.tipatipa.xyz",
	"s3.tipatipa.xyz":         "img.tipatipa.xyz",
	"storage1.tipatipa.xyz":   "img.tipatipa.xyz",
}

// imageCandidateHosts 图片下载候选域名(轮换重试用):原始 fileServer 域 → img 跳转域
// → 存储池/img 池各补一个备用,去重封顶 5 个。
func imageCandidateHosts(fileServerHost string) []string {
	hosts := make([]string, 0, 5)
	add := func(h string) {
		if h == "" {
			return
		}
		for _, exist := range hosts {
			if equalFoldASCII(exist, h) {
				return
			}
		}
		if len(hosts) < 5 {
			hosts = append(hosts, h)
		}
	}
	add(fileServerHost)
	add(storageJumpHosts[strings.ToLower(fileServerHost)])
	for _, h := range imgHosts {
		add(h)
	}
	for _, h := range storageDomains {
		add(h)
	}
	return hosts
}

// imageHostSuffixes 图片域白名单后缀(实测响应里的 fileServer 域名族:
// picacomic.com / diwodiwo.xyz / tipatipa.xyz 及其子域,含裸域 diwodiwo.xyz)。
var imageHostSuffixes = []string{
	"picacomic.com", ".picacomic.com",
	"diwodiwo.xyz", ".diwodiwo.xyz",
	"tipatipa.xyz", ".tipatipa.xyz",
}

// isKnownImageHost fileServer 白名单:只允许哔咔域名族(防 SSRF)。
func isKnownImageHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range imageHostSuffixes {
		if host == suffix || strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
