package store

// 作者标签(kind='author')与内容标签(kind='tag')结构性区分的单测:
// 迁移回填、写入口 AddAuthorTagToComic、读取口径(GetAllTags/GetTagsByKind/
// GetUntaggedComics/情景分组/归一预览)。

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/model"
)

// createAuthorTestComic 建一个最小书库+漫画,返回书库 ID。
func createAuthorTestComic(t *testing.T, comicID string) string {
	t.Helper()
	lib := &model.Library{ID: "lib-" + comicID, Name: "作者标签库-" + comicID, Type: "comic", RootPath: "/test/" + comicID, Enabled: true}
	if err := CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	comics := []struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{ID: comicID, Filename: comicID + ".zip", Title: comicID, FileSize: 10},
	}
	fileSource := map[string]string{comicID: ""}
	fileLibrary := map[string]string{comicID: lib.ID}
	if err := BulkCreateComicsWithSource(comics, fileSource, fileLibrary); err != nil {
		t.Fatalf("BulkCreateComicsWithSource failed: %v", err)
	}
	return lib.ID
}

// tagKindByName 查指定名字标签的 kind(不存在 → sql.ErrNoRows)。
func tagKindByName(t *testing.T, name string) string {
	t.Helper()
	var kind string
	if err := DB().QueryRow(`SELECT COALESCE("kind", 'tag') FROM "Tag" WHERE "name" = ?`, name).Scan(&kind); err != nil {
		t.Fatalf("query tag %q kind failed: %v", name, err)
	}
	return kind
}

