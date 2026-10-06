package store

import "testing"

// TestTagFilterCRUD 过滤名单增删查：既有标签与预防性拉黑同列、繁简/大小写折叠去重、
// 列表回填与标签库的关联信息。
func TestTagFilterCRUD(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"filter-1", "filter1.cbz", "Filter 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("filter-1", []string{"巨乳", "過膝襪"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	juruID := mustTagIDByName(t, "巨乳")

	// 添加：已入库标签 + 库中尚不存在的预防性拉黑 + 繁简变体；空白名跳过
	added, skipped, err := AddTagFilters([]string{"巨乳", "DL版", "过膝袜", "   "})
	if err != nil {
		t.Fatalf("AddTagFilters failed: %v", err)
	}
	if added != 3 || skipped != 1 {
		t.Fatalf("added=%d skipped=%d, want 3/1", added, skipped)
	}

	// 幂等：同名、繁简变体（過膝襪 ↔ 过膝袜）、大小写变体都视为已存在
	added, skipped, err = AddTagFilters([]string{"巨乳", "過膝襪", "dl版"})
	if err != nil {
		t.Fatalf("AddTagFilters idempotent failed: %v", err)
	}
	if added != 0 || skipped != 3 {
		t.Fatalf("idempotent added=%d skipped=%d, want 0/3", added, skipped)
	}

	// 超长名 → ErrTagFilterNameEmpty（调用方转 422）
	longName := make([]rune, maxTagFilterNameRunes+1)
	for i := range longName {
		longName[i] = 'x'
	}
	if _, _, err := AddTagFilters([]string{string(longName)}); err != ErrTagFilterNameEmpty {
		t.Fatalf("overlong name err = %v, want ErrTagFilterNameEmpty", err)
	}

	// 列表：巨乳 关联到既有标签（tagId/书目数）；DL版 尚未入库（tagId=0）
	list, err := ListTagFilters()
	if err != nil {
		t.Fatalf("ListTagFilters failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("filter size = %d, want 3 (%#v)", len(list), list)
	}
	byName := map[string]TagFilterItem{}
	for _, item := range list {
		byName[item.Name] = item
	}
	if byName["巨乳"].TagID != juruID || byName["巨乳"].ComicCount != 1 {
		t.Fatalf("巨乳 item = %#v, want tagId=%d comicCount=1", byName["巨乳"], juruID)
	}
	if byName["过膝袜"].TagID == 0 || byName["过膝袜"].ComicCount != 1 {
		t.Fatalf("过膝袜 (入库为 過膝襪) should resolve to existing tag: %#v", byName["过膝袜"])
	}
	if byName["DL版"].TagID != 0 || byName["DL版"].ComicCount != 0 {
		t.Fatalf("DL版 not in library should stay unmatched: %#v", byName["DL版"])
	}

	// 移除（幂等）
	RemoveTagFilter(byName["DL版"].ID)
	if err := RemoveTagFilter(byName["DL版"].ID); err != nil {
		t.Fatalf("RemoveTagFilter idempotent failed: %v", err)
	}
	if list, _ = ListTagFilters(); len(list) != 2 {
		t.Fatalf("filter size after remove = %d, want 2", len(list))
	}
}

// TestFilterBlockedTags 过滤口径：normKey 命中即丢弃（繁简/大小写折叠），
// 保序分组；名单为空时原样放行。
func TestFilterBlockedTags(t *testing.T) {
	setupTestDB(t)

	// 名单为空：不查库直接放行
	in := []string{"巨乳", "剧情"}
	kept, dropped, err := FilterBlockedTags(in)
	if err != nil || len(kept) != 2 || len(dropped) != 0 {
		t.Fatalf("empty blocklist kept=%v dropped=%v err=%v", kept, dropped, err)
	}

	if _, _, err := AddTagFilters([]string{"巨乳", "過膝襪", "DL版"}); err != nil {
		t.Fatalf("AddTagFilters failed: %v", err)
	}

	kept, dropped, err = FilterBlockedTags([]string{"巨乳", "剧情", "过膝袜", " dl版 ", "NTR"})
	if err != nil {
		t.Fatalf("FilterBlockedTags failed: %v", err)
	}
	wantKept := []string{"剧情", "NTR"}
	if len(kept) != len(wantKept) || kept[0] != wantKept[0] || kept[1] != wantKept[1] {
		t.Fatalf("kept = %v, want %v", kept, wantKept)
	}
	if len(dropped) != 3 {
		t.Fatalf("dropped = %v, want 3 items", dropped)
	}

	// 空输入
	if kept, dropped, err = FilterBlockedTags(nil); err != nil || kept != nil || dropped != nil {
		t.Fatalf("nil input kept=%v dropped=%v err=%v", kept, dropped, err)
	}
}

// TestAddTagsToComicWritesBlockedNames 过滤只作用于上游写入口（下载/补全），
// 手工添加标签不受名单限制——名单不是标签库的删除开关。
func TestAddTagsToComicWritesBlockedNames(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"filter-2", "filter2.cbz", "Filter 2", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if _, _, err := AddTagFilters([]string{"巨乳"}); err != nil {
		t.Fatalf("AddTagFilters failed: %v", err)
	}
	if err := AddTagsToComic("filter-2", []string{"巨乳"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if id := mustTagIDByName(t, "巨乳"); id == 0 {
		t.Fatal("manual tag write should not be blocked by the filter list")
	}
}
