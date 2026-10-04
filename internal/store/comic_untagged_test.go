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
