package store

import (
	"database/sql"
	"strings"
	"testing"
)

// createTagNormComic 直接插入测试用漫画行。
func createTagNormComic(t *testing.T, id string) {
	t.Helper()
	if _, err := DB().Exec(
		`INSERT INTO "Comic" ("id", "filename", "title", "type", "libraryId", "relativePath") VALUES (?, ?, ?, 'comic', 'default', ?)`,
		id, id+".cbz", id, id+".cbz",
	); err != nil {
		t.Fatalf("create comic %s: %v", id, err)
	}
}

// tagIDByName 按名取 tagId，不存在返回 (0, false)。
func tagIDByName(t *testing.T, name string) (int, bool) {
	t.Helper()
	var id int
	err := DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = ?`, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		t.Fatalf("query tag %s: %v", name, err)
	}
	return id, true
}

// comicHasTag 判断漫画是否挂有指定标签。
func comicHasTag(t *testing.T, comicID string, tagID int) bool {
	t.Helper()
	var n int
	if err := DB().QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" WHERE "comicId" = ? AND "tagId" = ?`, comicID, tagID,
	).Scan(&n); err != nil {
		t.Fatalf("query ComicTag: %v", err)
	}
	return n > 0
}

func TestTagNormMigrationTables(t *testing.T) {
	setupTestDB(t)

	tables := []string{"TagAlias", "TagOperation", "TagNormIgnore"}
	for _, table := range tables {
		if _, err := DB().Exec(`SELECT COUNT(*) FROM "` + table + `"`); err != nil {
			t.Errorf("Table %s does not exist after migrations: %v", table, err)
		}
	}

	// 最新迁移（v44，标签归一表）已记录到 _migrations
	var n int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM "_migrations" WHERE "version" = 44`).Scan(&n); err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if n != 1 {
		t.Errorf("migration v44 not recorded, got %d rows", n)
	}
}

func TestTagNormKeyFolding(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "  汉化  ", want: "汉化"},    // trim
		{in: "漢化", want: "汉化"},        // 简繁折叠（繁→简）
		{in: "HanZi", want: "hanzi"},   // 大小写
		{in: "後宮", want: "后宫"},        // 后/後 + 宫/宮
		{in: "東方", want: "东方"},        // 东/東
		{in: "全彩漢化組", want: "全彩汉化组"}, // 多字折叠
		{in: "遊戲", want: "游戏"},        // 游/遊 戏/戲
		{in: "无碼", want: "无码"},        // 码/碼
		{in: "", want: ""},
		{in: "   ", want: ""},
	}
	for _, c := range cases {
		got := TagNormKey(c.in)
		if got != c.want {
			t.Errorf("TagNormKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTagNormPairTableIntegrity(t *testing.T) {
	for _, pair := range strings.Fields(tagNormSimpTradPairs) {
		r := []rune(pair)
		if len(r) != 2 {
			t.Errorf("invalid pair %q: want exactly 2 runes, got %d", pair, len(r))
			continue
		}
		if r[0] == r[1] {
			t.Errorf("invalid pair %q: simplified and traditional are the same rune", pair)
		}
	}

	// 幂等性：折叠表中任意 key/value 再次折叠结果不变
	for trad, simp := range tagTradToSimp {
		if again, ok := tagTradToSimp[simp]; ok && again != simp {
			t.Errorf("folding not idempotent: %c -> %c -> %c", trad, simp, again)
		}
	}

	if len(tagTradToSimp) < 100 {
		t.Errorf("expected curated table with >=100 pairs, got %d", len(tagTradToSimp))
	}
}

func TestTagNormWritePathAttachExisting(t *testing.T) {
	setupTestDB(t)
	createTagNormComic(t, "c1")
	createTagNormComic(t, "c2")
	createTagNormComic(t, "c3")
	createTagNormComic(t, "c4")

	// 首次写入：建新标签，保留原始展示名
	if err := AddTagsToComic("c1", []string{"汉化"}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	hanID, ok := tagIDByName(t, "汉化")
	if !ok {
		t.Fatal("expected tag 汉化 to be created")
	}

	// 同 normKey（简繁变体）→ 挂既有标签，不新建
	if err := AddTagsToComic("c2", []string{"漢化"}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	if _, ok := tagIDByName(t, "漢化"); ok {
		t.Error("expected no new tag 漢化 (should reuse 汉化 by normKey)")
	}
	if !comicHasTag(t, "c2", hanID) {
		t.Error("expected c2 to be linked to 汉化 tag")
	}

	// 带空白 + 大小写 → 同一键
	if err := AddTagsToComic("c3", []string{" 汉化 "}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	if !comicHasTag(t, "c3", hanID) {
		t.Error("expected c3 to be linked to 汉化 tag")
	}

	// 别名精确命中 → 用其 tagId
	if err := AddTagAlias("translation", hanID); err != nil {
		t.Fatalf("AddTagAlias: %v", err)
	}
	if err := AddTagsToComic("c4", []string{"translation"}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	if !comicHasTag(t, "c4", hanID) {
		t.Error("expected c4 to be linked via alias to 汉化 tag")
	}
	if _, ok := tagIDByName(t, "translation"); ok {
		t.Error("expected no new tag named translation")
	}

	// 无匹配 → 建新且保留原名
	if err := AddTagsToComic("c4", []string{"漢化組"}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	if _, ok := tagIDByName(t, "漢化組"); !ok {
		t.Error("expected new tag with original display name 漢化組")
	}

	// 空名跳过
	if err := AddTagsToComic("c4", []string{"", "   "}); err != nil {
		t.Fatalf("AddTagsToComic with empty names: %v", err)
	}
}

func TestTagNormSameCallVariantsCollapse(t *testing.T) {
	setupTestDB(t)
	createTagNormComic(t, "c1")

	// 同一次调用里的同 normKey 变体应归并到同一个标签
	if err := AddTagsToComic("c1", []string{"汉化", "漢化"}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	var n int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE ` +
		`"name" IN ('汉化', '漢化')`).Scan(&n); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 tag for same-call variants, got %d", n)
	}
}

