package store

import (
	"strings"
)

func GetSeriesTags(seriesID string) ([]Tag, error) {
	rows, err := db.Query(`
		SELECT t."id", t."name", t."color"
		FROM "Tag" t
		JOIN "ComicSeriesTag" cst ON cst."tagId" = t."id"
		WHERE cst."seriesId" = ?
		ORDER BY t."name"
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []Tag{}
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func SetSeriesTags(seriesID string, tagNames []string) error {
	// 事务外先经归一解析/创建标签（别名/同 normKey 命中既有标签）
	ix, err := loadTagNormIndex()
	if err != nil {
		return err
	}
	var tagIDs []int
	seen := make(map[int]bool)
	for _, rawName := range tagNames {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		tagID, err := ix.resolve(name)
		if err != nil {
			return err
		}
		if seen[tagID] {
			continue
		}
		seen[tagID] = true
		tagIDs = append(tagIDs, tagID)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM "ComicSeriesTag" WHERE "seriesId" = ?`, seriesID); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO "ComicSeriesTag" ("seriesId", "tagId") VALUES (?, ?)`, seriesID, tagID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func SyncSeriesTagsToItems(seriesID string) (total, synced, tagsCount int, err error) {
	tags, err := GetSeriesTags(seriesID)
	if err != nil {
		return 0, 0, 0, err
	}
	ids, err := GetSeriesMemberComicIDs(seriesID)
	if err != nil {
		return 0, 0, 0, err
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	for _, comicID := range ids {
		if err := AddTagsToComic(comicID, names); err == nil {
			synced++
		}
	}
	return len(ids), synced, len(names), nil
}
