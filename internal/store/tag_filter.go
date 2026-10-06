package store

import (
	"errors"
	"strings"
	"time"
)

// ============================================================
// 标签过滤名单（TagFilter）
//
// 下载入库自动打标（handler/jm_download.go）与 JM 标签补全
// （handler/jm_backfill.go）写入前的黑名单：命中即丢弃，不建标签、不挂链。
// 只存名字不引用 Tag——允许标签尚未入库时预先拉黑（上游还会再带脏标签），
// 标签被删除/改名单条也在；匹配一律走 TagNormKey（trim+小写+繁简折叠），
// 繁简与大小写变体一并命中。
// ============================================================

// maxTagFilterNameRunes 单条过滤名单名称长度上限（与上游标签量级匹配，防脏数据）。
const maxTagFilterNameRunes = 100

// TagFilterItem 过滤名单条目（HTTP 契约字段）。
// TagID/ComicCount 描述与标签库的关联：TagID=0 表示库中尚无同名标签（预防性拉黑）。
type TagFilterItem struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	NormKey    string    `json:"normKey"`
	TagID      int       `json:"tagId"`
	ComicCount int       `json:"comicCount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ErrTagFilterNameEmpty 过滤名单名称为空或超长。
var ErrTagFilterNameEmpty = errors.New("tag filter name is empty or too long")

// ListTagFilters 返回过滤名单（id 升序），附带各项与标签库的关联信息。
func ListTagFilters() ([]TagFilterItem, error) {
	rows, err := db.Query(
		`SELECT "id", "name", "normKey", "createdAt" FROM "TagFilter" ORDER BY "id" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []TagFilterItem{}
	for rows.Next() {
		var item TagFilterItem
		if err := rows.Scan(&item.ID, &item.Name, &item.NormKey, &item.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 标签库关联：normKey → 既有内容标签（同键取最早创建者，与写入口归一同口径）
	tags, err := ListContentTagsWithCounts()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]TagVariant, len(tags))
	for _, tg := range tags {
		key := TagNormKey(tg.Name)
		if _, exists := byKey[key]; !exists {
			byKey[key] = tg
		}
	}
	for i := range list {
		if tg, ok := byKey[list[i].NormKey]; ok {
			list[i].TagID = tg.ID
			list[i].ComicCount = tg.ComicCount
		}
	}
	return list, nil
}

// AddTagFilters 批量添加过滤项（幂等：同 normKey 已存在则跳过）。
// 返回新增与跳过条数；空白名跳过，超长报 ErrTagFilterNameEmpty。
func AddTagFilters(names []string) (added int, skipped int, err error) {
	existing, err := loadTagFilterKeys()
	if err != nil {
		return 0, 0, err
	}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			skipped++
			continue
		}
		if len([]rune(name)) > maxTagFilterNameRunes {
			return added, skipped, ErrTagFilterNameEmpty
		}
		key := TagNormKey(name)
		if key == "" {
			skipped++
			continue
		}
		if _, dup := existing[key]; dup {
			skipped++
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO "TagFilter" ("name", "normKey") VALUES (?, ?) ON CONFLICT("normKey") DO NOTHING`,
			name, key); err != nil {
			return added, skipped, err
		}
		existing[key] = struct{}{}
		added++
	}
	return added, skipped, nil
}

// RemoveTagFilter 移除过滤项（幂等，不存在不报错）。
func RemoveTagFilter(id int) error {
	_, err := db.Exec(`DELETE FROM "TagFilter" WHERE "id" = ?`, id)
	return err
}

// FilterBlockedTags 按过滤名单裁剪标签：返回保留与被丢弃两组，保持原序。
// 名单为空时不查库直接放行（无名单是常态，省掉一次查询）。
func FilterBlockedTags(names []string) (kept []string, dropped []string, err error) {
	if len(names) == 0 {
		return names, nil, nil
	}
	keys, err := loadTagFilterKeys()
	if err != nil {
		return nil, nil, err
	}
	if len(keys) == 0 {
		return names, nil, nil
	}
	for _, name := range names {
		if _, blocked := keys[TagNormKey(name)]; blocked {
			dropped = append(dropped, name)
			continue
		}
		kept = append(kept, name)
	}
	return kept, dropped, nil
}

// loadTagFilterKeys 载入过滤名单的 normKey 集合。
func loadTagFilterKeys() (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT "normKey" FROM "TagFilter"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := map[string]struct{}{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys[key] = struct{}{}
	}
	return keys, rows.Err()
}
