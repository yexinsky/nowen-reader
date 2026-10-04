package store

// 补标签候选查询:书库中无任何标签的漫画(JM 下载自动打标/手动刮削均未覆盖)。
// 供 JM 在线源补标签(internal/handler/jm_backfill.go)使用。
// 「无标签」即进度源:补过标的漫画自动退出候选,前端刷新页面天然断点续跑。

import "time"

// UntaggedComic 无标签漫画的补标签候选行。
type UntaggedComic struct {
	ID           string    `json:"id"`
	LibraryID    string    `json:"libraryId"`
	Title        string    `json:"title"`
	Filename     string    `json:"filename"`
	RelativePath string    `json:"relativePath"`
	Author       string    `json:"author"`
	AddedAt      time.Time `json:"addedAt"`
}

// GetUntaggedComics 列出指定书库中无任何标签的漫画(按加入时间倒序)。
// libraryIDs 为空直接返回空列表(调用方已按管理权限过滤书库)。
// 排除 type='novel'(混合书库中的小说行不参与 JM 补标签)。
func GetUntaggedComics(libraryIDs []string) ([]UntaggedComic, error) {
	return queryBackfillComics(libraryIDs, true)
}

// ListTitleBackfillCandidates 列出指定书库中全部漫画(按加入时间倒序),
// 供「漫画名补全」(internal/handler/jm_backfill_rename.go)使用。
// 与 GetUntaggedComics 的唯一差别:**不加**「无标签」条件——改名对象是标题
// 坏掉的书,与是否已打标无关;书库范围/排除 novel/排序口径完全一致。
// libraryIDs 为空直接返回空列表(调用方已按管理权限过滤书库)。
func ListTitleBackfillCandidates(libraryIDs []string) ([]UntaggedComic, error) {
	return queryBackfillComics(libraryIDs, false)
}

// queryBackfillComics 补全候选共享查询:书库范围 + 排除 novel + addedAt 倒序;
// untaggedOnly 时追加 NOT EXISTS ComicTag(标签补全的进度源)。
func queryBackfillComics(libraryIDs []string, untaggedOnly bool) ([]UntaggedComic, error) {
	out := make([]UntaggedComic, 0)
	if len(libraryIDs) == 0 {
		return out, nil
	}
	untaggedClause := ""
	if untaggedOnly {
		untaggedClause = `
		  AND NOT EXISTS (SELECT 1 FROM "ComicTag" ct WHERE ct."comicId" = c."id")`
	}
	query := `
		SELECT c."id", COALESCE(c."libraryId", ''), c."title", c."filename",
		       COALESCE(c."relativePath", ''), COALESCE(c."author", ''), c."addedAt"
		FROM "Comic" c
		WHERE c."libraryId" IN (` + placeholders(len(libraryIDs)) + `)
		  AND COALESCE(c."type", '') != 'novel'` + untaggedClause + `
		ORDER BY c."addedAt" DESC
	`
	args := make([]any, len(libraryIDs))
	for i, id := range libraryIDs {
		args[i] = id
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var it UntaggedComic
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Title, &it.Filename,
			&it.RelativePath, &it.Author, &it.AddedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