func TestTagNormUpdateTagColorWritePath(t *testing.T) {
	setupTestDB(t)
	createTagNormComic(t, "c1")

	if err := AddTagsToComic("c1", []string{"汉化"}); err != nil {
		t.Fatalf("AddTagsToComic: %v", err)
	}
	hanID, _ := tagIDByName(t, "汉化")

	// 通过繁体名设置颜色 → 更新既有标签
	if err := UpdateTagColor("漢化", "red"); err != nil {
		t.Fatalf("UpdateTagColor: %v", err)
	}
	var color string
	if err := DB().QueryRow(`SELECT "color" FROM "Tag" WHERE "id" = ?`, hanID).Scan(&color); err != nil {
		t.Fatalf("query color: %v", err)
	}
	if color != "red" {
		t.Errorf("expected color red on existing tag, got %q", color)
	}
	if _, ok := tagIDByName(t, "漢化"); ok {
		t.Error("expected no new tag 漢化 from UpdateTagColor")
	}

	// 不存在 → 新建（保留原名）
	if err := UpdateTagColor("百合", "blue"); err != nil {
		t.Fatalf("UpdateTagColor: %v", err)
	}
	if _, ok := tagIDByName(t, "百合"); !ok {
		t.Error("expected tag 百合 to be created")
	}
}

