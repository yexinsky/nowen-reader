package handler

// JM 标签补全/漫画名补全端点(私有扩展):用 JM 在线源修补书库旧书。
//
//	GET  /api/jm/backfill/candidates → 候选清单(mode=tags 无标签 / mode=title 全部)
//	POST /api/jm/backfill/match      → 关键词搜索 + 标题打分(不改库)
//	POST /api/jm/backfill/apply      → 按 aid 拉详情,写入标签/作者
//	POST /api/jm/backfill/rename     → 按 aid 拉详情,合成新名改名(漫画名补全,实现见 jm_backfill_rename.go)
//
// 设计:无状态三端点,匹配/应用节奏由前端选择流驱动;「无标签」本身即进度源
// (补过标的漫画自动退出 candidates),重新打开弹窗天然断点续跑。
// candidates 支持 libraryIds 逗号分隔参数与可管理书库求交集(书库页弹窗按当前所选书库过滤)。
// 上游纪律:match/apply 共用限速器(≥1.2s/次),循环调用打不穿上游。
// 写入口径与下载入库自动打标(jm_download.go)完全一致:
// 标签 normalizeJmTags(上限 30)、作者过占位符、author/metadataSource 仅空缺回填。

import (
	"errors"
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
// HTML 实体 → 中央 id 拆分(取较干净的一半) → 数字 id 段/重复段 → 噪声括号 →
// 卷话后缀 → 尾部 id,循环至稳定;最后剥掉全部括号留标题主体。
// 实测:上游 /search 对带括号的长关键词会失效(返回默认排行列表,total=10000),
// 纯标题主体命中率最高。id 取 4~7 位:JM aid 实际 5~7 位,4 位起步会误杀
// "一拳超人 2"类短数字;全角数字不处理(实测旧库不存在该形态)。
var (
	reBackfillNoiseBracket = regexp.MustCompile(
		`(?i)[【\[(（][^】\])）]{0,40}?(?:汉化|漢化|group|中文|生肉|熟肉|无修|無修|raw|简体|繁體|繁体|搬运|搬運|转载|轉載|扫图|掃圖|嵌字|压制|潤色|润色|翻譯|翻訳|翻译|机翻|機翻|digital|カラー|color|generated|dl版|去码)[^】\])）]{0,40}?[】\])）]`)
	reBackfillVolumeSuffix = regexp.MustCompile(
		`(?:第\s*[0-9零一二三四五六七八九十百千两]+\s*[卷話巻话集章部季]|[Vv]ol(?:ume)?\.?\s*[0-9]+|[Cc]h(?:apter)?\.?\s*[0-9]+|\([0-9]{1,3}\))\s*$`)
	reBackfillTailID = regexp.MustCompile(`[-–—_]\s*[0-9]{4,7}$`)
	// 中央数字段:爬虫拼接的双变体标题 "变体A-<id>-变体B"(id 通常即 JM aid)
	reBackfillCentralID = regexp.MustCompile(`^(.+)-[0-9]{4,7}-(.+)$`)
	// 最内层括号组(迭代剥离实现嵌套支持)
	reBackfillParen  = regexp.MustCompile(`[（(][^()（）]*[)）]`)
	reBackfillSquare = regexp.MustCompile(`[【\[][^【\]【】]*[】\]]`)
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

// jmStripEntities 清理标题里的 HTML 实体(旧库实测含 "&amp;nbsp;")。
func jmStripEntities(s string) string {
	s = strings.ReplaceAll(s, "&amp;nbsp;", " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.ReplaceAll(s, "&amp;", " ")
}

// jmPickCleanerHalf 双变体标题 "A-<id>-B":两半是同一作品的不同详略版本
// (通常一半带汉化组前缀),取更短的一半作为搜索基准;过短(<8 字符)时取另一半。
// 两半皆为纯数字(如 "1234-5678")时放弃拆分——数字半边会被上游当车号误搜。
func jmPickCleanerHalf(s string) string {
	m := reBackfillCentralID.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	left, right := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	if left == "" || right == "" {
		return s
	}
	short, long := left, right
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) < 8 {
		short = long
	}
	if jmIsDigits(short) {
		return s
	}
	return short
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

// jmExtractCoreTitle 剥掉全部括号组(嵌套由最内层优先迭代解决)留标题主体。
// 结果过短(<4 字符,标题全在括号里)时返回空串,由调用方回退剥括号前的结果。
func jmExtractCoreTitle(s string) string {
	for i := 0; i < 8; i++ {
		before := s
		s = reBackfillParen.ReplaceAllString(s, " ")
		s = reBackfillSquare.ReplaceAllString(s, " ")
		if strings.TrimSpace(s) == before {
			break
		}
		s = strings.TrimSpace(s)
	}
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) < 4 {
		return ""
	}
	if n := utf8.RuneCountInString(s); n > 100 {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:100]))
	}
	return s
}

