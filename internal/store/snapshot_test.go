package store

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestSnapshotCaptureMutateRestoreRoundTrip 全流程：捕获 → 破坏 → 恢复 → 状态逐表一致。
func TestSnapshotCaptureMutateRestoreRoundTrip(t *testing.T) {
	setupTestDB(t)

	// ── 造数据：书目、内容/作者标签、别名、情景、忽略项、分类 ──
	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"snap-1", "snap1.cbz", "Snap 1", 1000},
		{"snap-2", "snap2.cbz", "Snap 2", 2000},
		{"snap-3", "snap3.cbz", "Snap 3", 3000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("snap-1", []string{"巨乳", "少女"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddTagsToComic("snap-2", []string{"少女"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddAuthorTagToComic("snap-3", "某作者"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}
	if err := AddTagAlias("besar", mustTagIDByName(t, "巨乳")); err != nil {
		t.Fatalf("AddTagAlias failed: %v", err)
	}
	scenarioID, err := CreateTagScenario("剧情", "#6366f1")
	if err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}
	if _, err := AssignTagScenario([]int{mustTagIDByName(t, "少女")}, &scenarioID); err != nil {
		t.Fatalf("AssignTagScenario failed: %v", err)
	}
	if err := IgnoreTagNormKey("SHOUJO"); err != nil {
		t.Fatalf("IgnoreTagNormKey failed: %v", err)
	}
	cat, err := CreateCategory("测试分类", "test-cat", "📚")
	if err != nil {
		t.Fatalf("CreateCategory failed: %v", err)
	}
	catID := cat.ID
	if err := BatchSetCategory([]string{"snap-1"}, []string{"test-cat"}); err != nil {
		t.Fatalf("BatchSetCategory failed: %v", err)
	}
	// 词表：巨乳、少女 入词表
	if err := AddTagsToVocabulary([]int{mustTagIDByName(t, "巨乳"), mustTagIDByName(t, "少女")}); err != nil {
		t.Fatalf("AddTagsToVocabulary failed: %v", err)
	}

	// ── 捕获快照 ──
	item, err := CreateSnapshot(SnapshotDomainTagCategory, "测试快照", "manual", "")
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	if item.ID <= 0 || item.Kind != "manual" || item.SizeBytes <= 0 {
		t.Fatalf("unexpected snapshot item: %+v", item)
	}

	// ── 破坏：删标签、并标签、改颜色、删情景、删分类、清别名 ──
	if err := DeleteTag("巨乳"); err != nil {
		t.Fatalf("DeleteTag failed: %v", err)
	}
	if _, err := ApplyTagMerge(mustTagIDByName(t, "少女"), []int{mustTagIDByName(t, "某作者")}); err != nil {
		t.Fatalf("ApplyTagMerge failed: %v", err)
	}
	if err := UpdateTagColor("少女", "#ff0000"); err != nil {
		t.Fatalf("UpdateTagColor failed: %v", err)
	}
	if err := DeleteTagScenario(scenarioID); err != nil {
		t.Fatalf("DeleteTagScenario failed: %v", err)
	}
	if err := DeleteTagAlias("besar"); err != nil {
		t.Fatalf("DeleteTagAlias failed: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM "Category" WHERE "id" = ?`, catID); err != nil {
		t.Fatalf("delete category failed: %v", err)
	}
	// 破坏后再新建一个标签，恢复后应消失，且其 id 不应与恢复回来的 id 冲突
	if err := AddTagsToComic("snap-2", []string{"恢复后不该存在的标签"}); err != nil {
		t.Fatalf("AddTagsToComic (post-snapshot) failed: %v", err)
	}

	// ── 恢复 ──
	result, err := RestoreSnapshot(item.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}
	if result.SafetySnapshot <= 0 {
		t.Fatalf("safety snapshot not created: %+v", result)
	}

	// ── 逐表核对 ──
	tags, err := GetTagsByKind("all")
	if err != nil {
		t.Fatalf("GetTagsByKind failed: %v", err)
	}
	got := map[string]string{}
	for _, tag := range tags {
		got[tag.Name] = tag.Color
	}
	want := map[string]string{"巨乳": "default", "少女": "default", "某作者": "default"}
	for name, color := range want {
		if c, ok := got[name]; !ok || c != color {
			t.Fatalf("tag %q = (%q, exists=%v), want color %q", name, c, ok, color)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("tags after restore = %v, want exactly %v", got, want)
	}

	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" ct JOIN "Tag" t ON t."id" = ct."tagId" WHERE ct."comicId" = 'snap-1'`,
	).Scan(&n); err != nil || n != 2 {
		t.Fatalf("snap-1 tag links after restore = %d (err=%v), want 2", n, err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "TagAlias" WHERE "alias" = 'besar'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("alias besar after restore = %d (err=%v), want 1", n, err)
	}
	scenarioIDAfter := tagScenarioID(t, "少女")
	if scenarioIDAfter == nil || *scenarioIDAfter != scenarioID {
		t.Fatalf("scenario assignment after restore = %v, want %d", scenarioIDAfter, scenarioID)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "TagNormIgnore" WHERE "normKey" = 'shoujo'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("norm ignore after restore = %d (err=%v), want 1", n, err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "ComicCategory" WHERE "comicId" = 'snap-1' AND "categoryId" = ?`, catID,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("comic-category link after restore = %d (err=%v), want 1", n, err)
	}

	// 词表随快照还原（破坏阶段删掉的词表项应回来）
	vocabList, err := ListTagVocabulary()
	if err != nil {
		t.Fatalf("ListTagVocabulary failed: %v", err)
	}
	vocabNames := map[string]bool{}
	for _, v := range vocabList {
		vocabNames[v.Name] = true
	}
	if len(vocabNames) != 2 || !vocabNames["巨乳"] || !vocabNames["少女"] {
		t.Fatalf("vocabulary after restore = %v, want [巨乳 少女]", vocabNames)
	}
	if result.Vocab != 2 {
		t.Fatalf("restore result vocab = %d, want 2", result.Vocab)
	}

	// ── 自增序列：恢复后新建标签 id 必须大于恢复回来的最大 id ──
	if err := AddTagsToComic("snap-2", []string{"序列检查标签"}); err != nil {
		t.Fatalf("AddTagsToComic (post-restore) failed: %v", err)
	}
	var maxID, newID int
	if err := db.QueryRow(`SELECT COALESCE(MAX("id"), 0) FROM "Tag"`).Scan(&maxID); err != nil {
		t.Fatalf("max tag id failed: %v", err)
	}
	if err := db.QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '序列检查标签'`).Scan(&newID); err != nil {
		t.Fatalf("new tag id failed: %v", err)
	}
	if newID <= maxID-1 {
		t.Fatalf("new tag id %d collides with restored ids (max=%d)", newID, maxID)
	}

	// ── 列表应含测试快照 + 恢复兜底快照 ──
	list, err := ListSnapshots(SnapshotDomainTagCategory)
	if err != nil {
		t.Fatalf("ListSnapshots failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("snapshots after restore = %d, want 2 (manual + safety)", len(list))
	}
	if list[0].Kind != "auto" || list[0].Name != "恢复前自动保存" {
		t.Fatalf("newest snapshot = %+v, want safety auto snapshot", list[0])
	}
}

// TestSnapshotRestorePreservesVocabForLegacySnapshot 旧快照（载荷无词表字段）
// 恢复时应保留「恢复前」的词表，而不是清空或回退。
func TestSnapshotRestorePreservesVocabForLegacySnapshot(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"snap-legacy-1", "snaplegacy1.cbz", "Snap Legacy 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("snap-legacy-1", []string{"巨乳", "少女"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	juruID := mustTagIDByName(t, "巨乳")
	shaoID := mustTagIDByName(t, "少女")

	// 快照时刻：词表 = [巨乳]
	if err := AddTagsToVocabulary([]int{juruID}); err != nil {
		t.Fatalf("AddTagsToVocabulary failed: %v", err)
	}
	item, err := CreateSnapshot(SnapshotDomainTagCategory, "旧格式快照", "manual", "")
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}

	// 模拟旧版本快照：从载荷中移除 tagVocab 字段
	var payload string
	if err := db.QueryRow(`SELECT "data" FROM "Snapshot" WHERE "id" = ?`, item.ID).Scan(&payload); err != nil {
		t.Fatalf("read snapshot payload failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	delete(m, "tagVocab")
	legacyPayload, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal legacy payload failed: %v", err)
	}
	if _, err := db.Exec(`UPDATE "Snapshot" SET "data" = ? WHERE "id" = ?`, string(legacyPayload), item.ID); err != nil {
		t.Fatalf("update snapshot payload failed: %v", err)
	}

	// 恢复前把词表改成 [少女]（与快照时刻不同）
	if err := RemoveTagsFromVocabulary([]int{juruID}); err != nil {
		t.Fatalf("RemoveTagsFromVocabulary failed: %v", err)
	}
	if err := AddTagsToVocabulary([]int{shaoID}); err != nil {
		t.Fatalf("AddTagsToVocabulary failed: %v", err)
	}

	if _, err := RestoreSnapshot(item.ID); err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}

	list, err := ListTagVocabulary()
	if err != nil {
		t.Fatalf("ListTagVocabulary failed: %v", err)
	}
	if len(list) != 1 || list[0].Name != "少女" {
		t.Fatalf("vocabulary after legacy restore = %#v, want preserved [少女]", list)
	}
}

// TestSnapshotAutoPrune 自动快照滚动保留最近 autoSnapshotKeep 份，手动快照不受影响。
func TestSnapshotAutoPrune(t *testing.T) {
	setupTestDB(t)

	if _, err := CreateSnapshot(SnapshotDomainTagCategory, "手动保留", "manual", ""); err != nil {
		t.Fatalf("CreateSnapshot manual failed: %v", err)
	}
	for i := 0; i < autoSnapshotKeep+3; i++ {
		if _, err := CreateSnapshot(SnapshotDomainTagCategory, fmt.Sprintf("自动 %d", i), "auto", "测试"); err != nil {
			t.Fatalf("CreateSnapshot auto failed: %v", err)
		}
	}

	list, err := ListSnapshots(SnapshotDomainTagCategory)
	if err != nil {
		t.Fatalf("ListSnapshots failed: %v", err)
	}
	if len(list) != autoSnapshotKeep+1 {
		t.Fatalf("snapshots after prune = %d, want %d (auto) + 1 (manual)", len(list), autoSnapshotKeep)
	}
	autoCount := 0
	sawOldest := false
	for _, s := range list {
		if s.Kind == "auto" {
			autoCount++
			if s.Name == "自动 0" {
				sawOldest = true // 最旧的自动快照应已被清理
			}
		}
	}
	if autoCount != autoSnapshotKeep {
		t.Fatalf("auto snapshots = %d, want %d", autoCount, autoSnapshotKeep)
	}
	if sawOldest {
		t.Fatalf("oldest auto snapshot was not pruned")
	}
}

// TestSnapshotRestoreMissing 恢复/删除不存在的快照返回哨兵错误。
func TestSnapshotRestoreMissing(t *testing.T) {
	setupTestDB(t)

	if _, err := RestoreSnapshot(9999); err != ErrSnapshotNotFound {
		t.Fatalf("RestoreSnapshot(9999) = %v, want ErrSnapshotNotFound", err)
	}
	if err := DeleteSnapshot(9999); err != nil {
		t.Fatalf("DeleteSnapshot missing should be idempotent, got %v", err)
	}
}