func TestTagNormApplyMergeAndUndoRoundTrip(t *testing.T) {
	setupTestDB(t)
	for _, id := range []string{"c1", "c2", "c3"} {
		createTagNormComic(t, id)
	}

	// 既有数据：汉化（最早创建）与简繁变体 漢化 并存（模拟归一上线前的历史数据）
	if err := AddTagsToComic("c3", []string{"汉化"}); err != nil {
		t.Fatalf("seed c3: %v", err)
	}
	if err := AddTagsToComic("c2", []string{"汉化"}); err != nil {
		t.Fatalf("seed c2: %v", err)
	}
	dstID, _ := tagIDByName(t, "汉化")

	res, err := DB().Exec(`INSERT INTO "Tag" ("name") VALUES ('漢化')`)
	if err != nil {
		t.Fatalf("seed legacy 漢化: %v", err)
	}
	srcID64, _ := res.LastInsertId()
	srcID := int(srcID64)
	if _, err := DB().Exec(
		`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES ('c1', ?), ('c2', ?)`, srcID, srcID,
	); err != nil {
		t.Fatalf("seed legacy ComicTag: %v", err)
	}

	// apply：c1 实际迁移（c2 已有目标标签，不重复计数）
	comicCount, err := ApplyTagMerge(dstID, []int{srcID})
	if err != nil {
		t.Fatalf("ApplyTagMerge: %v", err)
	}
	if comicCount != 1 {
		t.Errorf("expected comicCount=1 (only c1 newly linked), got %d", comicCount)
	}
	if _, ok := tagIDByName(t, "漢化"); ok {
		t.Error("expected source tag 漢化 to be deleted")
	}
	for _, cid := range []string{"c1", "c2", "c3"} {
		if !comicHasTag(t, cid, dstID) {
			t.Errorf("expected %s to have target tag after merge", cid)
		}
	}

	// 别名已写入
	aliases, err := ListTagAliases()
	if err != nil {
		t.Fatalf("ListTagAliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "漢化" || aliases[0].TagID != dstID {
		t.Errorf("unexpected aliases after merge: %+v", aliases)
	}

	// 操作日志
	ops, total, err := ListTagOperations(1, 20)
	if err != nil {
		t.Fatalf("ListTagOperations: %v", err)
	}
	if total != 1 || len(ops) != 1 {
		t.Fatalf("expected 1 operation, got total=%d list=%d", total, len(ops))
	}
	op := ops[0]
	if op.Kind != "merge" || op.ToTagID != dstID || op.ToTagName != "汉化" ||
		op.ComicCount != 1 || op.Undone || len(op.FromNames) != 1 || op.FromNames[0] != "漢化" {
		t.Errorf("unexpected operation item: %+v", op)
	}

	// undo：c1 挂回漢化、移除汉化；c3 不受影响；别名删除；undone=1
	if err := UndoTagOperation(op.ID); err != nil {
		t.Fatalf("UndoTagOperation: %v", err)
	}
	newSrcID, ok := tagIDByName(t, "漢化")
	if !ok {
		t.Fatal("expected source tag 漢化 to be restored")
	}
	if !comicHasTag(t, "c1", newSrcID) {
		t.Error("expected c1 to have restored source tag")
	}
	if comicHasTag(t, "c1", dstID) {
		t.Error("expected c1 to lose target tag after undo")
	}
	if !comicHasTag(t, "c2", dstID) {
		t.Error("expected c2 to keep target tag after undo (pre-existing link)")
	}
	if !comicHasTag(t, "c3", dstID) {
		t.Error("expected c3 untouched by undo")
	}
	if comicHasTag(t, "c2", newSrcID) {
		t.Error("expected c2 NOT to get restored source tag (was not migrated)")
	}
	aliases, _ = ListTagAliases()
	if len(aliases) != 0 {
		t.Errorf("expected aliases cleared after undo, got %+v", aliases)
	}
	ops, _, _ = ListTagOperations(1, 20)
	if !ops[0].Undone {
		t.Error("expected operation marked undone")
	}
	// 重复撤销 → 422 语义
	if err := UndoTagOperation(op.ID); err != ErrTagOperationUndone {
		t.Errorf("expected ErrTagOperationUndone, got %v", err)
	}
}

func TestTagNormApplyMultiSourcePerComicRestore(t *testing.T) {
	setupTestDB(t)
	for _, id := range []string{"c1", "c2"} {
		createTagNormComic(t, id)
	}

	// 目标标签（无任何书目）与两个源标签，三者 normKey 互不相同
	if err := AddTagsToComic("c1", []string{"甲标签"}); err != nil {
		t.Fatalf("seed c1: %v", err)
	}
	if err := AddTagsToComic("c2", []string{"乙標簽"}); err != nil {
		t.Fatalf("seed c2: %v", err)
	}
	dstName := "目标标签"
	if err := UpdateTagColor(dstName, "default"); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	dstID, ok := tagIDByName(t, dstName)
	if !ok {
		t.Fatal("expected target tag to exist")
	}
	srcA, _ := tagIDByName(t, "甲标签")
	srcB, _ := tagIDByName(t, "乙標簽")

	comicCount, err := ApplyTagMerge(dstID, []int{srcA, srcB})
	if err != nil {
		t.Fatalf("ApplyTagMerge: %v", err)
	}
	if comicCount != 2 {
		t.Errorf("expected comicCount=2, got %d", comicCount)
	}
	if _, ok := tagIDByName(t, "甲标签"); ok {
		t.Error("expected source 甲标签 deleted")
	}
	if _, ok := tagIDByName(t, "乙標簽"); ok {
		t.Error("expected source 乙標簽 deleted")
	}

	ops, _, _ := ListTagOperations(1, 20)
	if err := UndoTagOperation(ops[0].ID); err != nil {
		t.Fatalf("UndoTagOperation: %v", err)
	}
	// 各自挂回自己的源标签
	aRestored, _ := tagIDByName(t, "甲标签")
	bRestored, _ := tagIDByName(t, "乙標簽")
	if !comicHasTag(t, "c1", aRestored) {
		t.Error("expected c1 restored to 甲标签")
	}
	if comicHasTag(t, "c1", bRestored) {
		t.Error("expected c1 NOT restored to 乙標簽")
	}
	if !comicHasTag(t, "c2", bRestored) {
		t.Error("expected c2 restored to 乙標簽")
	}
	if comicHasTag(t, "c2", aRestored) {
		t.Error("expected c2 NOT restored to 甲标签")
	}
	for _, cid := range []string{"c1", "c2"} {
		if comicHasTag(t, cid, dstID) {
			t.Errorf("expected %s to lose target tag after undo", cid)
		}
	}
}

func TestTagNormApplyMergeNotFound(t *testing.T) {
	setupTestDB(t)
	createTagNormComic(t, "c1")
	if err := AddTagsToComic("c1", []string{"汉化"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dstID, _ := tagIDByName(t, "汉化")

	if _, err := ApplyTagMerge(dstID, []int{99999}); err != ErrTagNotFound {
		t.Errorf("expected ErrTagNotFound for missing source, got %v", err)
	}
	if _, err := ApplyTagMerge(99999, []int{dstID}); err != ErrTagNotFound {
		t.Errorf("expected ErrTagNotFound for missing target, got %v", err)
	}
}

func TestPreviewTagNormalizationAndIgnore(t *testing.T) {
	setupTestDB(t)
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		createTagNormComic(t, id)
	}
	// 簇：汉化(3 本，经归一写入) + 简繁变体 漢化(1 本，模拟历史数据)
	for _, cid := range []string{"c1", "c2", "c3"} {
		if err := AddTagsToComic(cid, []string{"汉化"}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := DB().Exec(`INSERT INTO "Tag" ("name") VALUES ('漢化')`)
	if err != nil {
		t.Fatalf("seed legacy 漢化: %v", err)
	}
	legacyID, _ := res.LastInsertId()
	if _, err := DB().Exec(`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES ('c4', ?)`, legacyID); err != nil {
		t.Fatal(err)
	}
	// 干扰项：单变体簇
	if err := AddTagsToComic("c1", []string{"百合", "百合向"}); err != nil {
		t.Fatal(err)
	}

	clusters, total, err := PreviewTagNormalization(1, 20)
	if err != nil {
		t.Fatalf("PreviewTagNormalization: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", total, clusters)
	}
	c := clusters[0]
	if c.NormKey != "汉化" {
		t.Errorf("unexpected normKey %q", c.NormKey)
	}
	if len(c.Variants) != 2 || c.TotalComics != 4 {
		t.Errorf("unexpected cluster: %+v", c)
	}

	// 分页：pageSize=1 取第一页
	page1, total, err := PreviewTagNormalization(1, 1)
	if err != nil {
		t.Fatalf("PreviewTagNormalization page: %v", err)
	}
	if total != 1 || len(page1) != 1 || page1[0].NormKey != "汉化" {
		t.Errorf("unexpected pagination result: total=%d page1=%+v", total, page1)
	}

	// ignore 后排除
	if err := IgnoreTagNormKey("汉化"); err != nil {
		t.Fatalf("IgnoreTagNormKey: %v", err)
	}
	clusters, total, err = PreviewTagNormalization(1, 20)
	if err != nil {
		t.Fatalf("PreviewTagNormalization: %v", err)
	}
	if total != 0 || len(clusters) != 0 {
		t.Errorf("expected empty preview after ignore, got total=%d", total)
	}
	ignores, err := ListTagNormIgnores()
	if err != nil {
		t.Fatalf("ListTagNormIgnores: %v", err)
	}
	if len(ignores) != 1 || ignores[0].NormKey != "汉化" {
		t.Errorf("unexpected ignores: %+v", ignores)
	}

	// unignore 恢复
	if err := UnignoreTagNormKey("汉化"); err != nil {
		t.Fatalf("UnignoreTagNormKey: %v", err)
	}
	_, total, err = PreviewTagNormalization(1, 20)
	if err != nil {
		t.Fatalf("PreviewTagNormalization: %v", err)
	}
	if total != 1 {
		t.Errorf("expected cluster back after unignore, got total=%d", total)
	}
}

func TestTagAliasCRUDAndConflict(t *testing.T) {
	setupTestDB(t)
	createTagNormComic(t, "c1")
	if err := AddTagsToComic("c1", []string{"汉化"}); err != nil {
		t.Fatal(err)
	}
	hanID, _ := tagIDByName(t, "汉化")

	// 新增别名
	if err := AddTagAlias("漢化", hanID); err != nil {
		t.Fatalf("AddTagAlias: %v", err)
	}
	list, err := ListTagAliases()
	if err != nil {
		t.Fatalf("ListTagAliases: %v", err)
	}
	if len(list) != 1 || list[0].Alias != "漢化" || list[0].TagID != hanID || list[0].TagName != "汉化" {
		t.Fatalf("unexpected alias list: %+v", list)
	}

	// 别名与现有标签名冲突 → 422 语义
	if err := AddTagAlias("汉化", hanID); err != ErrAliasConflictsWithTagName {
		t.Errorf("expected ErrAliasConflictsWithTagName, got %v", err)
	}

	// tagId 不存在 → 404 语义
	if err := AddTagAlias("ghost", 99999); err != ErrTagNotFound {
		t.Errorf("expected ErrTagNotFound, got %v", err)
	}

	// 更新指向（upsert）
	if err := AddTagsToComic("c1", []string{"翻译组"}); err != nil {
		t.Fatal(err)
	}
	otherID, _ := tagIDByName(t, "翻译组")
	if err := AddTagAlias("漢化", otherID); err != nil {
		t.Fatalf("AddTagAlias upsert: %v", err)
	}
	list, _ = ListTagAliases()
	if len(list) != 1 || list[0].TagID != otherID {
		t.Errorf("expected alias repointed to 翻译组 tag, got %+v", list)
	}

	// 删除
	if err := DeleteTagAlias("漢化"); err != nil {
		t.Fatalf("DeleteTagAlias: %v", err)
	}
	list, _ = ListTagAliases()
	if len(list) != 0 {
		t.Errorf("expected empty alias list after delete, got %+v", list)
	}
	// 幂等删除
	if err := DeleteTagAlias("漢化"); err != nil {
		t.Errorf("expected idempotent delete, got %v", err)
	}
}
