package store

import (
	"errors"
	"testing"
)

func mustTagIDByName(t *testing.T, name string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = ?`, name).Scan(&id); err != nil {
		t.Fatalf("get tag id by name %q failed: %v", name, err)
	}
	return id
}

func tagScenarioID(t *testing.T, name string) *int {
	t.Helper()
	var scenarioID *int
	if err := db.QueryRow(`SELECT "scenarioId" FROM "Tag" WHERE "name" = ?`, name).Scan(&scenarioID); err != nil {
		t.Fatalf("get tag scenarioId for %q failed: %v", name, err)
	}
	return scenarioID
}

func intPtr(v int) *int {
	return &v
}

func TestTagScenarioCRUDAndNameValidation(t *testing.T) {
	setupTestDB(t)

	// 创建：默认 sortOrder=0，name trim 后入库
	id, err := CreateTagScenario("  剧情  ", "#6366f1")
	if err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}
	if id <= 0 {
		t.Fatalf("unexpected scenario id: %d", id)
	}
	scenarios, err := ListTagScenarios()
	if err != nil {
		t.Fatalf("ListTagScenarios failed: %v", err)
	}
	if len(scenarios) != 1 || scenarios[0].Name != "剧情" || scenarios[0].Color != "#6366f1" || scenarios[0].SortOrder != 0 {
		t.Fatalf("unexpected stored scenario: %#v", scenarios)
	}

	// 重名 → 哨兵错误（422 语义）
	if _, err := CreateTagScenario("剧情", ""); !errors.Is(err, ErrTagScenarioNameConflict) {
		t.Fatalf("duplicate create error = %v, want ErrTagScenarioNameConflict", err)
	}

	// 名称校验：trim 后为空 / 超 50 rune
	if _, err := CreateTagScenario("   ", ""); !errors.Is(err, ErrTagScenarioNameInvalid) {
		t.Fatalf("blank name error = %v, want ErrTagScenarioNameInvalid", err)
	}
	long := make([]rune, 51)
	for i := range long {
		long[i] = '标'
	}
	if _, err := CreateTagScenario(string(long), ""); !errors.Is(err, ErrTagScenarioNameInvalid) {
		t.Fatalf("51-rune name error = %v, want ErrTagScenarioNameInvalid", err)
	}

	// 部分更新：只改 color
	other, err := CreateTagScenario("身体", "")
	if err != nil {
		t.Fatalf("CreateTagScenario(other) failed: %v", err)
	}
	newColor := "#ff0000"
	if err := UpdateTagScenario(other, nil, &newColor, nil); err != nil {
		t.Fatalf("UpdateTagScenario(color) failed: %v", err)
	}
	scenarios, _ = ListTagScenarios()
	if scenarios[1].Color != "#ff0000" || scenarios[1].Name != "身体" || scenarios[1].SortOrder != 0 {
		t.Fatalf("partial update changed wrong fields: %#v", scenarios[1])
	}

	// 部分更新：只改 sortOrder
	if err := UpdateTagScenario(other, nil, nil, intPtr(5)); err != nil {
		t.Fatalf("UpdateTagScenario(sortOrder) failed: %v", err)
	}
	scenarios, _ = ListTagScenarios()
	if scenarios[1].SortOrder != 5 {
		t.Fatalf("sortOrder = %d, want 5", scenarios[1].SortOrder)
	}

	// 更新重名（排除自身）→ 冲突
	plotName := "剧情"
	if err := UpdateTagScenario(other, &plotName, nil, nil); !errors.Is(err, ErrTagScenarioNameConflict) {
		t.Fatalf("rename to existing error = %v, want ErrTagScenarioNameConflict", err)
	}
	// 更新为自身同名 → 允许
	selfName := "身体"
	if err := UpdateTagScenario(other, &selfName, nil, nil); err != nil {
		t.Fatalf("rename to self should succeed: %v", err)
	}

	// 名称校验在 update 同样生效
	blank := "  "
	if err := UpdateTagScenario(other, &blank, nil, nil); !errors.Is(err, ErrTagScenarioNameInvalid) {
		t.Fatalf("update blank name error = %v, want ErrTagScenarioNameInvalid", err)
	}

	// 不存在 → 404 语义
	if err := UpdateTagScenario(9999, &plotName, nil, nil); !errors.Is(err, ErrTagScenarioNotFound) {
		t.Fatalf("update missing error = %v, want ErrTagScenarioNotFound", err)
	}
	if err := DeleteTagScenario(9999); !errors.Is(err, ErrTagScenarioNotFound) {
		t.Fatalf("delete missing error = %v, want ErrTagScenarioNotFound", err)
	}

	// 删除
	if err := DeleteTagScenario(other); err != nil {
		t.Fatalf("DeleteTagScenario failed: %v", err)
	}
	scenarios, _ = ListTagScenarios()
	if len(scenarios) != 1 {
		t.Fatalf("expected 1 scenario after delete, got %d", len(scenarios))
	}
}

func TestTagScenarioAssign(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-1", "ts1.cbz", "TS 1", 1000},
		{"ts-2", "ts2.cbz", "TS 2", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("ts-1", []string{"汉化", "BBD"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddTagsToComic("ts-2", []string{"汉化"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	scenarioID, err := CreateTagScenario("工具", "")
	if err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}

	hanhua := mustTagIDByName(t, "汉化")
	bbd := mustTagIDByName(t, "BBD")

	// 批量分配（tagIds 去重：3 个含重复 → 实际 2 个）
	assigned, err := AssignTagScenario([]int{hanhua, bbd, hanhua}, &scenarioID)
	if err != nil {
		t.Fatalf("AssignTagScenario failed: %v", err)
	}
	if assigned != 2 {
		t.Fatalf("assigned = %d, want 2", assigned)
	}
	if got := tagScenarioID(t, "汉化"); got == nil || *got != scenarioID {
		t.Fatalf("汉化 scenarioId = %v, want %d", got, scenarioID)
	}

	// null → 移出情景
	assigned, err = AssignTagScenario([]int{bbd}, nil)
	if err != nil {
		t.Fatalf("AssignTagScenario(nil) failed: %v", err)
	}
	if assigned != 1 {
		t.Fatalf("assigned = %d, want 1", assigned)
	}
	if got := tagScenarioID(t, "BBD"); got != nil {
		t.Fatalf("BBD scenarioId = %v, want NULL after unassign", *got)
	}

	// 不存在的 tagId → 404 语义
	if _, err := AssignTagScenario([]int{hanhua, 99999}, &scenarioID); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("missing tag error = %v, want ErrTagNotFound", err)
	}
	// 不存在的 scenarioId → 404 语义
	if _, err := AssignTagScenario([]int{hanhua}, intPtr(99999)); !errors.Is(err, ErrTagScenarioNotFound) {
		t.Fatalf("missing scenario error = %v, want ErrTagScenarioNotFound", err)
	}
	// 校验失败时不写入：汉化仍保留原情景
	if got := tagScenarioID(t, "汉化"); got == nil || *got != scenarioID {
		t.Fatalf("汉化 scenarioId changed after failed assign: %v", got)
	}
}

func TestTagScenarioDeleteResetsTagsToUnassigned(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-del-1", "tsdel.cbz", "TS Del", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("ts-del-1", []string{"制服"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	scenarioID, err := CreateTagScenario("服装", "")
	if err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}
	zhifu := mustTagIDByName(t, "制服")
	if _, err := AssignTagScenario([]int{zhifu}, &scenarioID); err != nil {
		t.Fatalf("AssignTagScenario failed: %v", err)
	}

	// 删除情景 → 标签 scenarioId 落回 NULL（SET NULL 生效）
	if err := DeleteTagScenario(scenarioID); err != nil {
		t.Fatalf("DeleteTagScenario failed: %v", err)
	}
	if got := tagScenarioID(t, "制服"); got != nil {
		t.Fatalf("制服 scenarioId = %v, want NULL after scenario delete", *got)
	}
}

func TestTagScenarioListOrderingAndCounts(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-l1", "tsl1.cbz", "TS L1", 1000},
		{"ts-l2", "tsl2.cbz", "TS L2", 1000},
		{"ts-l3", "tsl3.cbz", "TS L3", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("ts-l1", []string{"汉化", "巨乳", "BBD"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddTagsToComic("ts-l2", []string{"汉化", "剧情向"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddTagsToComic("ts-l3", []string{"AI繪圖", "巨乳"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	// 剧情 sortOrder=10、工具 sortOrder 缺省 0 → list 按 sortOrder ASC 排序
	toolID, err := CreateTagScenario("工具", "#22c55e")
	if err != nil {
		t.Fatalf("CreateTagScenario(tool) failed: %v", err)
	}
	plotID, err := CreateTagScenario("剧情", "#6366f1")
	if err != nil {
		t.Fatalf("CreateTagScenario(plot) failed: %v", err)
	}
	if err := UpdateTagScenario(plotID, nil, nil, intPtr(10)); err != nil {
		t.Fatalf("UpdateTagScenario(sortOrder) failed: %v", err)
	}

	hanhua := mustTagIDByName(t, "汉化")
	bbd := mustTagIDByName(t, "BBD")
	aiTag := mustTagIDByName(t, "AI繪圖")
	juQingXiang := mustTagIDByName(t, "剧情向")

	if _, err := AssignTagScenario([]int{hanhua, aiTag, bbd}, &toolID); err != nil {
		t.Fatalf("assign to tool failed: %v", err)
	}
	if _, err := AssignTagScenario([]int{juQingXiang}, &plotID); err != nil {
		t.Fatalf("assign to plot failed: %v", err)
	}

	groups, unassigned, err := ListTagScenariosWithTags()
	if err != nil {
		t.Fatalf("ListTagScenariosWithTags failed: %v", err)
	}

	// list 顺序：sortOrder ASC（工具=0 在前，剧情=10 在后）
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Name != "工具" || groups[1].Name != "剧情" {
		t.Fatalf("group order = [%s, %s], want [工具, 剧情]", groups[0].Name, groups[1].Name)
	}

	// 工具组：组内按 name ASC
	toolTags := groups[0].Tags
	if len(toolTags) != 3 {
		t.Fatalf("tool group tags = %d, want 3", len(toolTags))
	}
	counts := map[string]int{}
	for _, tg := range toolTags {
		counts[tg.Name] = tg.ComicCount
	}
	if counts["汉化"] != 2 || counts["BBD"] != 1 || counts["AI繪圖"] != 1 {
		t.Fatalf("tool group counts = %#v, want 汉化=2 BBD=1 AI繪圖=1", counts)
	}
	for i := 1; i < len(toolTags); i++ {
		if toolTags[i-1].Name > toolTags[i].Name {
			t.Fatalf("tool tags not sorted by name ASC: %#v", toolTags)
		}
	}

	// 剧情组
	plotTags := groups[1].Tags
	if len(plotTags) != 1 || plotTags[0].Name != "剧情向" || plotTags[0].ComicCount != 1 {
		t.Fatalf("plot group tags = %#v, want [剧情向 comicCount=1]", plotTags)
	}

	// unassigned：巨乳（scenarioId IS NULL），comicCount=2
	if len(unassigned) != 1 || unassigned[0].Name != "巨乳" || unassigned[0].ComicCount != 2 {
		t.Fatalf("unassigned = %#v, want [巨乳 comicCount=2]", unassigned)
	}
}

// ListTagsWithScenarioState 供 AI 分配读取候选：作者标签不参与情景分类，
// 与 ListTagScenariosWithTags（界面展示）保持一致。
func TestListTagsWithScenarioStateExcludesAuthorTags(t *testing.T) {
	setupTestDB(t)

	if err := BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-kind-1", "tskind1.cbz", "TS Kind 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := AddTagsToComic("ts-kind-1", []string{"巨乳"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := AddAuthorTagToComic("ts-kind-1", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}

	states, err := ListTagsWithScenarioState()
	if err != nil {
		t.Fatalf("ListTagsWithScenarioState failed: %v", err)
	}
	if len(states) != 1 || states[0].Name != "巨乳" || states[0].ScenarioID != 0 {
		t.Fatalf("states = %#v, want only content tag 巨乳", states)
	}

	// 全量清点：库内确有作者标签，只是不参与情景
	var authorCount int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "kind" = 'author'`).Scan(&authorCount); err != nil {
		t.Fatalf("count author tags failed: %v", err)
	}
	if authorCount != 1 {
		t.Fatalf("author tag count = %d, want 1 (fixture sanity)", authorCount)
	}
}
