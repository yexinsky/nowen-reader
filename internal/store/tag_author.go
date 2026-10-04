package store

// 作者标签与内容标签的结构性区分（写入口自动完成）。
//
// 历史上下载自动打标把作者名并入普通标签一起写 ComicTag，书库里作者名与内容
// 标签混排。上游 author 与 tags 本是分离字段，混入全是本地写入口造成。
// 本文件提供作者专用写入口：作者走独立 author-kind 标签，内容标签保持纯净化。
//
// kind 读取口径（'tag'=内容标签 / 'author'=作者标签）：
//   - GetAllTags（书库筛选/标签面板数据源）只返回内容标签；
//   - GetUntaggedComics（补标候选）只认内容标签关联——只有作者标签的书仍视为无标签；
//   - 情景分组（ListTagScenariosWithTags）与归一预览（PreviewTagNormalization）
//     排除作者标签（作者不参与情景分类/内容标签合并）；
//   - 按标签搜书（ComicTag join）不排除作者标签——按作者名搜书是合法需求。

import (
	"errors"
	"strings"
)

// TagKindTag / TagKindAuthor Tag.kind 枚举值。
const (
	TagKindTag    = "tag"
	TagKindAuthor = "author"
)

// ErrAuthorNameConflictsWithTag 作者名命中既有内容标签（同名或同 normKey 归一命中）——
// 同名不同义宁可不写：不升级既有标签、不建立关联，调用方记日志跳过。
var ErrAuthorNameConflictsWithTag = errors.New("author name conflicts with existing content tag")

// AddAuthorTagToComic 把作者名以 author-kind 标签挂到漫画（下载/补标写入口专用）。
// 归一口径与 AddTagsToComic 一致（TagAlias 别名精确命中 → 同 normKey 既有标签最早创建者）：
//   - 命中既有 kind='author' → 直接挂 ComicTag（幂等）；
//   - 命中既有 kind='tag'（内容标签撞作者名，如作者恰好叫"萝莉"）→ 返回
//     ErrAuthorNameConflictsWithTag，不升级、不挂；
//   - 未命中 → 新建 kind='author' 标签并挂 ComicTag。
//
// 空名/占位符过滤在 handler 层（jmSyncAuthorName）完成，这里仅兜底拒绝空名。
func AddAuthorTagToComic(comicID, authorName string) error {
	name := strings.TrimSpace(authorName)
	if name == "" {
		return errors.New("author name is empty")
	}
	if strings.TrimSpace(comicID) == "" {
		return errors.New("comic id is empty")
	}

	ix, err := loadTagNormIndex()
	if err != nil {
		return err
	}

	tagID, err := ix.resolveAuthor(name)
	if err != nil {
		return err
	}

	_, err = db.Exec(`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES (?, ?) ON CONFLICT DO NOTHING`, comicID, tagID)
	return err
}

// resolveAuthor 作者写入路径的标签解析：与 resolve 相同的命中顺序（别名精确 →
// 同 normKey 最早创建），但命中结果按 kind 分流——作者只挂 author-kind，
// 撞内容标签返回 ErrAuthorNameConflictsWithTag；未命中才新建 kind='author'。
func (ix *tagNormIndex) resolveAuthor(rawName string) (int, error) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return 0, errors.New("author name is empty")
	}

	// 1) 别名精确命中：别名指向的标签按 kind 分流
	if tagID, ok := ix.aliases[name]; ok {
		if ix.kinds[tagID] == TagKindAuthor {
			return tagID, nil
		}
		return 0, ErrAuthorNameConflictsWithTag
	}

	// 2) 同 normKey 既有标签（最早创建的）：kind=author 挂既有；kind=tag 视为撞名
	key := TagNormKey(name)
	if group := ix.groups[key]; len(group) > 0 {
		tagID := group[0]
		if ix.kinds[tagID] == TagKindAuthor {
			return tagID, nil
		}
		return 0, ErrAuthorNameConflictsWithTag
	}

	// 3) 未命中：新建 author-kind 标签（并发下同名冲突则取既有行）
	if _, err := db.Exec(
		`INSERT INTO "Tag" ("name", "kind") VALUES (?, ?) ON CONFLICT("name") DO NOTHING`,
		name, TagKindAuthor,
	); err != nil {
		return 0, err
	}
	var tagID int
	var kind string
	if err := db.QueryRow(
		`SELECT "id", COALESCE("kind", 'tag') FROM "Tag" WHERE "name" = ?`, name,
	).Scan(&tagID, &kind); err != nil {
		return 0, err
	}
	if kind != TagKindAuthor {
		// 并发竞态：同名内容标签先落库——与撞名口径一致，放弃写入
		return 0, ErrAuthorNameConflictsWithTag
	}
	ix.groups[key] = append(ix.groups[key], tagID)
	ix.kinds[tagID] = TagKindAuthor
	return tagID, nil
}

// GetTagsByKind 按 kind 过滤返回标签及漫画计数：'tag'（内容标签）/ 'author'（作者标签）/
// "all"（全部）。其他值返回错误（handler 层映射 400）。
func GetTagsByKind(kind string) ([]TagWithCount, error) {
	where := ""
	args := []interface{}{}
	switch kind {
	case TagKindTag, TagKindAuthor:
		where = ` WHERE COALESCE(t."kind", 'tag') = ?`
		args = append(args, kind)
	case "all":
		// 不过滤
	default:
		return nil, errors.New("invalid tag kind: must be tag, author or all")
	}

	rows, err := db.Query(`
		SELECT t."id", t."name", t."color", COALESCE(t."kind", 'tag'), COUNT(ct."comicId") as cnt
		FROM "Tag" t
		LEFT JOIN "ComicTag" ct ON ct."tagId" = t."id"`+where+`
		GROUP BY t."id"
		ORDER BY t."name" ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []TagWithCount
	for rows.Next() {
		var t TagWithCount
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Kind, &t.Count); err != nil {
			continue
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []TagWithCount{}
	}
	return tags, rows.Err()
}
