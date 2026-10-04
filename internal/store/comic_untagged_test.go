package store

import (
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/model"
)

func TestGetUntaggedComics(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	lib := &model.Library{Name: "补标库", Type: "comic", RootPath: "/test/backfill", Enabled: true}
	if err := CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}

	comics := []struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{ID: "bf-1", Filename: "呑噬万物-125734.zip", Title: "呑噬万物-125734", FileSize: 10},
		{ID: "bf-2", Filename: "海贼王.zip", Title: "海贼王", FileSize: 20},
		{ID: "bf-novel", Filename: "小说.txt", Title: "某小说", FileSize: 5},
	}
	fileSource := map[string]string{}
	fileLibrary := map[string]string{}
	for _, c := range comics {
		fileSource[c.ID] = ""
		fileLibrary[c.ID] = lib.ID
	}
	if err := BulkCreateComicsWithSource(comics, fileSource, fileLibrary); err != nil {
		t.Fatalf("BulkCreateComicsWithSource failed: %v", err)
	}
	// bf-novel 标记为小说行(混合书库口径),应被排除
	if err := UpdateComicFields("bf-novel", map[string]interface{}{"type": "novel"}); err != nil {
		t.Fatalf("mark novel failed: %v", err)
	}

	// 未打标:三行都在书库,但小说行排除 → bf-1/bf-2
	untagged, err := GetUntaggedComics([]string{lib.ID})
	if err != nil {
		t.Fatalf("GetUntaggedComics failed: %v", err)
	}
	if len(untagged) != 2 {
		t.Fatalf("expected 2 untagged comics, got %d: %+v", len(untagged), untagged)
	}
	ids := map[string]bool{}
	for _, it := range untagged {
		ids[it.ID] = true
	}
	if !ids["bf-1"] || !ids["bf-2"] {
		t.Fatalf("expected bf-1/bf-2 in result: %+v", ids)
	}
	if untagged[0].LibraryID != lib.ID {
		t.Errorf("LibraryID mismatch: %q", untagged[0].LibraryID)
	}

	// bf-2 打标后退出候选
	if err := AddTagsToComic("bf-2", []string{"冒险"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	untagged, err = GetUntaggedComics([]string{lib.ID})
	if err != nil {
		t.Fatalf("GetUntaggedComics failed: %v", err)
	}
	if len(untagged) != 1 || untagged[0].ID != "bf-1" {
		t.Fatalf("expected only bf-1 after tagging bf-2, got %+v", untagged)
	}

	// 空 libraryIDs → 空列表(不返回其他书库数据)
	if got, err := GetUntaggedComics(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty libraryIDs should return empty list, got %v, err %v", got, err)
	}
}

// 漫画名补全候选:不限标签(已打标书也在列)、排除 novel、书库范围正确。
func TestListTitleBackfillCandidates(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	libA := &model.Library{ID: "lib-rename-a", Name: "补名库A", Type: "comic", RootPath: "/test/rename-a", Enabled: true}
	libB := &model.Library{ID: "lib-rename-b", Name: "补名库B", Type: "comic", RootPath: "/test/rename-b", Enabled: true}
	if err := CreateLibrary(libA); err != nil {
		t.Fatalf("CreateLibrary A failed: %v", err)
	}
	if err := CreateLibrary(libB); err != nil {
		t.Fatalf("CreateLibrary B failed: %v", err)
	}

	comics := []struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{ID: "rn-1", Filename: "海贼王-125734.zip", Title: "海贼王-125734", FileSize: 10},
		{ID: "rn-2", Filename: "海贼王 第100卷.zip", Title: "海贼王 第100卷", FileSize: 20},
		{ID: "rn-novel", Filename: "小说.txt", Title: "某小说", FileSize: 5},
	}
	fileSource := map[string]string{}
	fileLibrary := map[string]string{}
	for _, c := range comics {
		fileSource[c.ID] = ""
		fileLibrary[c.ID] = libA.ID
	}
	fileLibrary["rn-other"] = libB.ID
	// rn-2 打标:已打标书不退出 title 候选(改名对象是标题坏掉的书,与标签无关)
	if err := BulkCreateComicsWithSource(comics, fileSource, fileLibrary); err != nil {
		t.Fatalf("BulkCreateComicsWithSource failed: %v", err)
	}
	if err := AddTagsToComic("rn-2", []string{"冒险"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	// rn-novel 标记为小说行(混合书库口径),应被排除
	if err := UpdateComicFields("rn-novel", map[string]interface{}{"type": "novel"}); err != nil {
		t.Fatalf("mark novel failed: %v", err)
	}

	// 全库范围:rn-1(未打标)+ rn-2(已打标)都在列;novel 行排除;B 库不混入
	got, err := ListTitleBackfillCandidates([]string{libA.ID})
	if err != nil {
		t.Fatalf("ListTitleBackfillCandidates failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 title-backfill candidates, got %d: %+v", len(got), got)
	}
	ids := map[string]bool{}
	for _, it := range got {
		ids[it.ID] = true
		if it.LibraryID != libA.ID {
			t.Errorf("LibraryID mismatch for %s: %q", it.ID, it.LibraryID)
		}
	}
	if !ids["rn-1"] || !ids["rn-2"] {
		t.Fatalf("expected rn-1/rn-2 in result (tagged comic must stay): %+v", ids)
	}

	// 书库范围:只取 B 库 → A 库三行均不出现
	got, err = ListTitleBackfillCandidates([]string{libB.ID})
	if err != nil {
		t.Fatalf("ListTitleBackfillCandidates(B) failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("library scope leak: got %+v", got)
	}

	// 空 libraryIDs → 空列表
	if got, err := ListTitleBackfillCandidates(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty libraryIDs should return empty list, got %v, err %v", got, err)
	}
}

// UpdateComicFields 写 title 后 titleSortKey 必须同步重算(漫画名补全改名依赖此行为)。
func TestTitleSortKeyUpdatedWithComicTitle(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	lib := &model.Library{ID: "lib-rename-sort", Name: "改名库", Type: "comic", RootPath: "/test/rename-sort", Enabled: true}
	if err := CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	comics := []struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{ID: "rn-sort-1", Filename: "海贼王-125734.zip", Title: "海贼王-125734", FileSize: 10},
	}
	fileSource := map[string]string{"rn-sort-1": ""}
	fileLibrary := map[string]string{"rn-sort-1": lib.ID}
	if err := BulkCreateComicsWithSource(comics, fileSource, fileLibrary); err != nil {
		t.Fatalf("BulkCreateComicsWithSource failed: %v", err)
	}

	before, err := GetComicByID("rn-sort-1")
	if err != nil || before == nil {
		t.Fatalf("GetComicByID failed: %v", err)
	}
	if before.TitleSortKey == BuildTitleSortKey("海贼王 第100卷") {
		t.Fatalf("precondition: sort key should differ before rename")
	}

	newTitle := "海贼王 第100卷"
	if err := UpdateComicFields("rn-sort-1", map[string]interface{}{"title": newTitle}); err != nil {
		t.Fatalf("UpdateComicFields failed: %v", err)
	}
	// titleSortKey 不经 GetComicByID 读回(该查询不返回该列),直接查库:
	// 写路径显式写入 + Comic_titleSortKey_au 触发器按新 title 重算,二者口径一致
	after, err := GetComicByID("rn-sort-1")
	if err != nil || after == nil {
		t.Fatalf("GetComicByID after rename failed: %v", err)
	}
	if after.Title != newTitle {
		t.Errorf("title = %q, want %q", after.Title, newTitle)
	}
	var gotKey string
	if err := DB().QueryRow(`SELECT "titleSortKey" FROM "Comic" WHERE "id" = 'rn-sort-1'`).Scan(&gotKey); err != nil {
		t.Fatalf("query titleSortKey failed: %v", err)
	}
	if want := BuildTitleSortKey(newTitle); gotKey != want {
		t.Errorf("titleSortKey = %q, want %q (auto-recomputed)", gotKey, want)
	}
}
