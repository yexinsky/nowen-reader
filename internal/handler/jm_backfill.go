package handler

// JM 书库补标签端点(私有扩展):用 JM 在线源给书库中无标签的旧书批量补标签。
//
//	GET  /api/jm/backfill/candidates → 无标签漫画清单(附清洗后的搜索词)
//	POST /api/jm/backfill/match      → 关键词搜索 + 标题打分(不改库)
//	POST /api/jm/backfill/apply      → 按 aid 拉详情,写入标签/作者
//
// 设计:无状态三端点,批量循环由前端驱动;「无标签」本身即进度源
// (补过标的漫画自动退出 candidates),刷新页面天然断点续跑。
// 上游纪律:match/apply 共用限速器(≥1.2s/次),批量循环打不穿上游。
// 写入口径与下载入库自动打标(jm_download.go)完全一致:
// 标签 normalizeJmTags(上限 30)、作者过占位符、author/metadataSource 仅空缺回填。

import (
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/jm"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

/* ── 搜索词清洗 ── */

// 旧书标题混有"漫画名-数字id"、卷话后缀、汉化组括号等噪声。清洗顺序:
// 数字 id 段/重复段 → 噪声括号 → 卷话后缀 → 尾部 id,循环至稳定。
// id 取 4~7 位:JM aid 实际 5~7 位,4 位起步会误杀"一拳超人 2"类短数字,
// 故定 4~7;全角数字不处理(实测旧库不存在该形态)。
var (
	reBackfillNoiseBracket = regexp.MustCompile(
		`(?i)[【\[(（][^】\])）]{0,40}?(?:汉化|漢化|group|中文|生肉|熟肉|无修|無修|raw|简体|繁體|繁体|搬运|搬運|转载|轉載|扫图|掃圖|嵌字|压制|潤色|润色)[^】\])）]{0,40}?[】\])）]`)
	reBackfillVolumeSuffix = regexp.MustCompile(
		`(?:第\s*[0-9零一二三四五六七八九十百千两]+\s*[卷話巻话集章部季]|[Vv]ol(?:ume)?\.?\s*[0-9]+|[Cc]h(?:apter)?\.?\s*[0-9]+|\([0-9]{1,3}\))\s*$`)
	reBackfillTailID = regexp.MustCompile(`[-–—_]\s*[0-9]{4,7}$`)
)

// jmIsDigits 纯 ASCII 数字判断(数字 id 段只可能是 ASCII,与 len 语义一致)。
func jmIsDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// jmStripIDSegments 处理连字符分隔形态的数字 id 与重复段(实测旧库存在
// "[作者]标题-224406-[作者]标题"——id 夹在中间、前后重复,尾部正则无法命中):
// 按 -/–/—/_ 切段,剔除 4~7 位纯数字段、去掉重复段,"A-A" → "A"。
// 保守边界:单段(含纯数字车号标题)、无任何剔除、或剔完为空(全数字段)时原样返回,
// 不动 "test-comic" 这类合法连字符名。
func jmStripIDSegments(s string) string {
	segs := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '–' || r == '—' || r == '_'
	})
	if len(segs) <= 1 {
		return s
	}
	out := make([]string, 0, len(segs))
	seen := make(map[string]struct{}, len(segs))
	changed := false
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		if n := len(seg); n >= 4 && n <= 7 && jmIsDigits(seg) {
			changed = true
			continue
		}
		if _, dup := seen[seg]; dup {
			changed = true
			continue
		}
		seen[seg] = struct{}{}
		out = append(out, seg)
	}
	if !changed || len(out) == 0 {
		return s
	}
	return strings.Join(out, "-")
}

// jmCleanSearchKeyword 书库标题 → JM 搜索词。
// 纯数字标题(车号)原样保留:上游 /search 对纯数字走 redirect_aid 单详情直达。
func jmCleanSearchKeyword(title string) string {
	s := strings.TrimSpace(title)
	for i := 0; i < 4; i++ {
		before := s
		s = jmStripIDSegments(s)
		s = strings.TrimSpace(reBackfillNoiseBracket.ReplaceAllString(s, " "))
		s = strings.TrimSpace(reBackfillVolumeSuffix.ReplaceAllString(s, ""))
		if t := strings.TrimSpace(reBackfillTailID.ReplaceAllString(s, "")); t != s && !jmIsDigits(t) {
			// 尾部 id 剥离后不得只剩纯数字(避免把 "1234-5678" 削成伪车号 "1234")
			s = t
		}
		if s == before {
			break
		}
	}
	return strings.Join(strings.Fields(s), " ")
}

