package handler

// JM 漫画名补全(私有扩展):复用标签补全的匹配管线,把写入对象从标签换成标题——
// 给标题坏掉的书(爬虫拼接名)用 JM 详情的 canonical 标题改名。
//
//	POST /api/jm/backfill/rename → 按 aid 拉详情取 canonical 标题,合成新名后改名
//
// 与标签补全的本质差异:改名是**替换**不是加法,防破坏优先——
//   - 卷话标记(第100卷/vol.3/ch.5 等)在 JM 原版标题普遍缺失,新名必须保留
//     本地尾缀,否则同一作品多卷会被改名成同一标题互相覆盖;
//   - 裸数字尾缀("进击的巨人 04")不视为卷号——可能是标题本体,宁可不剥;
//   - 改名不变(oldTitle == newTitle)时什么都不写,author/metadataSource 也不回填;
//   - 每次实际写入追加一行 JSONL 审计日志(写失败只记日志,不影响主流程)。
// 明确不做:不改磁盘文件名、不动 Series/Group 名、不回填简介封面、
// rename 不顺带打标、无撤销 UI(靠 JSONL 日志人工兜底)。

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/config"
	"github.com/nowen-reader/nowen-reader/internal/jm"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

/* ── 卷号标记提取与新名合成 ── */

// 尾部卷话标记正则族(锚定结尾,允许尾随空白):JM 原版标题普遍不带卷号,
// 本地卷号是唯一区分多卷的线索,改名时必须保住。三种形态:
//   - 第<N>[卷話话集章部季],N 支持小数(第12.5话;数字为 ASCII,与卷号实际形态一致)
//   - vol.3 / Vol 12(VOL 大小写不敏感,点可有可无)
//   - ch.5 / Ch 7 / chapter 9(点与 chapter 全拼二选一)
//
// 裸数字尾缀("进击的巨人 04")**不提取**——可能是标题本体,误剥会丢字。
var jmVolumeMarkerRes = []*regexp.Regexp{
	regexp.MustCompile(`第\s*\d+(?:\.\d+)?\s*[卷話话集章部季]\s*$`),
	regexp.MustCompile(`[Vv][Oo][Ll]\.?\s*\d+\s*$`),
	regexp.MustCompile(`[Cc][Hh](?:\.|apter)?\s*\d+\s*$`),
}

// jmExtractVolumeMarker 提取标题尾部的卷话标记(含尾随空白,trim 后返回);
// 无标记返回空串。只认尾部:标题中部的"第3话"可能是作品名本体(如 "第3话的爱情")。
func jmExtractVolumeMarker(title string) string {
	s := strings.TrimRight(title, " \t\n\r")
	for _, re := range jmVolumeMarkerRes {
		if m := re.FindString(s); m != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

// jmComposeRenamedTitle 合成改名后的标题:
//   - 本地无卷号标记 → 直接用 JM canonical 标题;
//   - 有标记且 JM 标题未包含该标记 → JM 标题 + 空格 + 标记(保住多卷区分度);
//   - JM 标题已包含该标记 → 原样(不重复拼接)。
func jmComposeRenamedTitle(originalTitle, jmTitle string) string {
	marker := jmExtractVolumeMarker(originalTitle)
	if marker == "" {
		return jmTitle
	}
	if strings.Contains(jmTitle, marker) {
		return jmTitle
	}
	return jmTitle + " " + marker
}

/* ── 改名端点 ── */

// jmRenameLogLine JSONL 审计日志行(仅实际写入时追加)。
type jmRenameLogLine struct {
	ComicID  string `json:"comicId"`
	OldTitle string `json:"oldTitle"`
	NewTitle string `json:"newTitle"`
	Aid      string `json:"aid"`
	At       string `json:"at"` // UTC RFC3339
}

// jmAppendRenameLog 追加一行改名审计日志到 <DataDir>/jm/title-rename-log.jsonl。
// 审计是附加增强:任何失败只记服务端日志,绝不影响改名主流程。
func jmAppendRenameLog(line jmRenameLogLine) {
	dir := filepath.Join(config.DataDir(), "jm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[jm] 改名日志目录创建失败(comic=%s): %v", line.ComicID, err)
		return
	}
	b, err := json.Marshal(line)
	if err != nil {
		log.Printf("[jm] 改名日志序列化失败(comic=%s): %v", line.ComicID, err)
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "title-rename-log.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[jm] 改名日志打开失败(comic=%s): %v", line.ComicID, err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		log.Printf("[jm] 改名日志写入失败(comic=%s): %v", line.ComicID, err)
	}
}

// jmBackfillRename POST /api/jm/backfill/rename — 按 aid 拉详情取 canonical 标题改名。
// 校验口径与 apply 同款:comicId/aid 必填且 aid 为数字 → 422;漫画不存在 → 404;
// 无书库管理权 → 403。上游纪律:共用 jmBackfillThrottle(≥1.2s/次)。
func jmBackfillRename(c *gin.Context) {
	var body struct {
		ComicID  string  `json:"comicId"`
		Aid      string  `json:"aid"`
		NewTitle *string `json:"newTitle"` // 可选:传入则手动指定新名
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ComicID == "" || body.Aid == "" {
		jmContent422(c, "comicId 与 aid 必填")
		return
	}
	body.Aid = strings.TrimSpace(body.Aid)
	if !jmIsDigits(body.Aid) {
		jmContent422(c, "aid 必须为数字")
		return
	}
	// 手动新名先行校验(纯本地校验,不消耗上游限速配额):trim 后非空、≤200 rune
	newTitle := ""
	if body.NewTitle != nil {
		newTitle = strings.TrimSpace(*body.NewTitle)
		if newTitle == "" {
			jmContent422(c, "newTitle 不能为空")
			return
		}
		if utf8.RuneCountInString(newTitle) > 200 {
			jmContent422(c, "newTitle 过长(上限 200 字符)")
			return
		}
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
	jmTitle := jm.ExtractDetailTitle(detail)
	if jmTitle == "" {
		jmFail(c, jm.CodeUpstream, "JM 详情无标题", nil)
		return
	}
	if newTitle == "" {
		newTitle = jmComposeRenamedTitle(comic.Title, jmTitle)
	}

	oldTitle := comic.Title
	changed := newTitle != oldTitle
	if changed {
		// 标题替换(UpdateComicFields 自动重算 titleSortKey);作者/元数据来源
		// 与 apply 完全同口径:作者过占位符、author/metadataSource 仅空缺回填。
		_, authorRaw := jm.ExtractDetailMeta(detail)
		author := jmSyncAuthorName(authorRaw)
		fields := map[string]interface{}{"title": newTitle}
		if author != "" && strings.TrimSpace(comic.Author) == "" {
			fields["author"] = author
		}
		if strings.TrimSpace(comic.MetadataSource) == "" {
			fields["metadataSource"] = "jm"
		}
		if err := store.UpdateComicFields(body.ComicID, fields); err != nil {
			jmFailErr(c, err, "标题写入失败")
			return
		}
		jmAppendRenameLog(jmRenameLogLine{
			ComicID: body.ComicID, OldTitle: oldTitle, NewTitle: newTitle,
			Aid: body.Aid, At: time.Now().UTC().Format(time.RFC3339),
		})
	}
	jmOK(c, gin.H{"oldTitle": oldTitle, "newTitle": newTitle, "viaAid": true, "changed": changed})
}
