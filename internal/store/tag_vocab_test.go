package store

import (
	"testing"
)

// TestTagVocabularyCRUD 词表增删查 + normKey（繁简）冲突拒绝。
func TestTagVocabularyCRUD(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"vocab-1", "vocab1.cbz", "Vocab 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("vocab-1", []string{"巨乳", "過膝襪"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	juruID := mustTagIDByName(t, "巨乳")
	guoxiID := mustTagIDByName(t, "過膝襪")

	// 添加
	if err := AddTagsToVocabulary([]int{juruID, guoxiID}); err != nil {
		t.Fatalf("AddTagsToVocabulary failed: %v", err)
	}
	// 幂等
	if err := AddTagsToVocabulary([]int{juruID}); err != nil {
		t.Fatalf("AddTagsToVocabulary idempotent failed: %v", err)
	}

	list, err := ListTagVocabulary()
	if err != nil {
		t.Fatalf("ListTagVocabulary failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("vocabulary size = %d, want 2", len(list))
	}
	byName := map[string]TagVocabItem{}
	for _, item := range list {
		byName[item.Name] = item
	}
	if byName["巨乳"].ComicCount != 1 || byName["過膝襪"].TagID != guoxiID {
		t.Fatalf("unexpected vocabulary items: %#v", list)
	}

	// 同 normKey 的简体变体（绕过写入口直接入库）→ 冲突拒绝
	if _, err := db.Exec(`INSERT INTO "Tag" ("name") VALUES ('过膝袜')`); err != nil {
		t.Fatalf("insert simplified variant failed: %v", err)
	}
	simplifiedID := mustTagIDByName(t, "过膝袜")
	if err := AddTagsToVocabulary([]int{simplifiedID}); err != ErrTagVocabNormKeyConflict {
		t.Fatalf("same normKey add = %v, want ErrTagVocabNormKeyConflict", err)
	}

	// 不存在的标签 → ErrTagNotFound
	if err := AddTagsToVocabulary([]int{999999}); err != ErrTagNotFound {
		t.Fatalf("missing tag add = %v, want ErrTagNotFound", err)
	}

	// 移除（幂等）
	if err := RemoveTagsFromVocabulary([]int{juruID}); err != nil {
		t.Fatalf("RemoveTagsFromVocabulary failed: %v", err)
	}
	if err := RemoveTagsFromVocabulary([]int{juruID}); err != nil {
		t.Fatalf("RemoveTagsFromVocabulary idempotent failed: %v", err)
	}
	list, err = ListTagVocabulary()
	if err != nil || len(list) != 1 || list[0].Name != "過膝襪" {
		t.Fatalf("vocabulary after remove = %#v (err=%v)", list, err)
	}
}