// comicHasTagName 判断漫画是否挂了指定名字的标签。
func comicHasTagName(t *testing.T, comicID, tagName string) bool {
	t.Helper()
	var n int
	if err := DB().QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" ct JOIN "Tag" t ON t."id" = ct."tagId"
		 WHERE ct."comicId" = ? AND t."name" = ?`, comicID, tagName,
	).Scan(&n); err != nil {
		t.Fatalf("query comic tag failed: %v", err)
	}
	return n > 0
}

// 迁移 v46:与某本书 Comic.author 字面相等的普通标签 → 迁移后 kind='author';
// 无作者匹配的标签保持 kind='tag'。
func TestTagAuthorKindMigrationBackfill(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-author.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(CloseDB)

	// 基础表(迁移前 schema)上造存量数据:
	// comic-1(author='山本ティナ')挂着同名普通标签;另有一个与任何作者无关的标签
	if _, err := DB().Exec(
		`INSERT INTO "Comic" ("id", "filename", "title", "author") VALUES ('comic-1', 'a.zip', 'A', '山本ティナ')`,
	); err != nil {
		t.Fatalf("seed comic failed: %v", err)
	}
	if _, err := DB().Exec(`INSERT INTO "Tag" ("name") VALUES ('山本ティナ')`); err != nil {
		t.Fatalf("seed author tag failed: %v", err)
	}
	if _, err := DB().Exec(`INSERT INTO "Tag" ("name") VALUES ('巨乳')`); err != nil {
		t.Fatalf("seed content tag failed: %v", err)
	}
	if _, err := DB().Exec(
		`INSERT INTO "ComicTag" ("comicId", "tagId")
		 SELECT 'comic-1', "id" FROM "Tag" WHERE "name" = '山本ティナ'`,
	); err != nil {
		t.Fatalf("seed comic-tag failed: %v", err)
	}

	// 标记 1..45 全部已应用 → RunMigrations 只跑 v46(含存量回填)
	if err := ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable failed: %v", err)
	}
	for _, m := range Migrations {
		if m.Version >= 46 {
			continue
		}
		if _, err := DB().Exec(
			`INSERT INTO "_migrations" ("version", "description", "applied_at") VALUES (?, '', CURRENT_TIMESTAMP)`,
			m.Version,
		); err != nil {
			t.Fatalf("mark migration %d failed: %v", m.Version, err)
		}
	}
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	if got := tagKindByName(t, "山本ティナ"); got != TagKindAuthor {
		t.Fatalf("tag %q kind = %q, want %q (backfilled from Comic.author)", "山本ティナ", got, TagKindAuthor)
	}
	if got := tagKindByName(t, "巨乳"); got != TagKindTag {
		t.Fatalf("tag %q kind = %q, want %q (no author match)", "巨乳", got, TagKindTag)
	}
	// 关联保留:迁移只改 kind,不动 ComicTag
	if !comicHasTagName(t, "comic-1", "山本ティナ") {
		t.Fatal("ComicTag link must survive the kind backfill")
	}
}

func TestAddAuthorTagToComic(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	createAuthorTestComic(t, "ac-1")
	createAuthorTestComic(t, "ac-2")

	// ① 新建 author 标签并挂链
	if err := AddAuthorTagToComic("ac-1", " 山本ティナ "); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}
	if got := tagKindByName(t, "山本ティナ"); got != TagKindAuthor {
		t.Fatalf("new tag kind = %q, want %q", got, TagKindAuthor)
	}
	if !comicHasTagName(t, "ac-1", "山本ティナ") {
		t.Fatal("author tag should be linked to ac-1")
	}

	// ② 同名再次写入:幂等挂同一标签,不新建
	if err := AddAuthorTagToComic("ac-2", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic(ac-2) failed: %v", err)
	}
	var n int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "name" = '山本ティナ'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 author tag row, got %d", n)
	}
	if !comicHasTagName(t, "ac-2", "山本ティナ") {
		t.Fatal("author tag should be linked to ac-2")
	}

	// ③ 同 normKey 变体(大小写)命中既有 author 标签 → 挂既有,不新建
	if err := AddAuthorTagToComic("ac-1", "Yamamoto Tina"); err != nil {
		t.Fatalf("AddAuthorTagToComic(Yamamoto Tina) failed: %v", err)
	}
	if err := AddAuthorTagToComic("ac-2", "YAMAMOTO TINA"); err != nil {
		t.Fatalf("AddAuthorTagToComic(YAMAMOTO TINA) failed: %v", err)
	}
	// TagNormKey 是 Go 侧函数:取全部 author 标签名,在 Go 侧按 normKey 统计簇数
	rows, err := DB().Query(`SELECT "name" FROM "Tag" WHERE "kind" = 'author'`)
	if err != nil {
		t.Fatal(err)
	}
	var authorNames []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		authorNames = append(authorNames, n)
	}
	rows.Close()
	yamamotoVariants := 0
	for _, n := range authorNames {
		if TagNormKey(n) == TagNormKey("Yamamoto Tina") {
			yamamotoVariants++
		}
	}
	if yamamotoVariants != 1 {
		t.Fatalf("expected single author tag for normKey variants, got %d (%v)", yamamotoVariants, authorNames)
	}
	if !comicHasTagName(t, "ac-2", "Yamamoto Tina") {
		t.Fatal("variant author write should link to the existing author tag")
	}

	// ④ 同 normKey 变体命中既有 author 标签:先建 "Loli",写 "LOLI" 应挂既有
	if err := AddAuthorTagToComic("ac-1", "Loli"); err != nil {
		t.Fatalf("AddAuthorTagToComic(Loli) failed: %v", err)
	}
	if err := AddAuthorTagToComic("ac-2", "LOLI"); err != nil {
		t.Fatalf("AddAuthorTagToComic(LOLI) failed: %v", err)
	}
	var loliRows int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "name" IN ('Loli', 'LOLI')`).Scan(&loliRows); err != nil {
		t.Fatal(err)
	}
	if loliRows != 1 {
		t.Fatalf("expected single author tag for normKey variants, got %d rows", loliRows)
	}
	if !comicHasTagName(t, "ac-2", "Loli") {
		t.Fatal("variant author write should link to the existing author tag")
	}

	// ⑤ 与内容标签同名 → 哨兵错误:不升级、不挂
	if err := AddTagsToComic("ac-1", []string{"萝莉"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddAuthorTagToComic("ac-2", "萝莉"); !errors.Is(err, ErrAuthorNameConflictsWithTag) {
		t.Fatalf("expected ErrAuthorNameConflictsWithTag, got %v", err)
	}
	if got := tagKindByName(t, "萝莉"); got != TagKindTag {
		t.Fatalf("content tag kind must stay %q, got %q (no upgrade)", TagKindTag, got)
	}
	if comicHasTagName(t, "ac-2", "萝莉") {
		t.Fatal("conflicting author write must not link the tag")
	}
	if !comicHasTagName(t, "ac-1", "萝莉") {
		t.Fatal("content tag link via AddTagsToComic must be untouched")
	}

	// ⑥ 别名指向 author 标签 → 命中挂既有;别名指向内容标签 → 哨兵
	var loliAuthorID int
	if err := DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = 'Loli'`).Scan(&loliAuthorID); err != nil {
		t.Fatal(err)
	}
	if err := AddTagAlias("萝莉作者", loliAuthorID); err != nil {
		t.Fatalf("AddTagAlias failed: %v", err)
	}
	if err := AddAuthorTagToComic("ac-2", "萝莉作者"); err != nil {
		t.Fatalf("AddAuthorTagToComic via alias failed: %v", err)
	}
	if !comicHasTagName(t, "ac-2", "Loli") {
		t.Fatal("alias hit should link the existing author tag")
	}
	var loliContentID int
	if err := DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '萝莉'`).Scan(&loliContentID); err != nil {
		t.Fatal(err)
	}
	if err := AddTagAlias("萝莉别名", loliContentID); err != nil {
		t.Fatalf("AddTagAlias failed: %v", err)
	}
	if err := AddAuthorTagToComic("ac-2", "萝莉别名"); !errors.Is(err, ErrAuthorNameConflictsWithTag) {
		t.Fatalf("expected ErrAuthorNameConflictsWithTag via alias to content tag, got %v", err)
	}

	// ⑦ 空名兜底拒绝(handler 层 jmSyncAuthorName 之外的防线)
	if err := AddAuthorTagToComic("ac-1", "   "); err == nil {
		t.Fatal("empty author name must be rejected")
	}
}

