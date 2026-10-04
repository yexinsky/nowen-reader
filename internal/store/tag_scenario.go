package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ============================================================
// 标签情景（Tag Scenario）
// 给标签本身建「情景」维度（如 剧情/身体/服装/画风/工具），
// 书库筛选面板按情景分组展示。与 Comic 级 Category 体系无关。
// ============================================================

// TagScenario 情景定义。
type TagScenario struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
}

// TagScenarioTag 情景下（或未分配）的单个标签及漫画计数。
type TagScenarioTag struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	ComicCount int    `json:"comicCount"`
}

// TagScenarioGroup 情景分组（含组内标签）。
type TagScenarioGroup struct {
	TagScenario
	Tags []TagScenarioTag `json:"tags"`
}

var (
	// ErrTagScenarioNotFound 情景不存在（映射 404）。
	ErrTagScenarioNotFound = errors.New("tag scenario not found")
	// ErrTagScenarioNameConflict 情景名称重复（唯一索引，映射 422）。
	ErrTagScenarioNameConflict = errors.New("tag scenario name conflict")
	// ErrTagScenarioNameInvalid 情景名称非法（trim 后为空或超 50 rune，映射 422）。
	ErrTagScenarioNameInvalid = errors.New("tag scenario name invalid")
)

// validateTagScenarioName 校验情景名称：trim 后非空且 ≤50 rune。
func validateTagScenarioName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 50 {
		return "", ErrTagScenarioNameInvalid
	}
	return name, nil
}

// CreateTagScenario 新建情景，返回新 id。color 可为空。
func CreateTagScenario(name, color string) (int, error) {
	name, err := validateTagScenarioName(name)
	if err != nil {
		return 0, err
	}

	// 重名 → 422 语义（唯一索引 TagScenario_name_key 兜底并发）
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM "TagScenario" WHERE "name" = ?)`, name).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, ErrTagScenarioNameConflict
	}

	res, err := db.Exec(`INSERT INTO "TagScenario" ("name", "color") VALUES (?, ?)`, name, color)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return 0, ErrTagScenarioNameConflict
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

// UpdateTagScenario 部分更新情景：传 nil 的字段不改。
func UpdateTagScenario(id int, name, color *string, sortOrder *int) error {
	if id <= 0 {
		return ErrTagScenarioNotFound
	}

	// 存在性优先于字段校验（404 语义）
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM "TagScenario" WHERE "id" = ?)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrTagScenarioNotFound
	}

	sets := []string{}
	args := []interface{}{}
	if name != nil {
		trimmed, err := validateTagScenarioName(*name)
		if err != nil {
			return err
		}
		var conflict bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM "TagScenario" WHERE "name" = ? AND "id" != ?)`,
			trimmed, id,
		).Scan(&conflict); err != nil {
			return err
		}
		if conflict {
			return ErrTagScenarioNameConflict
		}
		sets = append(sets, `"name" = ?`)
		args = append(args, trimmed)
	}
	if color != nil {
		sets = append(sets, `"color" = ?`)
		args = append(args, *color)
	}
	if sortOrder != nil {
		sets = append(sets, `"sortOrder" = ?`)
		args = append(args, *sortOrder)
	}
	if len(sets) == 0 {
		// 无字段可改（存在性已在上方校验）
		return nil
	}

	args = append(args, id)
	res, err := db.Exec(fmt.Sprintf(`UPDATE "TagScenario" SET %s WHERE "id" = ?`, strings.Join(sets, ", ")), args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrTagScenarioNameConflict
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTagScenarioNotFound
	}
	return nil
}