// jmCleanSearchKeyword 书库标题 → JM 搜索词(兜底路径;标题内嵌 aid 时优先车号直达)。
// 纯数字标题(车号)原样保留:上游 /search 对纯数字走 redirect_aid 单详情直达。
func jmCleanSearchKeyword(title string) string {
	s := jmStripEntities(strings.TrimSpace(title))
	s = jmPickCleanerHalf(s)
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
	s = strings.Join(strings.Fields(s), " ")
	if core := jmExtractCoreTitle(s); core != "" {
		return core // 剥括号后的主体(实测上游只对无括号短词可靠);全在括号里则回退
	}
	return s
}

/* ── 内嵌 aid 提取 ── */

// 标题内嵌的 JM aid(爬虫落库痕迹,实测占旧库绝大多数):段边界为连字符/括号/
// 首尾,5~7 位。命中即可走车号精确匹配,不再依赖标题相似度。4 位以下不取
// (卷号/短数字误报)。如 "X-262147-Y"、"X-465577"(尾部)、"(...-1054639)-..."。
var reBackfillEmbeddedAid = regexp.MustCompile(`[-–—_(](\d{5,7})[-–—_)]`)
var reBackfillTailAid = regexp.MustCompile(`[-–—_](\d{5,7})$`)

// jmExtractEmbeddedAid 返回标题内嵌的 JM aid;无则空串。
func jmExtractEmbeddedAid(title string) string {
	s := strings.TrimSpace(title)
	if jmIsDigits(s) && len(s) >= 5 && len(s) <= 7 {
		return s // 整个标题就是车号
	}
	if m := reBackfillEmbeddedAid.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if m := reBackfillTailAid.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

/* ── 标题打分 ── */

const (
	jmMatchConfHigh   = 0.85
	jmMatchConfMedium = 0.65
	// 车号直达命中后,标题相似度须达到的最低分(防伪车号:标题里的数字段撞上
	// 无关专辑的车号);达标视为确定命中(score=1/high),不达标按原分定档
	jmAidMatchMinScore = 0.25
)

// jmNormForMatch 归一:小写 + 仅保留字母/数字等文字符(去空白与标点与 HTML 实体,
// CJK 字符按 Letter 保留)。
func jmNormForMatch(s string) string {
	s = jmStripEntities(s)
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

// jmScoreMatch 本地标题/作者 与 JM 结果的匹配置信分(0~1)。
// 实测旧库标题普遍是"爬虫噪声超集"(汉化组括号/标签/双变体),与 JM 原版标题
// 互不包含,原先一律落入 bigram Jaccard 被长短悬殊稀释成低分——这正是大量
// "正确匹配却低置信"的成因。口径改为分档:
//   - 归一全等 → 1.0;
//   - 互相包含:短串 ≥8 字符(同一作品的详略两版,证据极强)→ 0.9;
//     过短子串(卷号撞系列名)→ 0.75;
//   - bigram 子集:一侧 bigram 几乎全部出现在另一侧(轻微删改的变体)→ 0.8;
//   - 其余 Dice 系数 × 0.7(比 Jaccard 对长短悬殊宽容);
//   - 双方作者非空且一致 +0.1(仅在已有标题分之上)。
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
		short, long := lt, rt
		if len(short) > len(long) {
			short, long = long, short
		}
		if utf8.RuneCountInString(short) >= 8 {
			score = 0.9
		} else {
			score = 0.75
		}
	default:
		lb, rb := jmBigramSet(lt), jmBigramSet(rt)
		inter := 0
		for g := range lb {
			if _, ok := rb[g]; ok {
				inter++
			}
		}
		small, big := lb, rb
		if len(small) > len(big) {
			small, big = big, small
		}
		contained := 0
		for g := range small {
			if _, ok := big[g]; ok {
				contained++
			}
		}
		switch {
		case len(small) >= 4 && contained == len(small):
			score = 0.8
		case len(lb)+len(rb) > 0:
			score = 0.7 * 2 * float64(inter) / float64(len(lb)+len(rb))
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
	// GET /backfill/candidates — 候选清单(仅 comic/mixed 且有管理权的书库,
	// 排除 novel 类型行;附标题清洗出的默认搜索词)。
	// mode=tags(缺省,现行为)→ 无标签漫画;mode=title → 书库全部漫画(漫画名
	// 补全:改名对象是标题坏掉的书,与是否已打标无关)。title 模式下 filter=aid
	// (缺省)按标题内嵌车号过滤、filter=all 不过滤;tags 模式忽略 filter。
	g.GET("/backfill/candidates", func(c *gin.Context) {
		uid := getUserID(c)
		libs, err := store.GetAllLibraries()
		if err != nil {
			jmFailErr(c, err, "获取书库失败")
			return
		}
		libraryIDs := make([]string, 0, len(libs))
		// 可选 libraryIds=逗号分隔:与可管理书库求交集(书库页弹窗按当前所选书库过滤);
		// 缺省/为空 = 全部可管理书库
		requested := map[string]struct{}{}
		if raw := strings.TrimSpace(c.Query("libraryIds")); raw != "" {
			for _, s := range strings.Split(raw, ",") {
				if s = strings.TrimSpace(s); s != "" {
					requested[s] = struct{}{}
				}
			}
		}
		for _, lib := range libs {
			if !lib.Enabled || lib.Type == "novel" {
				continue
			}
			if _, ok := requested[lib.ID]; len(requested) > 0 && !ok {
				continue
			}
			canManage, err := store.UserCanManageLibrary(uid, lib.ID)
			if err != nil || !canManage {
				continue
			}
			libraryIDs = append(libraryIDs, lib.ID)
		}

		mode := strings.TrimSpace(c.Query("mode"))
		if mode == "" {
			mode = "tags"
		}
		if mode != "tags" && mode != "title" {
			jmContent422(c, "mode 必须为 tags|title")
			return
		}
		filter := strings.TrimSpace(c.Query("filter"))
		if filter == "" {
			filter = "aid"
		}
		if mode == "title" && filter != "aid" && filter != "all" {
			jmContent422(c, "filter 必须为 aid|all")
			return
		}

		var comics []store.UntaggedComic
		if mode == "title" {
			comics, err = store.ListTitleBackfillCandidates(libraryIDs)
		} else {
			comics, err = store.GetUntaggedComics(libraryIDs)
		}
		if err != nil {
			jmFailErr(c, err, "查询候选漫画失败")
			return
		}
		items := make([]gin.H, 0, len(comics))
		for _, it := range comics {
			embeddedAid := jmExtractEmbeddedAid(it.Title)
			// title 模式 filter=aid 档:仅留标题内嵌车号的书(车号直达最可靠);
			// filter=all 档全量;tags 模式忽略 filter
			if mode == "title" && filter != "all" && embeddedAid == "" {
				continue
			}
			items = append(items, gin.H{
				"id":            it.ID,
				"libraryId":     it.LibraryID,
				"title":         it.Title,
				"author":        it.Author,
				"searchKeyword": jmCleanSearchKeyword(it.Title),
				"embeddedAid":   embeddedAid,
			})
		}
		jmOK(c, gin.H{"list": items, "total": len(items)})
	})

	// POST /backfill/match — 标题内嵌 aid 时优先车号直达(精确命中,标题相似度仅防
	// 伪车号);无 aid 或车号落空 → 关键词全站搜索第 1 页,按本地标题/作者打分排序
	// (不改库)
	g.POST("/backfill/match", func(c *gin.Context) {
		var body struct {
			Keyword string `json:"keyword"`
			Aid     string `json:"aid"`
			Title   string `json:"title"`
			Author  string `json:"author"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			jmContent422(c, "参数不合法")
			return
		}
		body.Aid = strings.TrimSpace(body.Aid)
		keyword := strings.TrimSpace(body.Keyword)
		if body.Aid == "" && keyword == "" {
			jmContent422(c, "keyword 与 aid 至少提供其一")
			return
		}
		if utf8.RuneCountInString(keyword) > 100 {
			jmContent422(c, "keyword 过长(上限 100 字符)")
			return
		}
		cl := jmService().AnonClient()

		// 车号直达:数字关键词触发上游 redirect_aid 单详情包装;要求结果恰好一条且
		// aid 一致(车号无效时上游返回默认列表,不得当作命中)。
		if body.Aid != "" && jmIsDigits(body.Aid) {
			jmBackfillThrottle()
			data, err := cl.ComicsSearch(c.Request.Context(), jm.SearchParams{Keyword: body.Aid, Page: 1})
			if err != nil {
				jmFailErr(c, err, "JM 搜索失败")
				return
			}
			res, _ := data.(map[string]any)
			rawList, _ := res["list"].([]any)
			if len(rawList) == 1 {
				if meta, ok := jm.ExtractComicItemMeta(rawList[0]); ok && meta.Aid == body.Aid {
					score := jmScoreMatch(body.Title, body.Author, meta.Title, meta.Author)
				item := jmMatchItem{
					Aid: meta.Aid, Title: meta.Title, Author: meta.Author,
					Tags: meta.Tags, Score: score, ViaAid: true, CoverURL: meta.CoverURL,
				}
					if score >= jmAidMatchMinScore {
						// aid 即权威匹配;相似度过关则视为确定命中
						item.Score = 1
						item.Confidence = "high"
					} else {
						item.Confidence = jmMatchConfidence(score)
					}
					jmOK(c, gin.H{"list": []jmMatchItem{item}, "total": 1, "keyword": body.Aid, "viaAid": true})
					return
				}
			}
			if keyword == "" {
				// 车号落空且无兜底关键词
				jmOK(c, gin.H{"list": []jmMatchItem{}, "total": 0, "keyword": body.Aid, "viaAid": false})
				return
			}
		}

		jmBackfillThrottle()
		data, err := cl.ComicsSearch(c.Request.Context(), jm.SearchParams{
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
				Aid:      meta.Aid,
				Title:    meta.Title,
				Author:   meta.Author,
				Tags:     meta.Tags,
				Score:    jmScoreMatch(body.Title, body.Author, meta.Title, meta.Author),
				CoverURL: meta.CoverURL,
			})
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
		if len(matches) > 8 {
			matches = matches[:8]
		}
		for i := range matches {
			matches[i].Confidence = jmMatchConfidence(matches[i].Score)
		}
		jmOK(c, gin.H{"list": matches, "total": len(matches), "keyword": keyword, "viaAid": false})
	})

	// POST /backfill/apply — 后端自行拉详情取标签(不信任客户端透传),
	// 写入口径与下载入库自动打标一致;内容标签不存在自动创建(AddTagsToComic upsert),
	// 有效作者名追加 author-kind 独立标签(AddAuthorTagToComic)
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
		// 作者独立标签(author-kind):与下载入库口径一致;撞既有内容标签同名仅日志跳过
		if author != "" {
			if err := store.AddAuthorTagToComic(body.ComicID, author); err != nil {
				if errors.Is(err, store.ErrAuthorNameConflictsWithTag) {
					log.Printf("[jm] 补标签:作者名与既有内容标签同名,跳过作者标签(comic=%s, author=%s)", body.ComicID, author)
				} else {
					log.Printf("[jm] 补标签作者标签写入失败(comic=%s): %v", body.ComicID, err)
				}
			}
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

	// POST /backfill/rename — 漫画名补全:按 aid 拉详情取 canonical 标题改名
	// (替换写入,防破坏纪律与 JSONL 审计见 jm_backfill_rename.go)
	g.POST("/backfill/rename", jmBackfillRename)
}

// jmMatchItem match 响应条目(按 score 倒序,最多 8 条)。
type jmMatchItem struct {
	Aid        string   `json:"aid"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Tags       []string `json:"tags"`
	Score      float64  `json:"score"`
	Confidence string   `json:"confidence"`
	CoverURL   string   `json:"coverUrl,omitempty"` // 站内 /api/image 代理路径,选择流封面用
	// ViaAid=true 表示按标题内嵌车号直达命中(aid 即权威匹配,与标题相似度无关)
	ViaAid bool `json:"viaAid"`
}
