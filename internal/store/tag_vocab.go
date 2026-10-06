package store

import (
	"errors"
	"time"
)

// ============================================================
// 目标词表（TagVocab）
//
// 词表驱动式标签重构的规范目标集合。词表项是既有标签的子集；
// 匹配一律走 TagNormKey（繁→简折叠）——词表内按 normKey 去重、
// AI 映射目标按 normKey 对齐回词表项，展示名保持词表选定写法。
// ============================================================

// TagVocabItem 词表条目（HTTP 契约字段）。
type TagVocabItem struct {
	TagID      int       `json:"tagId"`
	Name       string    `json:"name"`
	ComicCount int       `json:"comicCount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ErrTagVocabNormKeyConflict 词表内已存在同 normKey 的条目（仅繁简/大小写差异）。
var ErrTagVocabNormKeyConflict = errors.New("vocabulary already contains a tag with the same norm key")

// ListTagVocabulary 返回词表（带用量，id 升序）。
func ListTagVocabulary() ([]TagVocabItem, error) {
	rows, err := db.Query(
		`SELECT v."tagId", t."name", COUNT(ct."comicId"), v."createdAt"
		 FROM "TagVocab" v
		 JOIN "Tag" t ON t."id" = v."tagId"
		 LEFT JOIN "ComicTag" ct ON ct."tagId" = t."id"
		 GROUP BY v."tagId"
		 ORDER BY v."createdAt" ASC, v."tagId" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []TagVocabItem{}
	for rows.Next() {
		var item TagVocabItem
		if err := rows.Scan(&item.TagID, &item.Name, &item.ComicCount, &item.CreatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// AddTagsToVocabulary 把标签加入词表（幂等）。
// 与词表现有项同 normKey（仅繁简/大小写差异）→ ErrTagVocabNormKeyConflict。
func AddTagsToVocabulary(tagIDs []int) error {
	if len(tagIDs) == 0 {
		return nil
	}

	existing := make(map[string]bool)
	rows, err := db.Query(
		`SELECT t."name" FROM "TagVocab" v JOIN "Tag" t ON t."id" = v."tagId"`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		existing[TagNormKey(name)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range tagIDs {
		var name string
		if err := db.QueryRow(`SELECT "name" FROM "Tag" WHERE "id" = ?`, id).Scan(&name); err != nil {
			return ErrTagNotFound
		}
		key := TagNormKey(name)
		if existing[key] {
			// 已在词表中：同一 tagId → 幂等跳过；不同 tagId 但同键 → 冲突
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM "TagVocab" WHERE "tagId" = ?`, id).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			return ErrTagVocabNormKeyConflict
		}
		if _, err := db.Exec(
			`INSERT INTO "TagVocab" ("tagId") VALUES (?) ON CONFLICT("tagId") DO NOTHING`, id); err != nil {
			return err
		}
		existing[key] = true
	}
	return nil
}

// RemoveTagsFromVocabulary 把标签移出词表（幂等，不存在时不报错）。
func RemoveTagsFromVocabulary(tagIDs []int) error {
	for _, id := range tagIDs {
		if _, err := db.Exec(`DELETE FROM "TagVocab" WHERE "tagId" = ?`, id); err != nil {
			return err
		}
	}
	return nil
}