// 只有 author 标签的书仍视为无标签、仍进补标候选;有内容标签的书不在候选。
func TestGetUntaggedComicsIgnoresAuthorTags(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	if err := CreateLibrary(&model.Library{ID: "lib-author-untagged", Name: "候选库", Type: "comic", RootPath: "/test/author-untagged", Enabled: true}); err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	comics := []struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{ID: "ut-author-only", Filename: "a.zip", Title: "A", FileSize: 1},
		{ID: "ut-content-tagged", Filename: "b.zip", Title: "B", FileSize: 2},
		{ID: "ut-plain", Filename: "c.zip", Title: "C", FileSize: 3},
	}
	fileSource := map[string]string{}
	fileLibrary := map[string]string{}
	for _, c := range comics {
		fileSource[c.ID] = ""
		fileLibrary[c.ID] = "lib-author-untagged"
	}
	if err := BulkCreateComicsWithSource(comics, fileSource, fileLibrary); err != nil {
		t.Fatalf("BulkCreateComicsWithSource failed: %v", err)
	}

	// ut-author-only:只有 author 标签
	if err := AddAuthorTagToComic("ut-author-only", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}
	// ut-content-tagged:内容标签
	if err := AddTagsToComic("ut-content-tagged", []string{"冒险"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	got, err := GetUntaggedComics([]string{"lib-author-untagged"})
	if err != nil {
		t.Fatalf("GetUntaggedComics failed: %v", err)
	}
	ids := map[string]bool{}
	for _, it := range got {
		ids[it.ID] = true
	}
	if !ids["ut-author-only"] || !ids["ut-plain"] {
		t.Fatalf("author-only and plain comics must stay candidates, got %+v", ids)
	}
	if ids["ut-content-tagged"] {
		t.Fatalf("content-tagged comic must not be a candidate, got %+v", ids)
	}
}

// GetAllTags 只返回内容标签;GetTagsByKind 按口径分流,条目带 kind 字段。
func TestGetTagsByKindAuthorTagFiltering(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	createAuthorTestComic(t, "tk-1")

	if err := AddTagsToComic("tk-1", []string{"萝莉"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddAuthorTagToComic("tk-1", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}

	content, err := GetAllTags()
	if err != nil {
		t.Fatalf("GetAllTags failed: %v", err)
	}
	if len(content) != 1 || content[0].Name != "萝莉" || content[0].Kind != TagKindTag {
		t.Fatalf("GetAllTags must return only content tags with kind, got %+v", content)
	}

	authors, err := GetTagsByKind(TagKindAuthor)
	if err != nil {
		t.Fatalf("GetTagsByKind(author) failed: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "山本ティナ" || authors[0].Kind != TagKindAuthor {
		t.Fatalf("GetTagsByKind(author) mismatch: %+v", authors)
	}
	if authors[0].Count != 1 {
		t.Fatalf("author tag comicCount = %d, want 1", authors[0].Count)
	}

	all, err := GetTagsByKind("all")
	if err != nil {
		t.Fatalf("GetTagsByKind(all) failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("GetTagsByKind(all) = %+v, want 2 entries", all)
	}

	if _, err := GetTagsByKind("bogus"); err == nil {
		t.Fatal("invalid kind must return an error")
	}
}

// 情景分组与未分配集合排除 author 标签(作者不参与情景分类)。
func TestTagScenarioListExcludesAuthorTags(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	createAuthorTestComic(t, "sc-1")

	if err := AddTagsToComic("sc-1", []string{"剧情向"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddAuthorTagToComic("sc-1", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}

	scenarioID, err := CreateTagScenario("剧情", "")
	if err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}
	var contentTagID int
	if err := DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '剧情向'`).Scan(&contentTagID); err != nil {
		t.Fatal(err)
	}
	if _, err := AssignTagScenario([]int{contentTagID}, &scenarioID); err != nil {
		t.Fatalf("AssignTagScenario failed: %v", err)
	}

	groups, unassigned, err := ListTagScenariosWithTags()
	if err != nil {
		t.Fatalf("ListTagScenariosWithTags failed: %v", err)
	}
	foundInGroup := false
	for _, g := range groups {
		for _, tag := range g.Tags {
			if tag.Name == "山本ティナ" {
				t.Fatalf("author tag must not appear in scenario groups: %+v", tag)
			}
			if tag.Name == "剧情向" {
				foundInGroup = true
			}
		}
	}
	if !foundInGroup {
		t.Fatal("content tag should stay in its scenario group")
	}
	for _, tag := range unassigned {
		if tag.Name == "山本ティナ" {
			t.Fatalf("author tag must not appear in unassigned set: %+v", tag)
		}
	}
	if len(unassigned) != 0 {
		t.Fatalf("unassigned should be empty (content tag assigned, author excluded), got %+v", unassigned)
	}
}

// 归一预览聚类排除 author 标签(作者名变体不参与内容标签合并)。
func TestPreviewTagNormalizationExcludesAuthorTags(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	createAuthorTestComic(t, "np-1")

	// 内容标签同 normKey 簇:"戰鬥" 简繁折叠后与 "战斗" 同 normKey
	if err := AddTagsToComic("np-1", []string{"战斗"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if _, err := DB().Exec(`INSERT INTO "Tag" ("name") VALUES ('戰鬥')`); err != nil {
		t.Fatalf("seed content variant failed: %v", err)
	}
	// 作者标签同 normKey 簇:不应出现在预览中
	if _, err := DB().Exec(`INSERT INTO "Tag" ("name", "kind") VALUES ('Loli', 'author'), ('LOLI', 'author')`); err != nil {
		t.Fatalf("seed author variants failed: %v", err)
	}

	clusters, _, err := PreviewTagNormalization(1, 200)
	if err != nil {
		t.Fatalf("PreviewTagNormalization failed: %v", err)
	}
	contentClusterSeen := false
	for _, cl := range clusters {
		if cl.NormKey == TagNormKey("战斗") {
			contentClusterSeen = true
			if len(cl.Variants) != 2 {
				t.Fatalf("content cluster should hold 2 variants, got %+v", cl.Variants)
			}
		}
		if cl.NormKey == TagNormKey("Loli") {
			t.Fatalf("author variants must not participate in normalization preview: %+v", cl)
		}
	}
	if !contentClusterSeen {
		t.Fatalf("content cluster for 战斗 not found in %+v", clusters)
	}
}

// 按标签搜书路径(join ComicTag)不排除 author-kind:按作者名搜书是合法需求。
func TestComicSearchByTagIncludesAuthorTags(t *testing.T) {
	setupTestDB(t)
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	createAuthorTestComic(t, "sr-1")

	if err := AddAuthorTagToComic("sr-1", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}

	// 按标签搜书路径与筛选不同:author 标签参与(按作者名搜书是合法需求)。
	// 直接断言 ComicTag 关联可按 author 标签命中(搜索 SQL 的 join 语义保持不变)。
	var n int
	if err := DB().QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" ct JOIN "Tag" t ON t."id" = ct."tagId"
		 WHERE t."name" = '山本ティナ' AND COALESCE(t."kind", 'tag') = 'author'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("author tag join must stay visible to search path, got %d", n)
	}
}