// DeleteTagScenario 删除情景。标签的 scenarioId 由外键 ON DELETE SET NULL 落回未分配。
func DeleteTagScenario(id int) error {
	if id <= 0 {
		return ErrTagScenarioNotFound
	}
	res, err := db.Exec(`DELETE FROM "TagScenario" WHERE "id" = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTagScenarioNotFound
	}
	return nil
}

// AssignTagScenario 批量设置标签的情景。
// scenarioID 为 nil 表示移出情景（scenarioId 置 NULL）。
// tagIds 自动去重；含不存在的 tagId → ErrTagNotFound；
// scenarioID 非 nil 且不存在 → ErrTagScenarioNotFound。
// 返回实际写入的标签数（去重后）。全部校验通过才写库（事务）。
func AssignTagScenario(tagIDs []int, scenarioID *int) (int, error) {
	// 去重并去掉非法 id
	seen := make(map[int]bool)
	ids := make([]int, 0, len(tagIDs))
	for _, id := range tagIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if scenarioID != nil {
		var exists bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM "TagScenario" WHERE "id" = ?)`, *scenarioID).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, ErrTagScenarioNotFound
		}
	}

	// 校验标签全部存在
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var found int
	if err := tx.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM "Tag" WHERE "id" IN (%s)`, placeholders), args...,
	).Scan(&found); err != nil {
		return 0, err
	}
	if found != len(ids) {
		return 0, ErrTagNotFound
	}

	assigned := 0
	for _, id := range ids {
		var res sql.Result
		if scenarioID != nil {
			res, err = tx.Exec(`UPDATE "Tag" SET "scenarioId" = ? WHERE "id" = ?`, *scenarioID, id)
		} else {
			res, err = tx.Exec(`UPDATE "Tag" SET "scenarioId" = NULL WHERE "id" = ?`, id)
		}
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			assigned++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return assigned, nil
}

// ListTagScenarios 返回全部情景（sortOrder ASC, id ASC），供 AI 提示词等使用。
func ListTagScenarios() ([]TagScenario, error) {
	rows, err := db.Query(`SELECT "id", "name", "color", "sortOrder" FROM "TagScenario" ORDER BY "sortOrder" ASC, "id" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []TagScenario
	for rows.Next() {
		var s TagScenario
		if err := rows.Scan(&s.ID, &s.Name, &s.Color, &s.SortOrder); err != nil {
			continue
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// ListTagScenariosWithTags 返回按情景分组的标签清单与未分配标签。
// list 按 sortOrder ASC, id ASC；组内/unassigned 标签按 name ASC；
// comicCount = LEFT JOIN ComicTag 计数。unassigned 永远非 nil（可为空数组）。
func ListTagScenariosWithTags() (groups []TagScenarioGroup, unassigned []TagScenarioTag, err error) {
	scenarios, err := ListTagScenarios()
	if err != nil {
		return nil, nil, err
	}
	groups = make([]TagScenarioGroup, 0, len(scenarios))
	byID := make(map[int]int, len(scenarios)) // scenarioId → groups 下标
	for _, s := range scenarios {
		groups = append(groups, TagScenarioGroup{TagScenario: s, Tags: []TagScenarioTag{}})
		byID[s.ID] = len(groups) - 1
	}
	unassigned = []TagScenarioTag{}

	rows, err := db.Query(`
		SELECT t."id", t."name", t."scenarioId", COUNT(ct."comicId") AS cnt
		FROM "Tag" t
		LEFT JOIN "ComicTag" ct ON ct."tagId" = t."id"
		GROUP BY t."id"
		ORDER BY t."name" ASC
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tag        TagScenarioTag
			scenarioID sql.NullInt64
		)
		if err := rows.Scan(&tag.ID, &tag.Name, &scenarioID, &tag.ComicCount); err != nil {
			continue
		}
		if scenarioID.Valid {
			if idx, ok := byID[int(scenarioID.Int64)]; ok {
				groups[idx].Tags = append(groups[idx].Tags, tag)
				continue
			}
		}
		unassigned = append(unassigned, tag)
	}
	return groups, unassigned, rows.Err()
}

// TagScenarioState 标签的情景分配状态（供 AI 分配流程读取）。
type TagScenarioState struct {
	ID         int
	Name       string
	ScenarioID int // 0 = 未分配
}

// ListTagsWithScenarioState 返回全部标签及当前情景分配，按 name ASC。
func ListTagsWithScenarioState() ([]TagScenarioState, error) {
	rows, err := db.Query(`SELECT "id", "name", COALESCE("scenarioId", 0) FROM "Tag" ORDER BY "name" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []TagScenarioState
	for rows.Next() {
		var t TagScenarioState
		if err := rows.Scan(&t.ID, &t.Name, &t.ScenarioID); err != nil {
			continue
		}
		list = append(list, t)
	}
	return list, rows.Err()
}
