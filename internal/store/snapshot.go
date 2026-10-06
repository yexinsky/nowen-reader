package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ============================================================
// 快照（Snapshot）
//
// 目标：为「标签+分类域」保存可恢复的全量状态，作为 AI 批量写操作
// （批量标签/批量分类/情景 AI 分配/AI 归并）的安全网。
//
// 范围（domain = "tag_category"）：
//   Tag(id,name,color,kind,scenarioId) / ComicTag / TagAlias /
//   TagNormIgnore / TagScenario / Category / ComicCategory
// 不含 Comic 本体、TagOperation 审计日志。
//
// 恢复语义：整域替换——先删当前域数据，再按快照重建（保留原 id）。
// 恢复前自动生成一份「当前状态」快照兜底（kind=auto）。
// 自动快照（kind=auto）滚动保留最近 autoSnapshotKeep 份。
// ============================================================

// snapshotSchema 快照 JSON 的载荷版本；未来结构变更时递增并做兼容读取。
const snapshotSchema = 1

// autoSnapshotKeep 自动快照的最大保留份数（超出删最旧，仅对 kind=auto）。
const autoSnapshotKeep = 10

var (
	// ErrSnapshotNotFound 快照不存在（映射 404）。
	ErrSnapshotNotFound = errors.New("snapshot not found")
	// ErrSnapshotDomainUnknown 快照 domain 不受支持。
	ErrSnapshotDomainUnknown = errors.New("unknown snapshot domain")
)

// SnapshotItem 快照条目（列表展示用，不含 data 大字段）。
type SnapshotItem struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Reason    string    `json:"reason"`
	Summary   string    `json:"summary"`
	SizeBytes int       `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

// SnapshotDomainTagCategory 标签+分类域。
const SnapshotDomainTagCategory = "tag_category"

// snapshotData 快照 JSON 载荷（schema=1）。行集用「数组的数组」压缩体积，
// 字段顺序即下方注释所述，读写双方都由本文件唯一定义。
type snapshotData struct {
	Schema          int      `json:"schema"`
	Tags            [][]any  `json:"tags"`            // [id, name, color, kind, scenarioId(nullable)]
	ComicTags       [][]any  `json:"comicTags"`       // [comicId, tagId]
	Aliases         [][]any  `json:"aliases"`         // [alias, tagId]
	NormIgnores     []string `json:"normIgnores"`     // normKey
	Scenarios       [][]any  `json:"scenarios"`       // [id, name, color, sortOrder]
	Categories      [][]any  `json:"categories"`      // [id, name, slug, icon, sortOrder]
	ComicCategories [][]any  `json:"comicCategories"` // [comicId, categoryId]
	// TagVocab 目标词表（tagId）。指针区分「本快照未采集词表」（旧快照/字段缺失，
	// 恢复时保留恢复前词表）与「采集了但为空」（恢复为无词表）。
	TagVocab *[]int `json:"tagVocab,omitempty"`
}

// snapshotSummary 供列表页展示的行数统计。
type snapshotSummary struct {
	Tags            int `json:"tags"`
	ComicTags       int `json:"comicTags"`
	Aliases         int `json:"aliases"`
	NormIgnores     int `json:"normIgnores"`
	Scenarios       int `json:"scenarios"`
	Categories      int `json:"categories"`
	ComicCategories int `json:"comicCategories"`
	Vocab           int `json:"vocab"`
}

// CreateSnapshot 捕获当前指定 domain 的全量状态并写入快照表。
// kind: "manual" | "auto"；auto 快照写入后滚动清理超量的旧自动快照。
func CreateSnapshot(domain, name, kind, reason string) (SnapshotItem, error) {
	if domain != SnapshotDomainTagCategory {
		return SnapshotItem{}, ErrSnapshotDomainUnknown
	}
	if name == "" {
		name = time.Now().Format("快照 2006-01-02 15:04:05")
	}
	if kind != "auto" {
		kind = "manual"
	}

	data, summary, err := captureTagDomain()
	if err != nil {
		return SnapshotItem{}, err
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return SnapshotItem{}, err
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return SnapshotItem{}, err
	}

	res, err := db.Exec(
		`INSERT INTO "Snapshot" ("domain", "name", "kind", "reason", "summary", "data", "createdAt")
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		domain, name, kind, reason, string(summaryJSON), string(dataJSON), time.Now().UTC(),
	)
	if err != nil {
		return SnapshotItem{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return SnapshotItem{}, err
	}

	if kind == "auto" {
		if err := pruneAutoSnapshots(domain); err != nil {
			// 清理失败不影响快照本身
			fmt.Printf("[Snapshot] auto prune failed: %v\n", err)
		}
	}

	return SnapshotItem{
		ID:        id,
		Domain:    domain,
		Name:      name,
		Kind:      kind,
		Reason:    reason,
		Summary:   string(summaryJSON),
		SizeBytes: len(dataJSON),
		CreatedAt: time.Now().UTC(),
	}, nil
}

