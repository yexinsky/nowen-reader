package handler

import (
	"sort"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/jm"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// jmComicTagsByKind 读一本漫画挂的标签，按 kind（'tag' 内容 / 'author' 作者）分组。
func jmComicTagsByKind(t *testing.T, comicID string) map[string][]string {
	t.Helper()
	rows, err := store.DB().Query(
		`SELECT t."name", COALESCE(t."kind", 'tag') FROM "ComicTag" ct
		 JOIN "Tag" t ON t."id" = ct."tagId" WHERE ct."comicId" = ?`, comicID)
	if err != nil {
		t.Fatalf("query comic tags failed: %v", err)
	}
	defer rows.Close()
	byKind := map[string][]string{}
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			t.Fatalf("scan comic tag failed: %v", err)
		}
		byKind[kind] = append(byKind[kind], name)
	}
	sort.Strings(byKind["tag"])
	sort.Strings(byKind["author"])
	return byKind
}

// TestJmDownloadAutoTagsRespectBlocklist 下载入库自动打标端到端：归档后按任务快照打标时，
// 过滤名单命中项不写入（不建标签、不挂链），未命中项与作者标签照常写入。
func TestJmDownloadAutoTagsRespectBlocklist(t *testing.T) {
	setupTestRouter(t)
	t.Setenv("DATA_DIR", t.TempDir())

	const (
		libID   = "lib-download-filter"
		zipName = "测试漫画-125734.zip"
	)
	comicID := store.PathToID(libID, zipName)
	if _, err := store.DB().Exec(
		`INSERT INTO "Comic" ("id", "filename", "title", "relativePath", "libraryId", "type", "fileSize")
		 VALUES (?, ?, ?, ?, ?, 'comic', 1000)`,
		comicID, zipName, "测试漫画", zipName, libID); err != nil {
		t.Fatalf("insert comic failed: %v", err)
	}

	if _, _, err := store.AddTagFilters([]string{"過膝襪", "DL版"}); err != nil {
		t.Fatalf("AddTagFilters failed: %v", err)
	}

	jmApplyTagsWhenComicExists(&jm.DownloadTask{
		Aid:       "125734",
		LibraryID: libID,
		ZipName:   zipName,
		Tags:      []string{"巨乳", "过膝袜", "剧情", "dl版"},
		Author:    "山本ティナ",
	})

	byKind := jmComicTagsByKind(t, comicID)
	wantContent := []string{"剧情", "巨乳"}
	if got := byKind["tag"]; len(got) != len(wantContent) || got[0] != wantContent[0] || got[1] != wantContent[1] {
		t.Fatalf("content tags = %v, want %v（过滤名单命中项不得写入）", got, wantContent)
	}
	if got := byKind["author"]; len(got) != 1 || got[0] != "山本ティナ" {
		t.Fatalf("author tags = %v, want [山本ティナ]（作者不走内容标签过滤）", got)
	}

	// 命中项不得在标签库留下记录（污名不建标签）
	var leaked int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM "Tag" WHERE "name" IN ('DL版','过膝袜')`).Scan(&leaked); err != nil {
		t.Fatalf("count leaked tags failed: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("过滤名单命中项不应进入 Tag 表, got %d", leaked)
	}
}

// TestJmBackfillApplyTagsRespectBlocklist 标签补全 apply 的写入闸门：命中名单的标签
// 不写库、不出现在写入集合，改为从 dropped 侧返回（端点把它放进 filteredTags）。
func TestJmBackfillApplyTagsRespectBlocklist(t *testing.T) {
	setupTestRouter(t)

	if _, _, err := store.AddTagFilters([]string{"DL版"}); err != nil {
		t.Fatalf("AddTagFilters failed: %v", err)
	}
	kept, dropped := jmFilterTagsByBlocklist([]string{"巨乳", "DL版", "剧情"})
	if len(kept) != 2 || kept[0] != "巨乳" || kept[1] != "剧情" {
		t.Fatalf("kept = %v, want [巨乳 剧情]", kept)
	}
	if len(dropped) != 1 || dropped[0] != "DL版" {
		t.Fatalf("dropped = %v, want [DL版]", dropped)
	}
}