/* ── 标题打分 ── */

const (
	jmMatchConfHigh   = 0.85
	jmMatchConfMedium = 0.65
)

// jmNormForMatch 归一:小写 + 仅保留字母/数字等文字符(去空白与标点,
// CJK 字符按 Letter 保留)。
func jmNormForMatch(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// jmBigramSet CJK 友好的相邻字符对集合(单字符串返回空集,只走全等/包含分支)。
func jmBigramSet(s string) map[string]struct{} {
	rs := []rune(s)
	set := make(map[string]struct{}, len(rs))
	for i := 0; i+1 < len(rs); i++ {
		set[string(rs[i:i+2])] = struct{}{}
	}
	return set
}

// jmScoreMatch 本地标题/作者 与 JM 结果的匹配置信分(0~1):
// 归一全等 1.0;互相包含 0.75;其余 CJK 字符 bigram Jaccard × 0.7;
// 双方作者非空且一致 +0.1(仅在已有标题分之上,无标题分不加)。
func jmScoreMatch(localTitle, localAuthor, remoteTitle, remoteAuthor string) float64 {
	lt, rt := jmNormForMatch(localTitle), jmNormForMatch(remoteTitle)
	if lt == "" || rt == "" {
		return 0
	}
	var score float64
	switch {
	case lt == rt:
		score = 1
	case strings.Contains(lt, rt) || strings.Contains(rt, lt):
		score = 0.75
	default:
		lb, rb := jmBigramSet(lt), jmBigramSet(rt)
		inter := 0
		for g := range lb {
			if _, ok := rb[g]; ok {
				inter++
			}
		}
		if union := len(lb) + len(rb) - inter; union > 0 {
			score = 0.7 * float64(inter) / float64(union)
		}
	}
	if la, ra := jmNormForMatch(localAuthor), jmNormForMatch(remoteAuthor); score > 0 &&
		la != "" && ra != "" && la == ra {
		score += 0.1
	}
	if score > 1 {
		score = 1
	}
	return score
}

// jmMatchConfidence 分数 → 置信档位(前端徽章与"高置信一键应用"的依据)。
func jmMatchConfidence(score float64) string {
	switch {
	case score >= jmMatchConfHigh:
		return "high"
	case score >= jmMatchConfMedium:
		return "medium"
	default:
		return "low"
	}
}

/* ── 上游限速 ── */

// jmBackfillMinInterval match/apply 共用的上游最小调用间隔。
const jmBackfillMinInterval = 1200 * time.Millisecond

var (
	jmBackfillRateMu   sync.Mutex
	jmBackfillLastCall time.Time
)

// jmBackfillThrottle 持锁睡眠实现全局串行排队:并发请求在此天然节流,
// 无论前端循环多快,到上游的节奏恒 ≥ jmBackfillMinInterval。
func jmBackfillThrottle() {
	jmBackfillRateMu.Lock()
	defer jmBackfillRateMu.Unlock()
	if wait := jmBackfillMinInterval - time.Since(jmBackfillLastCall); wait > 0 {
		time.Sleep(wait)
	}
	jmBackfillLastCall = time.Now()
}

/* ── 端点 ── */

func registerJMBackfillRoutes(g *gin.RouterGroup) {
	// GET /backfill/candidates — 无标签漫画清单(仅 comic/mixed 且有管理权的书库,
	// 排除 novel 类型行;附标题清洗出的默认搜索词)
	g.GET("/backfill/candidates", func(c *gin.Context) {
		uid := getUserID(c)
		libs, err := store.GetAllLibraries()
		if err != nil {
			jmFailErr(c, err, "获取书库失败")
			return
		}
		libraryIDs := make([]string, 0, len(libs))
		for _, lib := range libs {
			if !lib.Enabled || lib.Type == "novel" {
				continue
			}
			canManage, err := store.UserCanManageLibrary(uid, lib.ID)
			if err != nil || !canManage {
				continue
			}
			libraryIDs = append(libraryIDs, lib.ID)
		}
		comics, err := store.GetUntaggedComics(libraryIDs)
		if err != nil {
			jmFailErr(c, err, "查询无标签漫画失败")
			return
		}
		items := make([]gin.H, 0, len(comics))
		for _, it := range comics {
			items = append(items, gin.H{
				"id":            it.ID,
				"libraryId":     it.LibraryID,
				"title":         it.Title,
				"author":        it.Author,
				"searchKeyword": jmCleanSearchKeyword(it.Title),
			})
		}
		jmOK(c, gin.H{"list": items, "total": len(items)})
	})

	// POST /backfill/match — 关键词全站搜索第 1 页,按本地标题/作者打分排序(不改库)
	g.POST("/backfill/match", func(c *gin.Context) {
		var body struct {
			Keyword string `json:"keyword"`
			Title   string `json:"title"`
			Author  string `json:"author"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Keyword) == "" {
			jmContent422(c, "keyword 必填")
			return
		}
		keyword := strings.TrimSpace(body.Keyword)
		if utf8.RuneCountInString(keyword) > 100 {
			jmContent422(c, "keyword 过长(上限 100 字符)")
			return
		}
		jmBackfillThrottle()
		data, err := jmService().AnonClient().ComicsSearch(c.Request.Context(), jm.SearchParams{
			Keyword: keyword,
			Page:    1,
		})
		if err != nil {
			jmFailErr(c, err, "JM 搜索失败")
			return
		}
		res, _ := data.(map[string]any)
		rawList, _ := res["list"].([]any)

		matches := make([]jmMatchItem, 0, 8)
		for _, item := range rawList {
			meta, ok := jm.ExtractComicItemMeta(item)
			if !ok {
				continue
			}
			matches = append(matches, jmMatchItem{
				Aid:    meta.Aid,
				Title:  meta.Title,
				Author: meta.Author,
				Tags:   meta.Tags,
				Score:  jmScoreMatch(body.Title, body.Author, meta.Title, meta.Author),
			})
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
		if len(matches) > 8 {
			matches = matches[:8]
		}
		for i := range matches {
			matches[i].Confidence = jmMatchConfidence(matches[i].Score)
		}
		jmOK(c, gin.H{"list": matches, "total": len(matches), "keyword": keyword})
	})

	// POST /backfill/apply — 后端自行拉详情取标签(不信任客户端透传),
	// 写入口径与下载入库自动打标一致;标签不存在自动创建(AddTagsToComic upsert)
	g.POST("/backfill/apply", func(c *gin.Context) {
		var body struct {
			ComicID string `json:"comicId"`
			Aid     string `json:"aid"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.ComicID == "" || body.Aid == "" {
			jmContent422(c, "comicId 与 aid 必填")
			return
		}
		comic, err := store.GetComicByID(body.ComicID)
		if err != nil || comic == nil {
			jmFail(c, jm.CodeNotFound, "漫画不存在", nil)
			return
		}
		if canManage, err := store.UserCanManageLibrary(getUserID(c), comic.LibraryID); err != nil || !canManage {
			c.JSON(403, gin.H{"detail": "Forbidden: 无该书库的管理权限"})
			return
		}
		jmBackfillThrottle()
		detail, err := jmService().AnonClient().ComicDetail(c.Request.Context(), body.Aid)
		if err != nil {
			jmFailErr(c, err, "获取 JM 详情失败")
			return
		}
		tags, authorRaw := jm.ExtractDetailMeta(detail)
		author := jmSyncAuthorName(authorRaw)

		applied := 0
		if len(tags) > 0 {
			if err := store.AddTagsToComic(body.ComicID, tags); err != nil {
				jmFailErr(c, err, "标签写入失败")
				return
			}
			applied = len(tags)
		}
		// 作者与元数据来源仅空缺回填,不覆盖刮削/手动结果
		fields := map[string]interface{}{}
		if author != "" && strings.TrimSpace(comic.Author) == "" {
			fields["author"] = author
		}
		if strings.TrimSpace(comic.MetadataSource) == "" {
			fields["metadataSource"] = "jm"
		}
		if len(fields) > 0 {
			if err := store.UpdateComicFields(body.ComicID, fields); err != nil {
				log.Printf("[jm] 补标签元数据回填失败(comic=%s): %v", body.ComicID, err)
			}
		}
		jmOK(c, gin.H{"applied": applied, "tags": tags, "author": author})
	})
}

// jmMatchItem match 响应条目(按 score 倒序,最多 8 条)。
type jmMatchItem struct {
	Aid        string   `json:"aid"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Tags       []string `json:"tags"`
	Score      float64  `json:"score"`
	Confidence string   `json:"confidence"`
}