// captureTagDomain 读取标签+分类域全部表，组装版本化 JSON 载荷。
func captureTagDomain() (snapshotData, snapshotSummary, error) {
	data := snapshotData{Schema: snapshotSchema}
	summary := snapshotSummary{}

	rows, err := db.Query(
		`SELECT "id", "name", "color", COALESCE("kind", 'tag'), "scenarioId" FROM "Tag" ORDER BY "id" ASC`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var id int
		var name, color, kind string
		var scenarioID sql.NullInt64
		if err := rows.Scan(&id, &name, &color, &kind, &scenarioID); err != nil {
			rows.Close()
			return data, summary, err
		}
		var scenario any
		if scenarioID.Valid {
			scenario = scenarioID.Int64
		}
		data.Tags = append(data.Tags, []any{id, name, color, kind, scenario})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.Tags = len(data.Tags)

	rows, err = db.Query(`SELECT "comicId", "tagId" FROM "ComicTag"`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var comicID string
		var tagID int
		if err := rows.Scan(&comicID, &tagID); err != nil {
			rows.Close()
			return data, summary, err
		}
		data.ComicTags = append(data.ComicTags, []any{comicID, tagID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.ComicTags = len(data.ComicTags)

	rows, err = db.Query(`SELECT "alias", "tagId" FROM "TagAlias"`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var alias string
		var tagID int
		if err := rows.Scan(&alias, &tagID); err != nil {
			rows.Close()
			return data, summary, err
		}
		data.Aliases = append(data.Aliases, []any{alias, tagID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.Aliases = len(data.Aliases)

	rows, err = db.Query(`SELECT "normKey" FROM "TagNormIgnore"`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return data, summary, err
		}
		data.NormIgnores = append(data.NormIgnores, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.NormIgnores = len(data.NormIgnores)

	rows, err = db.Query(`SELECT "id", "name", "color", "sortOrder" FROM "TagScenario" ORDER BY "id" ASC`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var id, sortOrder int
		var name, color string
		if err := rows.Scan(&id, &name, &color, &sortOrder); err != nil {
			rows.Close()
			return data, summary, err
		}
		data.Scenarios = append(data.Scenarios, []any{id, name, color, sortOrder})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.Scenarios = len(data.Scenarios)

	rows, err = db.Query(
		`SELECT "id", "name", "slug", "icon", "sortOrder" FROM "Category" ORDER BY "id" ASC`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var id, sortOrder int
		var name, slug, icon string
		if err := rows.Scan(&id, &name, &slug, &icon, &sortOrder); err != nil {
			rows.Close()
			return data, summary, err
		}
		data.Categories = append(data.Categories, []any{id, name, slug, icon, sortOrder})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.Categories = len(data.Categories)

	rows, err = db.Query(`SELECT "comicId", "categoryId" FROM "ComicCategory"`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var comicID string
		var catID int
		if err := rows.Scan(&comicID, &catID); err != nil {
			rows.Close()
			return data, summary, err
		}
		data.ComicCategories = append(data.ComicCategories, []any{comicID, catID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	summary.ComicCategories = len(data.ComicCategories)

	// 目标词表（始终采集，空词表也写出空数组——与"字段缺失=旧快照"区分）
	vocabIDs := []int{}
	rows, err = db.Query(`SELECT "tagId" FROM "TagVocab" ORDER BY "createdAt" ASC, "tagId" ASC`)
	if err != nil {
		return data, summary, err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return data, summary, err
		}
		vocabIDs = append(vocabIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, summary, err
	}
	data.TagVocab = &vocabIDs
	summary.Vocab = len(vocabIDs)

	return data, summary, nil
}

// ListSnapshots 返回指定 domain 的快照条目（最新在前，不含 data）。
func ListSnapshots(domain string) ([]SnapshotItem, error) {
	rows, err := db.Query(
		`SELECT "id", "domain", "name", "kind", "reason", "summary", LENGTH("data"), "createdAt"
		 FROM "Snapshot" WHERE "domain" = ? ORDER BY "id" DESC`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []SnapshotItem{}
	for rows.Next() {
		var item SnapshotItem
		var size int64
		if err := rows.Scan(&item.ID, &item.Domain, &item.Name, &item.Kind, &item.Reason,
			&item.Summary, &size, &item.CreatedAt); err != nil {
			continue
		}
		item.SizeBytes = int(size)
		list = append(list, item)
	}
	return list, rows.Err()
}

// RestoreResult 恢复完成的行数统计。
type RestoreResult struct {
	Tags            int   `json:"tags"`
	ComicTags       int   `json:"comicTags"`
	Aliases         int   `json:"aliases"`
	NormIgnores     int   `json:"normIgnores"`
	Scenarios       int   `json:"scenarios"`
	Categories      int   `json:"categories"`
	ComicCategories int   `json:"comicCategories"`
	Vocab           int   `json:"vocab"`
	SafetySnapshot  int64 `json:"safetySnapshotId"`
}

// RestoreSnapshot 将当前标签+分类域整体替换为快照状态。
// 恢复前自动保存一份当前状态快照（kind=auto）兜底。
func RestoreSnapshot(id int64) (RestoreResult, error) {
	var payload string
	var domain string
	err := db.QueryRow(`SELECT "domain", "data" FROM "Snapshot" WHERE "id" = ?`, id).Scan(&domain, &payload)
	if err == sql.ErrNoRows {
		return RestoreResult{}, ErrSnapshotNotFound
	}
	if err != nil {
		return RestoreResult{}, err
	}
	if domain != SnapshotDomainTagCategory {
		return RestoreResult{}, ErrSnapshotDomainUnknown
	}

	var data snapshotData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return RestoreResult{}, fmt.Errorf("invalid snapshot payload: %w", err)
	}
	if data.Schema != snapshotSchema {
		return RestoreResult{}, fmt.Errorf("unsupported snapshot schema %d (want %d)", data.Schema, snapshotSchema)
	}

	// 恢复前兜底：保存当前状态（失败则拒绝恢复，避免两头落空）
	safety, err := CreateSnapshot(domain, "恢复前自动保存", "auto", "恢复快照前的当前状态")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("failed to capture safety snapshot: %w", err)
	}

	// 旧快照未采集词表：恢复时保留恢复前的词表（删除 Tag 会级联清掉 TagVocab）
	var preservedVocab []int
	if data.TagVocab == nil {
		rows, err := db.Query(`SELECT "tagId" FROM "TagVocab"`)
		if err != nil {
			return RestoreResult{}, err
		}
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return RestoreResult{}, err
			}
			preservedVocab = append(preservedVocab, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return RestoreResult{}, err
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return RestoreResult{}, err
	}
	defer tx.Rollback()

	// 清空当前域（先子后父，兼容外键约束）
	for _, stmt := range []string{
		`DELETE FROM "ComicTag"`,
		`DELETE FROM "TagAlias"`,
		`DELETE FROM "TagVocab"`,
		`DELETE FROM "ComicCategory"`,
		`DELETE FROM "Tag"`,
		`DELETE FROM "TagScenario"`,
		`DELETE FROM "Category"`,
		`DELETE FROM "TagNormIgnore"`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return RestoreResult{}, err
		}
	}

	result := RestoreResult{SafetySnapshot: safety.ID}

	// 重建父表（保留原 id），随后同步 AUTOINCREMENT 序列
	for _, row := range data.Scenarios {
		if len(row) != 4 {
			return RestoreResult{}, fmt.Errorf("invalid scenario row")
		}
		if _, err := tx.Exec(
			`INSERT INTO "TagScenario" ("id", "name", "color", "sortOrder") VALUES (?, ?, ?, ?)`,
			row[0], row[1], row[2], row[3],
		); err != nil {
			return RestoreResult{}, err
		}
		result.Scenarios++
	}
	for _, row := range data.Tags {
		if len(row) != 5 {
			return RestoreResult{}, fmt.Errorf("invalid tag row")
		}
		if _, err := tx.Exec(
			`INSERT INTO "Tag" ("id", "name", "color", "kind", "scenarioId") VALUES (?, ?, ?, ?, ?)`,
			row[0], row[1], row[2], row[3], row[4],
		); err != nil {
			return RestoreResult{}, err
		}
		result.Tags++
	}
	for _, row := range data.Categories {
		if len(row) != 5 {
			return RestoreResult{}, fmt.Errorf("invalid category row")
		}
		if _, err := tx.Exec(
			`INSERT INTO "Category" ("id", "name", "slug", "icon", "sortOrder") VALUES (?, ?, ?, ?, ?)`,
			row[0], row[1], row[2], row[3], row[4],
		); err != nil {
			return RestoreResult{}, err
		}
		result.Categories++
	}
	// 重建目标词表：新快照按快照内容还原；旧快照（未采集）保留恢复前词表，
	// 仅保留在恢复后仍存在的标签（避免外键悬空）。
	vocabToRestore := preservedVocab
	if data.TagVocab != nil {
		vocabToRestore = *data.TagVocab
	}
	for _, id := range vocabToRestore {
		res, err := tx.Exec(
			`INSERT INTO "TagVocab" ("tagId")
			 SELECT ? WHERE EXISTS (SELECT 1 FROM "Tag" WHERE "id" = ?)`,
			id, id,
		)
		if err != nil {
			return RestoreResult{}, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			result.Vocab++
		}
	}

	// 同步自增序列，保证恢复后新建行 id 不与恢复进来的 id 冲突
	for _, tbl := range []string{"Tag", "TagScenario", "Category"} {
		if _, err := tx.Exec(
			`INSERT INTO sqlite_sequence ("name", "seq")
			 SELECT ?, (SELECT COALESCE(MAX("id"), 0) FROM "`+tbl+`")
			 WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE "name" = ?)`,
			tbl, tbl,
		); err != nil {
			return RestoreResult{}, err
		}
		if _, err := tx.Exec(
			`UPDATE sqlite_sequence SET "seq" = (SELECT COALESCE(MAX("id"), 0) FROM "`+tbl+`") WHERE "name" = ?`,
			tbl,
		); err != nil {
			return RestoreResult{}, err
		}
	}

	// 重建子表
	for _, row := range data.ComicTags {
		if len(row) != 2 {
			return RestoreResult{}, fmt.Errorf("invalid comicTag row")
		}
		if _, err := tx.Exec(
			`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES (?, ?)`, row[0], row[1],
		); err != nil {
			return RestoreResult{}, err
		}
		result.ComicTags++
	}
	for _, row := range data.Aliases {
		if len(row) != 2 {
			return RestoreResult{}, fmt.Errorf("invalid alias row")
		}
		if _, err := tx.Exec(
			`INSERT INTO "TagAlias" ("alias", "tagId") VALUES (?, ?)`, row[0], row[1],
		); err != nil {
			return RestoreResult{}, err
		}
		result.Aliases++
	}
	for _, key := range data.NormIgnores {
		if _, err := tx.Exec(`INSERT INTO "TagNormIgnore" ("normKey") VALUES (?)`, key); err != nil {
			return RestoreResult{}, err
		}
		result.NormIgnores++
	}
	for _, row := range data.ComicCategories {
		if len(row) != 2 {
			return RestoreResult{}, fmt.Errorf("invalid comicCategory row")
		}
		if _, err := tx.Exec(
			`INSERT INTO "ComicCategory" ("comicId", "categoryId") VALUES (?, ?)`, row[0], row[1],
		); err != nil {
			return RestoreResult{}, err
		}
		result.ComicCategories++
	}

	if err := tx.Commit(); err != nil {
		return RestoreResult{}, err
	}
	return result, nil
}

// DeleteSnapshot 删除快照（幂等，不存在时不报错）。
func DeleteSnapshot(id int64) error {
	_, err := db.Exec(`DELETE FROM "Snapshot" WHERE "id" = ?`, id)
	return err
}

// pruneAutoSnapshots 将 domain 下 kind=auto 的快照滚动保留最近 autoSnapshotKeep 份。
func pruneAutoSnapshots(domain string) error {
	_, err := db.Exec(
		`DELETE FROM "Snapshot"
		 WHERE "domain" = ? AND "kind" = 'auto' AND "id" NOT IN (
			SELECT "id" FROM "Snapshot" WHERE "domain" = ? AND "kind" = 'auto'
			ORDER BY "id" DESC LIMIT ?)`,
		domain, domain, autoSnapshotKeep,
	)
	return err
}
